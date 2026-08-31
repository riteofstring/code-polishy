package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type MergeGateOptions struct {
	Resume bool
}

type gateRunController struct {
	run                  *gaterun.Run
	runner               *gateArtifactRunner
	candidate            string
	requestedBase        string
	workingTreeCandidate bool
	behaviorReview       gaterun.BehaviorReview
	behaviorStatus       BehaviorReviewStatus
}

type gateArtifactRunner struct {
	delegate    runner.Runner
	run         *gaterun.Run
	expected    []MergeGateExecutionCommand
	reusable    map[int]gaterun.ReusableReceipt
	logPaths    map[testLogKey]string
	failedTests map[string]int
	progress    io.Writer
	next        int
	err         error
}

type testLogKey struct {
	name    string
	attempt int
}

type gateDiagnosticRunner struct {
	parent *gateArtifactRunner
}

func newGateRunController(engine *Engine, gate gaterun.GateKind, requestedBase, exactBase, candidate, level string, commands []MergeGateExecutionCommand, behaviorStatus BehaviorReviewStatus, resume bool) (*gateRunController, error) {
	behaviorReview := gaterunBehaviorReview(behaviorStatus)
	identity, err := gateRunIdentity(engine, gate, requestedBase, exactBase, candidate, level, commands, behaviorReview)
	if err != nil {
		return nil, err
	}
	reusable, err := reusableGateReceipts(engine.Repository.Root, gate, identity, resume)
	if err != nil {
		return nil, err
	}
	run, err := gaterun.Start(gaterun.StartOptions{RepositoryRoot: engine.Repository.Root, Identity: identity})
	if err != nil {
		return nil, err
	}
	commandRunner := newGateArtifactRunner(engine, run, commands, reusable)
	return &gateRunController{
		run: run, runner: commandRunner, candidate: candidate, requestedBase: requestedBase,
		behaviorReview: behaviorReview, behaviorStatus: cloneBehaviorReviewStatus(behaviorStatus),
	}, nil
}

func reusableGateReceipts(root string, gate gaterun.GateKind, identity gaterun.Identity, resume bool) (map[int]gaterun.ReusableReceipt, error) {
	if !resume {
		return map[int]gaterun.ReusableReceipt{}, nil
	}
	if gate != gaterun.MergeGate {
		return nil, fmt.Errorf("only merge-gate supports --resume")
	}
	prior, err := gaterun.LoadReport(root, identity)
	if err != nil {
		return nil, fmt.Errorf("resume exact merge-gate run: %w", err)
	}
	if prior.Status != gaterun.RunFailed {
		return nil, fmt.Errorf("resume exact merge-gate run: prior run status is %s, want failed", prior.Status)
	}
	return reusableReceiptsFromReport(root, identity, prior)
}

func reusableReceiptsFromReport(root string, identity gaterun.Identity, report gaterun.Report) (map[int]gaterun.ReusableReceipt, error) {
	reusable := map[int]gaterun.ReusableReceipt{}
	for _, outcome := range report.Commands {
		if !reusableGateOutcome(outcome) {
			continue
		}
		receipt, err := gaterun.LoadReusableReceipt(root, identity, outcome.CommandIndex)
		if err != nil {
			return nil, fmt.Errorf("validate reusable receipt for %q: %w", outcome.Name, err)
		}
		reusable[outcome.CommandIndex] = receipt
	}
	return reusable, nil
}

func reusableGateOutcome(outcome gaterun.CommandOutcome) bool {
	return outcome.Category == gaterun.OrdinaryTest && outcome.Status == gaterun.Passed && outcome.ReceiptPath != ""
}

func newGateArtifactRunner(engine *Engine, run *gaterun.Run, commands []MergeGateExecutionCommand, reusable map[int]gaterun.ReusableReceipt) *gateArtifactRunner {
	progress := engine.Output
	if progress == nil {
		progress = io.Discard
	}
	return &gateArtifactRunner{
		delegate: engine.Runner, run: run, expected: commands, reusable: reusable,
		logPaths: map[testLogKey]string{}, failedTests: map[string]int{}, progress: progress,
	}
}

func gateCandidateIdentity(repo repository.Repository, selection repository.Selection, allowWorkingTree bool) (string, bool, error) {
	candidate, err := repo.CleanHead()
	if err == nil {
		return candidate, false, nil
	}
	if !allowWorkingTree || !errors.Is(err, repository.ErrDirtyCandidate) {
		return "", false, err
	}
	digest, digestErr := workingTreeCandidateDigest(repo.Root, selection)
	return digest, true, digestErr
}

func workingTreeCandidateDigest(root string, selection repository.Selection) (string, error) {
	type entry struct {
		Path    string `json:"path"`
		Mode    uint32 `json:"mode,omitempty"`
		Content []byte `json:"content,omitempty"`
		Deleted bool   `json:"deleted,omitempty"`
	}
	entries := make([]entry, 0, len(selection.Candidate.Paths()))
	deleted := map[string]bool{}
	for _, path := range selection.Candidate.Deleted {
		deleted[path] = true
	}
	for _, path := range selection.Candidate.Paths() {
		item := entry{Path: path, Deleted: deleted[path]}
		if !item.Deleted {
			fullPath := filepath.Join(root, filepath.FromSlash(path))
			info, err := os.Lstat(fullPath)
			if err != nil {
				return "", fmt.Errorf("inspect candidate path %q: %w", path, err)
			}
			item.Mode = uint32(info.Mode())
			if info.Mode()&os.ModeSymlink != 0 {
				target, readErr := os.Readlink(fullPath)
				if readErr != nil {
					return "", fmt.Errorf("read candidate symlink %q: %w", path, readErr)
				}
				item.Content = []byte(target)
			} else {
				data, readErr := os.ReadFile(fullPath)
				if readErr != nil {
					return "", fmt.Errorf("read candidate path %q: %w", path, readErr)
				}
				item.Content = data
			}
		}
		entries = append(entries, item)
	}
	payload, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("encode candidate identity: %w", err)
	}
	return gaterun.ContentSHA256(payload), nil
}

func gateRunIdentity(engine *Engine, gate gaterun.GateKind, requestedBase, exactBase, candidate, level string, commands []MergeGateExecutionCommand, behaviorReview gaterun.BehaviorReview) (gaterun.Identity, error) {
	configuration, err := json.Marshal(engine.Repository.Config)
	if err != nil {
		return gaterun.Identity{}, fmt.Errorf("encode loaded policy configuration: %w", err)
	}
	configurationDigest := gaterun.ContentSHA256(configuration)
	releaseIdentity := gaterun.ReleaseIdentity{Version: "development", Digest: configurationDigest}
	lock, found, err := release.ReadLock(engine.Repository.Root)
	if err != nil {
		return gaterun.Identity{}, err
	}
	if found {
		releaseIdentity = gaterun.ReleaseIdentity{Version: lock.CodePolishyVersion, Digest: lock.ReleaseDigest}
	}
	specifications := make([]gaterun.CommandSpec, 0, len(commands))
	environmentNames := map[string]bool{}
	for _, command := range commands {
		specifications = append(specifications, gateRunCommandSpec(command))
		for _, name := range command.Command.Environment {
			environmentNames[name] = true
		}
	}
	names := make([]string, 0, len(environmentNames))
	for name := range environmentNames {
		names = append(names, name)
	}
	sort.Strings(names)
	environment := make([]gaterun.EnvironmentInput, 0, len(names))
	for _, name := range names {
		value, present := os.LookupEnv(name)
		environment = append(environment, gaterun.EnvironmentInput{Name: name, Value: value, Present: present})
	}
	ambient := []gaterun.EnvironmentInput{}
	for _, entry := range runner.EnvironmentWithPath(names, engine.Repository.CommandEnvironment().PathEntries) {
		name, value, present := strings.Cut(entry, "=")
		if present {
			ambient = append(ambient, gaterun.EnvironmentInput{Name: name, Value: value, Present: true})
		}
	}
	return gaterun.NewIdentity(gaterun.IdentityInput{
		Gate: gate, RequestedBase: requestedBase, ExactBase: exactBase, Candidate: candidate, PolicyLevel: level,
		Release: releaseIdentity, ConfigurationSHA256: configurationDigest,
		Platform: gaterun.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}, Commands: specifications,
		Environment: environment, AmbientEnvironment: ambient, BehaviorReview: behaviorReview,
	})
}

func gateRunCommandSpec(planned MergeGateExecutionCommand) gaterun.CommandSpec {
	command := planned.Command
	return gaterun.CommandSpec{
		Category: planned.Category, Scope: planned.Scope, Cost: planned.Cost, Name: command.Name,
		Provides: append([]string{}, command.Provides...), Argv: append([]string{}, command.Argv...), Cwd: command.Cwd,
		Paths: append([]string{}, command.Paths...), Modules: append([]string{}, command.Modules...), RunOn: append([]string{}, command.RunOn...),
		Environment: append([]string{}, command.Environment...), ExclusiveResources: append([]string{}, command.ExclusiveResources...),
		TimeoutSeconds: command.TimeoutSeconds, Managed: command.Managed, PassFiles: command.PassFiles,
		PassFilePaths: append([]string{}, command.PassFilePaths...), SealedEnvironment: command.SealedEnvironment,
	}
}

func gateBehaviorProofCommands(plan behaviorreview.GateReplayPlan) ([]MergeGateExecutionCommand, error) {
	if len(plan.Proofs) == 0 {
		return []MergeGateExecutionCommand{}, nil
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("encode behavior proof replay plan: %w", err)
	}
	modules := []string{}
	paths := []string{}
	environment := []string{}
	resources := []string{}
	timeout := 1
	for _, proof := range plan.Proofs {
		for _, command := range []policy.Command{proof.Baseline, proof.Candidate} {
			modules = append(modules, command.Modules...)
			paths = append(paths, command.Paths...)
			environment = append(environment, command.Environment...)
			resources = append(resources, command.ExclusiveResources...)
			if timeout < 24*60*60-command.TimeoutSeconds {
				timeout += command.TimeoutSeconds
			} else {
				timeout = 24 * 60 * 60
			}
		}
	}
	command := policy.Command{
		Name: "behavior-proof-replay", Provides: []string{"behavior-proof-replay"},
		Argv: []string{"code-polishy-internal-behavior-proof-replay", string(encoded)}, Cwd: ".",
		Paths: sortedUniqueStrings(paths), Modules: sortedUniqueStrings(modules),
		Environment: sortedUniqueStrings(environment), ExclusiveResources: sortedUniqueStrings(resources), TimeoutSeconds: timeout,
	}
	return []MergeGateExecutionCommand{{
		Category: gaterun.BehaviorProof, Kind: "behavior-proof", Scope: "repository", Cost: "standard", Command: command,
	}}, nil
}

func (controller *gateRunController) finalize(engine *Engine, report Report, gateErr error) (Report, error) {
	controller.attachTestLogPaths(&report)
	operationalErr := errors.Join(controller.runner.err, controller.candidateIntegrityError(engine))
	status := gateRunStatus(report, gateErr, operationalErr)
	final, finalizeErr := controller.finalizeArtifact(status, report)
	if finalizeErr != nil {
		operationalErr = errors.Join(operationalErr, finalizeErr)
		status = gaterun.RunOperational
	}
	report.GateRunPolicy = controller.gateRunPolicy(status, final)
	if operationalErr == nil {
		return report, gateErr
	}
	return report, errors.Join(gateErr, operationalErr)
}

func (controller *gateRunController) preparePassed(engine *Engine, report *Report) (*gaterun.PreparedFinalization, error) {
	controller.attachTestLogPaths(report)
	if err := errors.Join(controller.runner.err, controller.candidateIntegrityError(engine)); err != nil {
		return nil, err
	}
	return controller.run.PrepareFinalization(controller.finalizeOptions(gaterun.RunPassed, *report))
}

func (controller *gateRunController) finalizeOperational(report Report, cause error) (Report, error) {
	controller.attachTestLogPaths(&report)
	final, err := controller.run.Finalize(controller.finalizeOptions(gaterun.RunOperational, report))
	if err != nil {
		report.GateRunPolicy = controller.gateRunPolicy(gaterun.RunOperational, gaterun.Report{})
		return report, errors.Join(cause, err)
	}
	report.GateRunPolicy = controller.gateRunPolicy(gaterun.RunOperational, final)
	return report, cause
}

func (controller *gateRunController) attachTestLogPaths(report *Report) {
	for key, path := range controller.runner.logPaths {
		AttachTestCommandLogPath(report, key.name, key.attempt, path)
	}
}

func (controller *gateRunController) candidateIntegrityError(engine *Engine) error {
	current, err := controller.currentCandidate(engine)
	if err != nil {
		return err
	}
	if current == controller.candidate {
		return nil
	}
	return fmt.Errorf("gate candidate changed from %s to %s", controller.candidate, current)
}

func (controller *gateRunController) currentCandidate(engine *Engine) (string, error) {
	if !controller.workingTreeCandidate {
		return engine.Repository.CleanHead()
	}
	selection, err := engine.Repository.SelectBase(controller.requestedBase)
	if err != nil {
		return "", err
	}
	return workingTreeCandidateDigest(engine.Repository.Root, selection)
}

func gateRunStatus(report Report, gateErr, operationalErr error) gaterun.RunStatus {
	if operationalErr != nil {
		return gaterun.RunOperational
	}
	if gateErr != nil || len(report.Findings) > 0 {
		return gaterun.RunFailed
	}
	return gaterun.RunPassed
}

func (controller *gateRunController) finalizeArtifact(status gaterun.RunStatus, report Report) (gaterun.Report, error) {
	return controller.run.Finalize(controller.finalizeOptions(status, report))
}

func (controller *gateRunController) finalizeOptions(status gaterun.RunStatus, report Report) gaterun.FinalizeOptions {
	behaviorReview := controller.behaviorReview
	if report.BehaviorReview != nil {
		behaviorReview = gaterunBehaviorReview(*report.BehaviorReview)
	}
	return gaterun.FinalizeOptions{
		Status: status, Findings: gateRunFindings(report.Findings), TestEvidence: gateRunTestEvidence(report.TestCommands),
		TestDiagnostics: gateRunTestDiagnostics(report.TestDiagnostics), BehaviorReview: behaviorReview,
	}
}

func gateRunFindings(findings []policy.Finding) []gaterun.Finding {
	result := make([]gaterun.Finding, 0, len(findings))
	for _, finding := range findings {
		result = append(result, gaterun.Finding{Check: finding.Check, Path: finding.Path, Subject: finding.Subject, Message: finding.Message})
	}
	return result
}

func (controller *gateRunController) gateRunPolicy(status gaterun.RunStatus, report gaterun.Report) *GateRunPolicy {
	reused := []string{}
	for _, outcome := range report.Commands {
		if outcome.Reused {
			reused = append(reused, outcome.Name)
		}
	}
	path := ""
	if report.ExecutionID != "" {
		path = controller.run.ReportPath()
	}
	return &GateRunPolicy{Status: string(status), ReportPath: path, ReusedPhases: reused}
}

func gateRunTestEvidence(commands []TestCommandEvidence) []gaterun.TestEvidence {
	evidence := make([]gaterun.TestEvidence, 0, len(commands))
	for _, command := range commands {
		status := gaterun.Passed
		if command.FailureMessage != "" {
			status = gaterun.Failed
		}
		evidence = append(evidence, gaterun.TestEvidence{
			Name: command.Name, Kind: command.Kind, Scope: command.Scope, Cost: command.Cost, Target: command.Target,
			SuiteModules: append([]string{}, command.SuiteModules...), SuitePaths: append([]string{}, command.SuitePaths...),
			ChangedModules: append([]string{}, command.ChangedModules...), ImpactedModules: append([]string{}, command.ImpactedModules...),
			ChangedModuleOverlap: append([]string{}, command.ChangedModuleOverlap...), ImpactedModuleOverlap: append([]string{}, command.ImpactedModuleOverlap...),
			ChangedPathOverlap: append([]string{}, command.ChangedPathOverlap...), Status: status,
			FailureCategory: gaterun.FailureCategory(command.FailureCategory), FailureMessage: command.FailureMessage,
			Attempt: command.Attempt, LogPath: command.LogPath, Diagnostic: command.Attempt > 1 || command.Target != "working-tree",
		})
	}
	return evidence
}

func gateRunTestDiagnostics(diagnostics []TestFailureDiagnostic) []gaterun.TestDiagnostic {
	result := make([]gaterun.TestDiagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		converted := gaterun.TestDiagnostic{Suite: diagnostic.Suite, State: diagnostic.State}
		if diagnostic.CandidateRetry != nil {
			evidence := gateRunTestEvidence([]TestCommandEvidence{*diagnostic.CandidateRetry})[0]
			converted.CandidateRetry = &evidence
		}
		if diagnostic.BaselineReplay != nil {
			evidence := gateRunTestEvidence([]TestCommandEvidence{*diagnostic.BaselineReplay})[0]
			converted.BaselineReplay = &evidence
		}
		result = append(result, converted)
	}
	return result
}
