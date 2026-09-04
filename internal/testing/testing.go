package testing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

type Request struct {
	Full          bool
	Recommended   bool
	Supplemental  bool
	Resume        bool
	Modules       []string
	Suites        []string
	Changed       repository.Selection
	RequestedBase string
}

type Plan struct {
	Suites          []policy.TestSuite
	Aggregations    []SuiteAggregation
	ChangedModules  []string
	ImpactedModules []string
	SelectionBase   string
	Level           string
	Reasons         []string
}

type SuiteAggregation struct {
	Suite      string
	ExecutedBy string
	Reason     string
}

type RunResult struct {
	Findings   []policy.Finding
	Executions []SuiteExecution
}

type SuiteExecution struct {
	Suite           policy.TestSuite
	Result          runner.Result
	ResultKnown     bool
	FailureCategory runner.FailureCategory
	FailureMessage  string
	Attempt         int
	Artifacts       []testartifact.Record
	Reused          bool
	ReceiptPath     string
	ReceiptSHA256   string
}

type SuiteReuseController interface {
	ReuseSuite(policy.TestSuite, int) (SuiteExecution, bool, error)
	RecordSuite(SuiteExecution) error
}

type SuiteExecutionViewController interface {
	PrepareSuiteView(policy.TestSuite) (string, func() error, error)
}

func (execution SuiteExecution) Failed() bool {
	return execution.FailureMessage != ""
}

type Advice struct {
	ChangedModules           []string
	ImpactedModules          []string
	Focused                  []policy.TestSuite
	Recommended              []policy.TestSuite
	Full                     []policy.TestSuite
	AllSupplemental          []policy.TestSuite
	Supplemental             []policy.TestSuite
	FocusedAggregations      []SuiteAggregation
	RecommendedAggregations  []SuiteAggregation
	FullAggregations         []SuiteAggregation
	SupplementalAggregations []SuiteAggregation
	Suggested                string
	Reasons                  []string
}

const (
	MergeLevelDocumentation = "documentation"
	MergeLevelRecommended   = "recommended"
	MergeLevelFull          = "full"
)

type MergeDecision struct {
	Level   string
	Reasons []string
}

func BuildPlan(repo repository.Repository, request Request) (Plan, error) {
	plan, err := buildUnaggregatedPlan(repo, request)
	if err != nil {
		return Plan{}, err
	}
	plan.Suites, plan.Aggregations = aggregateSelectedSuites(plan.Suites)
	return plan, nil
}

func buildUnaggregatedPlan(repo repository.Repository, request Request) (Plan, error) {
	if plan, selected := documentationPlan(repo, request); selected {
		return plan, nil
	}
	if request.Supplemental {
		return Plan{
			Suites: selectByRunOn(repo.Config.Tests.Suites, "supplemental"), Level: "supplemental",
			Reasons: []string{"direct supplemental selection"},
		}, nil
	}
	if request.Full {
		return Plan{
			Suites: selectByRunOn(repo.Config.Tests.Suites, "full"), Level: "full",
			Reasons: []string{"direct full-profile selection"},
		}, nil
	}
	if len(request.Suites) > 0 {
		plan, err := explicitSuitePlan(repo.Config.Tests.Suites, request.Suites)
		if err != nil {
			return Plan{}, err
		}
		plan.Level = "focused"
		plan.Reasons = []string{"direct named-suite selection"}
		return plan, nil
	}
	direct := uniqueStrings(request.Modules)
	impacted := append([]string{}, direct...)
	if len(request.Modules) == 0 {
		impact := repo.ImpactForPaths(analysisPaths(request.Changed))
		direct = impact.DirectModules
		impacted = impact.ImpactedModules
	}
	if err := validateModuleNames(repo.Config, direct); err != nil {
		return Plan{}, err
	}
	changedPaths := analysisPaths(request.Changed)
	profile := "focused"
	reason := "direct focused selection"
	if request.Recommended {
		profile = "recommended"
		reason = "direct recommended-profile selection"
	}
	if len(request.Modules) > 0 {
		reason = "direct module-focused selection"
	}
	selected := suitesForProfile(repo.Config.Tests.Suites, profile, impacted, changedPaths)
	return Plan{
		Suites: uniqueSuites(selected), ChangedModules: direct, ImpactedModules: impacted,
		SelectionBase: request.Changed.Base, Level: profile, Reasons: []string{reason},
	}, nil
}

func documentationPlan(repo repository.Repository, request Request) (Plan, bool) {
	if request.Full || request.Recommended || request.Supplemental || len(request.Modules) > 0 || len(request.Suites) > 0 {
		return Plan{}, false
	}
	classification := repo.ClassifyDocumentationCandidate(request.Changed)
	if !classification.Ordinary {
		return Plan{}, false
	}
	return Plan{
		Level: MergeLevelDocumentation, SelectionBase: request.Changed.Base, Reasons: []string{classification.Reason},
	}, true
}

func BuildAdvice(repo repository.Repository, selection repository.Selection) (Advice, error) {
	focused, err := BuildPlan(repo, Request{Changed: selection})
	if err != nil {
		return Advice{}, err
	}
	recommended, err := BuildPlan(repo, Request{Recommended: true, Changed: selection})
	if err != nil {
		return Advice{}, err
	}
	full, err := BuildPlan(repo, Request{Full: true})
	if err != nil {
		return Advice{}, err
	}
	allSupplemental, err := BuildPlan(repo, Request{Supplemental: true})
	if err != nil {
		return Advice{}, err
	}
	changedPaths := analysisPaths(selection)
	supplemental := suitesForProfile(repo.Config.Tests.Suites, "supplemental", recommended.ImpactedModules, changedPaths)
	suggested, reasons := recommendScope(repo.Config, selection, recommended)
	return Advice{
		ChangedModules: recommended.ChangedModules, ImpactedModules: recommended.ImpactedModules,
		Focused: focused.Suites, Recommended: recommended.Suites, Full: full.Suites,
		AllSupplemental: allSupplemental.Suites, Supplemental: uniqueSuites(supplemental),
		FocusedAggregations: focused.Aggregations, RecommendedAggregations: recommended.Aggregations,
		FullAggregations: full.Aggregations, SupplementalAggregations: allSupplemental.Aggregations,
		Suggested: suggested, Reasons: reasons,
	}, nil
}

func aggregateSelectedSuites(suites []policy.TestSuite) ([]policy.TestSuite, []SuiteAggregation) {
	unique := uniqueSuites(suites)
	selected := selectedSuiteNames(unique)
	redirect, reasons := coveredSuiteRedirects(suites, selected)
	withoutCovered := suitesWithoutRedirect(unique, redirect)
	kept := deduplicateSuites(withoutCovered, redirect, reasons)
	return kept, suiteAggregations(suites, redirect, reasons)
}

func selectedSuiteNames(suites []policy.TestSuite) map[string]bool {
	selected := map[string]bool{}
	for _, suite := range suites {
		selected[suite.Name] = true
	}
	return selected
}

func coveredSuiteRedirects(suites []policy.TestSuite, selected map[string]bool) (map[string]string, map[string]string) {
	redirect := map[string]string{}
	reasons := map[string]string{}
	for _, suite := range suites {
		if !selected[suite.Name] {
			continue
		}
		for _, target := range suite.Covers {
			if selected[target] {
				redirect[target] = suite.Name
				reasons[target] = "covered"
			}
		}
	}
	return redirect, reasons
}

func suitesWithoutRedirect(suites []policy.TestSuite, redirect map[string]string) []policy.TestSuite {
	withoutCovered := []policy.TestSuite{}
	for _, suite := range suites {
		if redirect[suite.Name] == "" {
			withoutCovered = append(withoutCovered, suite)
		}
	}
	return withoutCovered
}

func deduplicateSuites(suites []policy.TestSuite, redirect, reasons map[string]string) []policy.TestSuite {
	executions := map[string]string{}
	kept := []policy.TestSuite{}
	for _, suite := range suites {
		key, eligible := duplicateSuiteKey(suite)
		if prior := executions[key]; eligible && prior != "" {
			redirect[suite.Name] = prior
			reasons[suite.Name] = "duplicate-command"
			continue
		}
		if eligible {
			executions[key] = suite.Name
		}
		kept = append(kept, suite)
	}
	return kept
}

func suiteAggregations(suites []policy.TestSuite, redirect, reasons map[string]string) []SuiteAggregation {
	aggregations := make([]SuiteAggregation, 0, len(redirect))
	for _, suite := range suites {
		if redirect[suite.Name] == "" {
			continue
		}
		executedBy := resolveSuiteExecution(redirect, redirect[suite.Name])
		aggregations = append(aggregations, SuiteAggregation{Suite: suite.Name, ExecutedBy: executedBy, Reason: reasons[suite.Name]})
	}
	sort.Slice(aggregations, func(left, right int) bool { return aggregations[left].Suite < aggregations[right].Suite })
	return aggregations
}

func duplicateSuiteKey(suite policy.TestSuite) (string, bool) {
	if len(suite.Argv) == 0 || len(suite.Artifacts) > 0 {
		return "", false
	}
	payload := struct {
		Argv               []string
		Cwd                string
		Scope              string
		Reusable           bool
		Modules            []string
		Paths              []string
		ExtraInputs        []string
		Environment        []string
		ExclusiveResources []string
		TimeoutSeconds     int
	}{
		suite.Argv, suite.Cwd, suite.Scope, suite.Reusable, suite.Modules, suite.Paths, suite.ExtraInputs,
		suite.Environment, suite.ExclusiveResources, suite.TimeoutSeconds,
	}
	data, err := json.Marshal(payload)
	return string(data), err == nil
}

func resolveSuiteExecution(redirect map[string]string, name string) string {
	for redirect[name] != "" {
		name = redirect[name]
	}
	return name
}

func BuildMergeDecision(repo repository.Repository, selection repository.Selection) (MergeDecision, error) {
	classification := repo.ClassifyDocumentationCandidate(selection)
	if classification.Ordinary {
		return MergeDecision{Level: MergeLevelDocumentation, Reasons: []string{classification.Reason}}, nil
	}
	advice, err := BuildAdvice(repo, selection)
	if err != nil {
		return MergeDecision{}, err
	}
	mergeGate := repo.Config.Verification.MergeGate
	if mergeGate == nil {
		return MergeDecision{
			Level: MergeLevelFull, Reasons: []string{"adaptive merge verification is not configured"},
		}, nil
	}
	if selection.All {
		return MergeDecision{Level: MergeLevelFull, Reasons: append([]string{}, advice.Reasons...)}, nil
	}
	allowed := map[string]bool{}
	for _, module := range mergeGate.RecommendedModules {
		allowed[module] = true
	}
	changedPaths := selection.Candidate.Paths()
	for _, path := range changedPaths {
		modules := repo.ModuleNames(path)
		if len(modules) != 1 {
			return MergeDecision{
				Level:   MergeLevelFull,
				Reasons: []string{fmt.Sprintf("changed path %q belongs to %d modules; recommended merge verification requires exactly one", path, len(modules))},
			}, nil
		}
		if !allowed[modules[0]] {
			return MergeDecision{
				Level:   MergeLevelFull,
				Reasons: []string{fmt.Sprintf("changed module %q is not configured for recommended merge verification", modules[0])},
			}, nil
		}
	}
	if advice.Suggested == MergeLevelFull {
		return MergeDecision{Level: MergeLevelFull, Reasons: append([]string{}, advice.Reasons...)}, nil
	}
	reasons := []string{}
	if len(changedPaths) > 0 {
		reasons = append(reasons, "every changed path belongs to exactly one module configured for recommended merge verification")
	}
	reasons = append(reasons, advice.Reasons...)
	return MergeDecision{Level: MergeLevelRecommended, Reasons: reasons}, nil
}

func explicitSuitePlan(suites []policy.TestSuite, names []string) (Plan, error) {
	selected := []policy.TestSuite{}
	for _, name := range names {
		index := slices.IndexFunc(suites, func(suite policy.TestSuite) bool { return suite.Name == name })
		if index < 0 {
			return Plan{}, fmt.Errorf("unknown test suite %q", name)
		}
		selected = append(selected, suites[index])
	}
	return Plan{Suites: uniqueSuites(selected)}, nil
}

func validateModuleNames(config policy.Config, names []string) error {
	for _, name := range names {
		if _, exists := config.ModuleByName[name]; !exists {
			return fmt.Errorf("unknown module %q", name)
		}
	}
	return nil
}

func suitesForProfile(suites []policy.TestSuite, profile string, impacted, changedPaths []string) []policy.TestSuite {
	selected := []policy.TestSuite{}
	for _, suite := range suites {
		if !slices.Contains(suite.RunOn, profile) {
			continue
		}
		moduleMatch := suite.Scope == "module" && intersects(suite.Modules, impacted)
		repositoryMatch := suite.Scope == "repository" && pathsMatch(changedPaths, suite.Paths)
		if moduleMatch || repositoryMatch {
			selected = append(selected, suite)
		}
	}
	return selected
}

func recommendScope(config policy.Config, selection repository.Selection, plan Plan) (string, []string) {
	changedPaths := analysisPaths(selection)
	if len(changedPaths) == 0 {
		return "recommended", []string{"no governed changes were detected, so no broader test run is warranted"}
	}
	if selection.All {
		return "full", []string{"impact expanded to the whole repository because this is a new repository or the change includes a deletion or policy, dependency, workflow, or container input"}
	}
	if len(changedPaths) >= 20 {
		return "full", []string{fmt.Sprintf("the change spans %d governed paths", len(changedPaths))}
	}
	moduleCount := len(config.Modules)
	if moduleCount >= 3 && len(plan.ImpactedModules)*3 >= moduleCount*2 {
		return "full", []string{fmt.Sprintf("the change impacts %d of %d modules through the dependency graph", len(plan.ImpactedModules), moduleCount)}
	}
	if len(plan.ChangedModules) >= 3 {
		return "full", []string{fmt.Sprintf("the change directly crosses %d module boundaries", len(plan.ChangedModules))}
	}
	return "recommended", []string{fmt.Sprintf("the change is limited to %d governed paths and impacts %d of %d modules", len(changedPaths), len(plan.ImpactedModules), moduleCount)}
}

func analysisPaths(selection repository.Selection) []string {
	return uniqueStrings(append(append([]string{}, selection.Files...), selection.Candidate.Deleted...))
}

func Run(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, plan Plan, reporter *ExecutionReporter) []policy.Finding {
	return RunWithEvidence(ctx, repo, commandRunner, plan, reporter).Findings
}

func RunUntilFailure(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, plan Plan, reporter *ExecutionReporter) []policy.Finding {
	return RunUntilFailureWithEvidence(ctx, repo, commandRunner, plan, reporter).Findings
}

func RunWithEvidence(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, plan Plan, reporter *ExecutionReporter) RunResult {
	return run(ctx, repo, commandRunner, plan, reporter, false)
}

func RunUntilFailureWithEvidence(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, plan Plan, reporter *ExecutionReporter) RunResult {
	return run(ctx, repo, commandRunner, plan, reporter, true)
}

func run(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, plan Plan, reporter *ExecutionReporter, stopAfterFailure bool) RunResult {
	runResult := RunResult{}
	var artifactExecution *testartifact.Execution
	reuse := suiteReuseController(commandRunner)
	for index, suite := range plan.Suites {
		runContext := suiteRunContext(ctx, reporter, index)
		execution := runOneSuite(runContext, repo.Root, commandRunner, suite, reuse, &artifactExecution)
		runResult.Executions = append(runResult.Executions, execution)
		reportSuiteExecution(reporter, index, execution)
		if execution.Failed() {
			runResult.Findings = append(runResult.Findings, policy.Finding{Check: "test." + suite.Kind, Path: "repository", Subject: suite.Name, Message: execution.FailureMessage})
			if stopAfterFailure {
				break
			}
		}
	}
	if artifactExecution != nil {
		if err := artifactExecution.Complete(); err != nil {
			runResult.Findings = append(runResult.Findings, policy.Finding{Check: "test.artifacts", Path: testartifact.RootName, Subject: artifactExecution.ID(), Message: err.Error()})
		}
	}
	return runResult
}

func suiteRunContext(ctx context.Context, reporter *ExecutionReporter, index int) context.Context {
	if reporter == nil {
		return ctx
	}
	reporter.CommandWaiting(index)
	return runner.WithResourceWaitObserver(ctx, func(elapsed time.Duration) {
		reporter.CommandResourceWaiting(index, elapsed)
	})
}

func runOneSuite(ctx context.Context, root string, commandRunner runner.Runner, suite policy.TestSuite, reuse SuiteReuseController, artifacts **testartifact.Execution) SuiteExecution {
	execution, reused, err := reusableSuiteExecution(reuse, suite, 1)
	if err != nil {
		return failedSuiteExecution(suite, 1, err)
	}
	if reused {
		return execution
	}
	if err := ensureRunArtifacts(root, commandRunner, artifacts); err != nil {
		return failedSuiteExecution(suite, 1, err)
	}
	executionRoot, cleanup, err := suiteExecutionRoot(root, commandRunner, suite)
	if err != nil {
		return failedSuiteExecution(suite, 1, err)
	}
	execution = executeSuite(ctx, executionRoot, commandRunner, suite, 1, *artifacts)
	finishSuiteExecution(&execution, cleanup(), reuse)
	return execution
}

func ensureRunArtifacts(root string, commandRunner runner.Runner, artifacts **testartifact.Execution) error {
	if *artifacts != nil || runnerManagesTestArtifacts(commandRunner) {
		return nil
	}
	execution, err := testartifact.Start(root, "")
	*artifacts = execution
	return err
}

func suiteExecutionRoot(root string, commandRunner runner.Runner, suite policy.TestSuite) (string, func() error, error) {
	if !suite.Reusable {
		return root, func() error { return nil }, nil
	}
	controller, available := commandRunner.(SuiteExecutionViewController)
	if !available {
		return "", nil, errors.New("reusable suite execution view is unavailable")
	}
	return controller.PrepareSuiteView(suite)
}

func finishSuiteExecution(execution *SuiteExecution, cleanupErr error, reuse SuiteReuseController) {
	if cleanupErr != nil {
		execution.FailureCategory = runner.FailureOperational
		execution.FailureMessage = errors.Join(errorFromMessage(execution.FailureMessage), cleanupErr).Error()
	}
	if execution.Suite.Reusable && !execution.Failed() && reuse != nil {
		if err := reuse.RecordSuite(*execution); err != nil {
			execution.FailureCategory = runner.FailureOperational
			execution.FailureMessage = err.Error()
		}
	}
}

func errorFromMessage(message string) error {
	if message == "" {
		return nil
	}
	return errors.New(message)
}

func reportSuiteExecution(reporter *ExecutionReporter, index int, execution SuiteExecution) {
	if reporter == nil {
		return
	}
	status := executionStatusPassed
	if execution.Failed() {
		status = executionStatusFailed
	}
	reporter.CommandFinished(index, ExecutionResult{
		Status: status, ExecutionDuration: execution.Result.ExecutionDuration,
		ResourceWait: execution.Result.ResourceWait, ResourceWaitKnown: execution.ResultKnown,
	})
}

func reusableSuiteExecution(controller SuiteReuseController, suite policy.TestSuite, attempt int) (SuiteExecution, bool, error) {
	if controller == nil || !suite.Reusable {
		return SuiteExecution{}, false, nil
	}
	execution, reusable, err := controller.ReuseSuite(suite, attempt)
	if err != nil || !reusable {
		return SuiteExecution{}, reusable, err
	}
	execution.Suite = cloneSuite(suite)
	execution.Attempt = attempt
	execution.Reused = true
	return execution, true, nil
}

func suiteReuseController(commandRunner runner.Runner) SuiteReuseController {
	controller, _ := commandRunner.(SuiteReuseController)
	return controller
}

func ExecuteSuite(ctx context.Context, root string, commandRunner runner.Runner, suite policy.TestSuite, attempt int) SuiteExecution {
	if runnerManagesTestArtifacts(commandRunner) {
		return executeSuite(ctx, root, commandRunner, suite, attempt, nil)
	}
	execution, err := testartifact.Start(root, "")
	if err != nil {
		return failedSuiteExecution(suite, attempt, err)
	}
	result := executeSuite(ctx, root, commandRunner, suite, attempt, execution)
	if completeErr := execution.Complete(); completeErr != nil {
		result.FailureCategory = runner.FailureOperational
		if result.FailureMessage == "" {
			result.FailureMessage = completeErr.Error()
		} else {
			result.FailureMessage = errors.Join(errors.New(result.FailureMessage), completeErr).Error()
		}
	}
	return result
}

func executeSuite(ctx context.Context, root string, commandRunner runner.Runner, suite policy.TestSuite, attempt int, artifactExecution *testartifact.Execution) SuiteExecution {
	command := suiteCommand(suite)
	if artifactExecution != nil {
		prepared, err := artifactExecution.PrepareCommand(suite, command)
		if err != nil {
			return failedSuiteExecution(suite, attempt, err)
		}
		command = prepared
	}
	result, resultKnown, err := runSuite(ctx, root, commandRunner, command)
	artifacts := []testartifact.Record{}
	if artifactExecution != nil {
		var artifactErr error
		artifacts, artifactErr = artifactExecution.ValidateCommand(command)
		err = errors.Join(err, artifactErr)
	} else if provider, ok := commandRunner.(interface {
		TestArtifacts(string, int) []testartifact.Record
	}); ok {
		artifacts = provider.TestArtifacts(suite.Name, attempt)
	}
	category := runner.FailureCategoryFor(ctx, result, err)
	if err != nil {
		result.FailureCategory = category
	}
	execution := SuiteExecution{
		Suite: cloneSuite(suite), Result: result, ResultKnown: resultKnown,
		FailureCategory: category, Attempt: attempt,
		Artifacts: append([]testartifact.Record{}, artifacts...),
	}
	if err != nil {
		execution.FailureMessage = err.Error()
	}
	return execution
}

func failedSuiteExecution(suite policy.TestSuite, attempt int, err error) SuiteExecution {
	return SuiteExecution{
		Suite: cloneSuite(suite), Result: runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational},
		ResultKnown: true, FailureCategory: runner.FailureOperational, FailureMessage: err.Error(), Attempt: attempt,
	}
}

func runnerManagesTestArtifacts(commandRunner runner.Runner) bool {
	managed, ok := commandRunner.(interface{ ManagesTestArtifacts() bool })
	return ok && managed.ManagesTestArtifacts()
}

func suiteCommand(suite policy.TestSuite) policy.Command {
	return policy.Command{
		Name: suite.Name, Argv: append([]string{}, suite.Argv...), Cwd: suite.Cwd,
		Paths: append([]string{}, suite.Paths...), Modules: append([]string{}, suite.Modules...),
		Environment: append([]string{}, suite.Environment...), ExclusiveResources: append([]string{}, suite.ExclusiveResources...),
		TimeoutSeconds:    suite.TimeoutSeconds,
		SealedEnvironment: suite.Reusable,
		TestArtifacts:     append([]policy.TestArtifact{}, suite.Artifacts...),
	}
}

func cloneSuite(suite policy.TestSuite) policy.TestSuite {
	suite.Modules = append([]string{}, suite.Modules...)
	suite.Argv = append([]string{}, suite.Argv...)
	suite.Paths = append([]string{}, suite.Paths...)
	suite.ExtraInputs = append([]string{}, suite.ExtraInputs...)
	suite.Covers = append([]string{}, suite.Covers...)
	suite.RunOn = append([]string{}, suite.RunOn...)
	suite.Environment = append([]string{}, suite.Environment...)
	suite.ExclusiveResources = append([]string{}, suite.ExclusiveResources...)
	suite.Artifacts = append([]policy.TestArtifact{}, suite.Artifacts...)
	return suite
}

type resultRunner interface {
	RunWithResult(context.Context, string, policy.Command) (runner.Result, error)
}

func runSuite(ctx context.Context, root string, commandRunner runner.Runner, command policy.Command) (runner.Result, bool, error) {
	if observed, ok := commandRunner.(resultRunner); ok {
		result, err := observed.RunWithResult(ctx, root, command)
		return result, true, err
	}
	started := time.Now()
	err := commandRunner.Run(ctx, root, command)
	return runner.Result{ExecutionDuration: time.Since(started)}, false, err
}

func ExecutionPlanForDirectTest(plan Plan) ExecutionPlan {
	commands := make([]ExecutionCommand, 0, len(plan.Suites))
	for _, suite := range plan.Suites {
		commands = append(commands, ExecutionCommand{
			Name: suite.Name, Kind: suite.Kind, Scope: suite.Scope, Cost: suite.Cost,
			Argv: append([]string{}, suite.Argv...), Cwd: suite.Cwd,
			ExclusiveResources: append([]string{}, suite.ExclusiveResources...), TimeoutSeconds: suite.TimeoutSeconds,
		})
	}
	trustedBase := plan.SelectionBase
	if trustedBase == "" {
		trustedBase = "not-applicable"
	}
	return ExecutionPlan{
		Stage: "direct", Level: plan.Level, TrustedBase: trustedBase, Candidate: "working-tree",
		Reasons: append([]string{}, plan.Reasons...), ImpactedModules: append([]string{}, plan.ImpactedModules...), Commands: commands,
	}
}

func NewDirectExecutionReporter(output io.Writer, plan Plan, verbose bool) *ExecutionReporter {
	return newExecutionReporter(output, ExecutionPlanForDirectTest(plan), verbose)
}

func CoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	config := repo.Config
	findings := focusedCoverageFindings(config)
	findings = append(findings, fullRepositoryCoverageFindings(config.Tests.Suites)...)
	findings = append(findings, capabilityCoverageFindings(config)...)
	findings = append(findings, conditionalModuleCoverageFindings(config)...)
	findings = append(findings, requiredKindFindings(config)...)
	findings = append(findings, requiredSupplementalKindFindings(config)...)
	findings = append(findings, gherkinCoverageFindings(config.Tests.Suites, files)...)
	findings = append(findings, testOwnershipFindings(repo, files)...)
	return findings
}

func testOwnershipFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range files {
		if !repo.IsExecutableSource(path) || !repo.IsTest(path) {
			continue
		}
		owners := repo.ModuleNames(path)
		if len(owners) != 1 {
			findings = append(findings, policy.Finding{
				Check: "policy.testOwnership", Path: path, Subject: "module",
				Message: fmt.Sprintf("governed test source belongs to %d modules; exactly one is required", len(owners)),
			})
			continue
		}
		if !focusedSuiteOwnsTest(repo.Config, owners[0], path) {
			findings = append(findings, policy.Finding{
				Check: "policy.testOwnership", Path: path, Subject: owners[0],
				Message: "no quick focused suite for the owning module includes this test path",
			})
		}
	}
	return findings
}

func focusedSuiteOwnsTest(config policy.Config, module, path string) bool {
	moduleIndex, found := config.ModuleByName[module]
	if !found {
		return false
	}
	for _, suite := range config.Tests.Suites {
		if suite.Scope != "module" || suite.Cost != "quick" || !slices.Contains(suite.RunOn, "focused") ||
			!slices.Contains(suite.Modules, module) {
			continue
		}
		patterns := suite.Paths
		if len(patterns) == 0 {
			patterns = config.Modules[moduleIndex].Paths
		}
		if policy.MatchesAny(path, patterns) {
			return true
		}
	}
	return false
}

func Notes(_ repository.Repository, files []string) []string {
	notes := []string{}
	if hasGherkinFile(files) {
		notes = append(notes, "conditional executable-specification policy: governed .feature files")
	}
	return notes
}

func focusedCoverageFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, module := range config.Modules {
		if !moduleHasFocusedSuite(config.Tests.Suites, module.Name) {
			findings = append(findings, policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: module.Name, Message: "module has no focused boundary test suite"})
		}
	}
	return findings
}

func moduleHasFocusedSuite(suites []policy.TestSuite, module string) bool {
	for _, suite := range suites {
		if suite.Scope == "module" && slices.Contains(suite.RunOn, "focused") && slices.Contains(suite.Modules, module) {
			return true
		}
	}
	return false
}

func fullRepositoryCoverageFindings(suites []policy.TestSuite) []policy.Finding {
	for _, suite := range suites {
		if suite.Scope == "repository" && slices.Contains(suite.RunOn, "full") {
			return nil
		}
	}
	return []policy.Finding{{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: "full", Message: "at least one repository-scoped full suite is required"}}
}

func capabilityCoverageFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, capability := range config.Project.Capabilities {
		kinds, expected := capabilityRequirement(capability)
		if len(kinds) > 0 && !hasFullRepositoryKind(config.Tests.Suites, kinds...) {
			findings = append(findings, capabilityFinding(capability, expected))
		}
	}
	return findings
}

func conditionalModuleCoverageFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, module := range config.ActivePolicyModules {
		var kinds []string
		var expected string
		switch module.Name {
		case "react":
			kinds, expected = []string{"component", "browser", "e2e"}, "component, browser, or e2e"
		case "electron":
			kinds, expected = []string{"electron", "browser", "e2e"}, "Electron, browser, or e2e"
		default:
			continue
		}
		if !hasFullRepositoryKind(config.Tests.Suites, kinds...) {
			subject := module.Name + ":" + module.Root
			message := fmt.Sprintf("detected %s policy at %s requires a full repository-scoped %s suite", module.Name, module.Root, expected)
			findings = append(findings, policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: subject, Message: message})
		}
	}
	return findings
}

func hasFullRepositoryKind(suites []policy.TestSuite, kinds ...string) bool {
	for _, suite := range suites {
		if suite.Scope == "repository" && slices.Contains(kinds, suite.Kind) && slices.Contains(suite.RunOn, "full") {
			return true
		}
	}
	return false
}

func capabilityRequirement(capability string) ([]string, string) {
	requirements := map[string]struct {
		kinds    []string
		expected string
	}{
		"backend":  {[]string{"integration", "contract", "e2e"}, "integration, contract, or e2e"},
		"frontend": {[]string{"component", "browser", "e2e"}, "component, browser, or e2e"},
		"ui":       {[]string{"browser", "e2e"}, "browser or e2e"},
		"visual":   {[]string{"visual"}, "visual"},
		"content":  {[]string{"content", "contract"}, "content or contract"},
	}
	requirement := requirements[capability]
	return requirement.kinds, requirement.expected
}

func requiredKindFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, kind := range uniqueStrings(config.Tests.RequiredKinds) {
		if !hasFullKind(config.Tests.Suites, kind) {
			findings = append(findings, policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: kind, Message: fmt.Sprintf("required test kind %q has no full suite", kind)})
		}
	}
	return findings
}

func requiredSupplementalKindFindings(config policy.Config) []policy.Finding {
	findings := []policy.Finding{}
	for _, kind := range uniqueStrings(config.Tests.RequiredSupplementalKinds) {
		if !hasSupplementalKind(config.Tests.Suites, kind) {
			message := fmt.Sprintf("required supplemental test kind %q has no supplemental suite", kind)
			findings = append(findings, policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: kind, Message: message})
		}
	}
	return findings
}

func gherkinCoverageFindings(suites []policy.TestSuite, files []string) []policy.Finding {
	if !hasGherkinFile(files) {
		return nil
	}
	findings := []policy.Finding{}
	if !hasFullRepositoryKind(suites, "acceptance", "gherkin") {
		findings = append(findings, policy.Finding{Check: "policy.testStrength", Path: policy.ConfigFilename, Subject: "gherkin:acceptance", Message: "governed .feature files must generate and execute a full repository acceptance suite"})
	}
	return findings
}

func hasGherkinFile(files []string) bool {
	for _, path := range files {
		if strings.EqualFold(filepath.Ext(path), ".feature") {
			return true
		}
	}
	return false
}

func hasSupplementalKind(suites []policy.TestSuite, kinds ...string) bool {
	for _, suite := range suites {
		if slices.Contains(kinds, suite.Kind) && slices.Contains(suite.RunOn, "supplemental") {
			return true
		}
	}
	return false
}

func capabilityFinding(capability, expected string) policy.Finding {
	return policy.Finding{Check: "policy.testCoverage", Path: policy.ConfigFilename, Subject: capability, Message: fmt.Sprintf("project capability %q requires a full %s suite", capability, expected)}
}

func hasFullKind(suites []policy.TestSuite, kinds ...string) bool {
	for _, suite := range suites {
		if slices.Contains(kinds, suite.Kind) && slices.Contains(suite.RunOn, "full") {
			return true
		}
	}
	return false
}

func pathsMatch(paths, patterns []string) bool {
	if len(patterns) == 0 {
		return len(paths) > 0
	}
	for _, path := range paths {
		if policy.MatchesAny(path, patterns) {
			return true
		}
	}
	return false
}

func intersects(left, right []string) bool {
	for _, value := range left {
		if slices.Contains(right, value) {
			return true
		}
	}
	return false
}

func selectByRunOn(suites []policy.TestSuite, profile string) []policy.TestSuite {
	selected := []policy.TestSuite{}
	for _, suite := range suites {
		if slices.Contains(suite.RunOn, profile) {
			selected = append(selected, suite)
		}
	}
	return selected
}

func uniqueSuites(suites []policy.TestSuite) []policy.TestSuite {
	result := []policy.TestSuite{}
	seen := map[string]bool{}
	for _, suite := range suites {
		if !seen[suite.Name] {
			seen[suite.Name] = true
			result = append(result, suite)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
