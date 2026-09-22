package pack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/schema"
)

type ConformanceOptions struct {
	LedgerPath           string
	ReferenceExecutable  string
	ReferencePolicyRoot  string
	CandidateExecutable  string
	CandidatePolicyRoot  string
	CandidatePackSources []string
}

type ConformanceReport struct {
	Schema         string                         `json:"$schema"`
	Protocol       string                         `json:"protocol"`
	Ledger         ConformanceEvidenceIdentity    `json:"ledger"`
	Reference      ConformanceExecutableIdentity  `json:"reference"`
	Candidate      ConformanceExecutableIdentity  `json:"candidate"`
	CandidatePacks []ConformancePackIdentity      `json:"candidatePacks"`
	Environment    ConformanceEnvironmentIdentity `json:"environment"`
	Fixtures       []ConformanceFixtureEvidence   `json:"fixtures"`
	Summary        ConformanceReportSummary       `json:"summary"`
}

type ConformanceEvidenceIdentity struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ConformanceExecutableIdentity struct {
	Path       string `json:"path"`
	PolicyRoot string `json:"policyRoot"`
	SHA256     string `json:"sha256"`
}

type ConformancePackIdentity struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type ConformanceEnvironmentIdentity struct {
	Platform string                  `json:"platform"`
	Git      ConformanceToolIdentity `json:"git"`
}

type ConformanceToolIdentity struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

type ConformanceFixtureEvidence struct {
	ID                string                  `json:"id"`
	BehaviorIDs       []string                `json:"behaviorIds"`
	Status            string                  `json:"status"`
	Reason            string                  `json:"reason,omitempty"`
	Reference         *ConformanceRunEvidence `json:"reference,omitempty"`
	Candidate         *ConformanceRunEvidence `json:"candidate,omitempty"`
	Differences       []ConformanceDifference `json:"differences,omitempty"`
	AssertionFailures []string                `json:"assertionFailures,omitempty"`
}

type ConformanceRunEvidence struct {
	ExitStatus int                       `json:"exitStatus"`
	Report     json.RawMessage           `json:"report"`
	Stderr     string                    `json:"stderr,omitempty"`
	Before     []ConformanceFileIdentity `json:"before"`
	After      []ConformanceFileIdentity `json:"after"`
	BeforeGit  ConformanceGitIdentity    `json:"beforeGit"`
	AfterGit   ConformanceGitIdentity    `json:"afterGit"`
}

type ConformanceGitIdentity struct {
	Head            string   `json:"head"`
	Branch          string   `json:"branch"`
	IndexDiffSHA256 string   `json:"indexDiffSha256"`
	Status          []string `json:"status"`
}

type ConformanceFileIdentity struct {
	Path   string `json:"path"`
	Mode   string `json:"mode"`
	SHA256 string `json:"sha256"`
}

type ConformanceReportSummary struct {
	Status    string                     `json:"status"`
	Passed    int                        `json:"passed"`
	Failed    int                        `json:"failed"`
	Skipped   int                        `json:"skipped"`
	Blocked   int                        `json:"blocked"`
	Behaviors ConformanceBehaviorSummary `json:"behaviors"`
}

type ConformanceBehaviorSummary struct {
	Passing             int `json:"passing"`
	Untested            int `json:"untested"`
	Failing             int `json:"failing"`
	Blocked             int `json:"blocked"`
	DeliberatelyChanged int `json:"deliberatelyChanged"`
}

type conformanceExecution struct {
	ExitStatus int
	Stdout     []byte
	Stderr     []byte
}

type conformanceExecutor interface {
	Run(context.Context, string, string, string, []string, int, []string) (conformanceExecution, error)
}

type osConformanceExecutor struct{}

func RunConformance(ctx context.Context, options ConformanceOptions) (ConformanceReport, error) {
	return runConformance(ctx, options, osConformanceExecutor{})
}

func runConformance(ctx context.Context, options ConformanceOptions, executor conformanceExecutor) (ConformanceReport, error) {
	ledger, err := LoadConformanceLedger(options.LedgerPath)
	if err != nil {
		return ConformanceReport{}, err
	}
	reference, err := conformanceExecutableIdentity(options.ReferenceExecutable, options.ReferencePolicyRoot)
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("reference executable: %w", err)
	}
	candidate, err := conformanceExecutableIdentity(options.CandidateExecutable, options.CandidatePolicyRoot)
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("candidate executable: %w", err)
	}
	git, err := conformanceGitTool(ctx)
	if err != nil {
		return ConformanceReport{}, fmt.Errorf("git tool: %w", err)
	}
	environmentRoot, err := conformanceTemporary("code-polishy-conformance-environment-")
	if err != nil {
		return ConformanceReport{}, err
	}
	defer func() {
		makeWritable(environmentRoot)
		_ = os.RemoveAll(environmentRoot)
	}()
	referenceEnvironment := conformanceDataEnvironment(filepath.Join(environmentRoot, "reference"))
	candidateHome := filepath.Join(environmentRoot, "candidate")
	candidateEnvironment := conformanceDataEnvironment(candidateHome)
	candidatePacks, err := installConformancePacks(options.CandidatePackSources, conformanceDataRoot(candidateHome), candidate.PolicyRoot)
	if err != nil {
		return ConformanceReport{}, err
	}
	report := ConformanceReport{
		Schema:         ConformanceReportSchema,
		Protocol:       ConformanceReportProtocol,
		Ledger:         ConformanceEvidenceIdentity{Path: filepath.ToSlash(ledger.Path), SHA256: ledger.SHA256},
		Reference:      reference,
		Candidate:      candidate,
		CandidatePacks: candidatePacks,
		Environment:    ConformanceEnvironmentIdentity{Platform: CurrentPlatform(), Git: git},
		Fixtures:       []ConformanceFixtureEvidence{},
		Summary:        ConformanceReportSummary{Status: "passed"},
	}
	for _, fixture := range ledger.Fixtures {
		if fixture.Maturity == "planned" {
			report.Fixtures = append(report.Fixtures, ConformanceFixtureEvidence{ID: fixture.ID, BehaviorIDs: slices.Clone(fixture.BehaviorIDs), Status: "blocked", Reason: fixture.Gap})
			report.Summary.Blocked++
			report.Summary.Status = "failed"
			continue
		}
		if !slices.Contains(fixture.Platforms, CurrentPlatform()) {
			report.Fixtures = append(report.Fixtures, ConformanceFixtureEvidence{ID: fixture.ID, BehaviorIDs: slices.Clone(fixture.BehaviorIDs), Status: "skipped", Reason: "platform " + CurrentPlatform() + " is not declared by the fixture"})
			report.Summary.Skipped++
			continue
		}
		evidence, runErr := runConformanceFixture(
			ctx,
			fixture,
			reference.Path,
			reference.PolicyRoot,
			conformanceDataRoot(filepath.Join(environmentRoot, "reference")),
			candidate.Path,
			candidate.PolicyRoot,
			conformanceDataRoot(candidateHome),
			git.Path,
			referenceEnvironment,
			candidateEnvironment,
			executor,
		)
		if runErr != nil {
			return ConformanceReport{}, fmt.Errorf("fixture %s: %w", fixture.ID, runErr)
		}
		report.Fixtures = append(report.Fixtures, evidence)
		if evidence.Status == "passed" {
			report.Summary.Passed++
		} else {
			report.Summary.Failed++
			report.Summary.Status = "failed"
		}
	}
	for _, behavior := range ledger.Behaviors {
		switch behavior.Status {
		case "passing":
			report.Summary.Behaviors.Passing++
		case "deliberately-changed":
			report.Summary.Behaviors.DeliberatelyChanged++
		case "untested":
			report.Summary.Behaviors.Untested++
			report.Summary.Status = "failed"
		case "failing":
			report.Summary.Behaviors.Failing++
			report.Summary.Status = "failed"
		case "blocked":
			report.Summary.Behaviors.Blocked++
			report.Summary.Status = "failed"
		}
	}
	data, err := json.Marshal(report)
	if err != nil {
		return ConformanceReport{}, err
	}
	if err := schema.NewValidator(ConformanceReportSchema).Validate(data); err != nil {
		return ConformanceReport{}, fmt.Errorf("validate conformance report: %w", err)
	}
	return report, nil
}

func installConformancePacks(sources []string, dataRoot, policyRoot string) ([]ConformancePackIdentity, error) {
	versionData, err := os.ReadFile(filepath.Join(policyRoot, "VERSION"))
	if err != nil {
		return nil, fmt.Errorf("candidate policy version: %w", err)
	}
	engineVersion := strings.TrimSpace(string(versionData))
	identities := make([]ConformancePackIdentity, 0, len(sources))
	seen := map[string]bool{}
	for index, source := range sources {
		identity, _, err := Install(source, dataRoot, engineVersion)
		if err != nil {
			return nil, fmt.Errorf("candidate pack source %d: %w", index, err)
		}
		if seen[identity.Name] {
			return nil, fmt.Errorf("candidate pack source %d repeats pack %s", index, identity.Name)
		}
		seen[identity.Name] = true
		identities = append(identities, ConformancePackIdentity(identity))
	}
	slices.SortFunc(identities, func(left, right ConformancePackIdentity) int { return strings.Compare(left.Name, right.Name) })
	return identities, nil
}

func conformanceDataEnvironment(root string) []string {
	if runtime.GOOS == "windows" {
		return []string{"LOCALAPPDATA=" + root}
	}
	return []string{"XDG_DATA_HOME=" + root}
}

func conformanceDataRoot(root string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(root, "CodePolishy", "packs")
	}
	return filepath.Join(root, "code-polishy", "packs")
}

func conformanceTemporary(pattern string) (string, error) {
	root, err := os.MkdirTemp("", pattern)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		_ = os.RemoveAll(root)
		return "", err
	}
	return canonical, nil
}

func conformanceExecutableIdentity(name, requestedPolicyRoot string) (ConformanceExecutableIdentity, error) {
	canonical, digest, err := conformanceExecutablePathAndDigest(name)
	if err != nil {
		return ConformanceExecutableIdentity{}, err
	}
	policyRoot, err := resolveConformancePolicyRoot(canonical, requestedPolicyRoot)
	if err != nil {
		return ConformanceExecutableIdentity{}, err
	}
	return ConformanceExecutableIdentity{Path: canonical, PolicyRoot: policyRoot, SHA256: digest}, nil
}

func resolveConformancePolicyRoot(executable, requested string) (string, error) {
	if strings.TrimSpace(requested) == "" {
		return conformancePolicyRoot(executable)
	}
	root, err := canonicalDirectory(requested)
	if err != nil {
		return "", err
	}
	version, versionErr := os.Stat(filepath.Join(root, "VERSION"))
	configuration, configurationErr := os.Stat(filepath.Join(root, "schema", "code-polishy.schema.json"))
	if versionErr != nil || configurationErr != nil || !version.Mode().IsRegular() || !configuration.Mode().IsRegular() {
		return "", errors.New("policy root must contain regular VERSION and schema/code-polishy.schema.json files")
	}
	return root, nil
}

func conformanceExecutablePathAndDigest(name string) (string, string, error) {
	if strings.TrimSpace(name) == "" {
		return "", "", errors.New("path is required")
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", "", err
	}
	file, err := os.Open(canonical)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", "", errors.New("path must resolve to an executable regular file")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(file, 512<<20)); err != nil {
		return "", "", err
	}
	if info.Size() > 512<<20 {
		return "", "", errors.New("executable exceeds 512 MiB")
	}
	return canonical, hex.EncodeToString(digest.Sum(nil)), nil
}

func conformancePolicyRoot(executable string) (string, error) {
	current := filepath.Dir(executable)
	for {
		version, versionErr := os.Stat(filepath.Join(current, "VERSION"))
		configuration, configurationErr := os.Stat(filepath.Join(current, "schema", "code-polishy.schema.json"))
		if versionErr == nil && configurationErr == nil && version.Mode().IsRegular() && configuration.Mode().IsRegular() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("executable is not contained by a Code Polishy policy root")
		}
		current = parent
	}
}

func runConformanceFixture(ctx context.Context, fixture ConformanceFixture, referenceExecutable, referencePolicyRoot, referenceDataRoot, candidateExecutable, candidatePolicyRoot, candidateDataRoot, gitExecutable string, referenceEnvironment, candidateEnvironment []string, executor conformanceExecutor) (ConformanceFixtureEvidence, error) {
	temporary, err := conformanceTemporary("code-polishy-conformance-")
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	defer os.RemoveAll(temporary)
	referenceRoot := filepath.Join(temporary, "reference")
	candidateRoot := filepath.Join(temporary, "candidate")
	if err := materializeConformanceFixture(ctx, referenceRoot, fixture, "reference", gitExecutable); err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	if err := materializeConformanceFixture(ctx, candidateRoot, fixture, "candidate", gitExecutable); err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	referenceBefore, err := conformanceSnapshot(referenceRoot)
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	candidateBefore, err := conformanceSnapshot(candidateRoot)
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	variantPaths := conformanceLaneOverridePaths(fixture.LaneOverrides)
	if !equivalentConformanceSnapshots(referenceBefore, candidateBefore, variantPaths) {
		return ConformanceFixtureEvidence{}, errors.New("materialized repositories differ outside declared lane overrides")
	}
	referenceBeforeGit, err := conformanceGitSnapshot(ctx, gitExecutable, referenceRoot)
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	candidateBeforeGit, err := conformanceGitSnapshot(ctx, gitExecutable, candidateRoot)
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	if !equivalentConformanceGit(referenceBeforeGit, candidateBeforeGit, len(variantPaths) > 0) {
		return ConformanceFixtureEvidence{}, errors.New("materialized Git repositories differ outside declared lane overrides")
	}
	referenceRun, err := executeConformanceLane(ctx, fixture, referenceExecutable, referencePolicyRoot, gitExecutable, referenceRoot, referenceBefore, referenceBeforeGit, referenceEnvironment, executor)
	if err != nil {
		return ConformanceFixtureEvidence{}, fmt.Errorf("reference: %w", err)
	}
	candidateRun, err := executeConformanceLane(ctx, fixture, candidateExecutable, candidatePolicyRoot, gitExecutable, candidateRoot, candidateBefore, candidateBeforeGit, candidateEnvironment, executor)
	if err != nil {
		return ConformanceFixtureEvidence{}, fmt.Errorf("candidate: %w", err)
	}
	differences, err := compareConformanceRuns(
		referenceRun,
		conformanceComparisonRoots{repository: referenceRoot, policy: referencePolicyRoot, data: referenceDataRoot},
		candidateRun,
		conformanceComparisonRoots{repository: candidateRoot, policy: candidatePolicyRoot, data: candidateDataRoot},
		variantPaths,
	)
	if err != nil {
		return ConformanceFixtureEvidence{}, err
	}
	referenceExpected, candidateExpected := conformanceLaneExpectations(fixture.Expected)
	failures := assertConformanceOutcome("reference", referenceExpected, referenceRun)
	failures = append(failures, assertConformanceOutcome("candidate", candidateExpected, candidateRun)...)
	failures = append(failures, assertConformanceDifferences(fixture.AcceptedDifferences, differences)...)
	evidence := ConformanceFixtureEvidence{
		ID:                fixture.ID,
		BehaviorIDs:       slices.Clone(fixture.BehaviorIDs),
		Status:            "passed",
		Reference:         &referenceRun,
		Candidate:         &candidateRun,
		Differences:       differences,
		AssertionFailures: failures,
	}
	if len(failures) != 0 {
		evidence.Status = "failed"
	}
	return evidence, nil
}

func conformanceLaneExpectations(expected ConformanceExpectedOutcome) (ConformanceExpectedOutcome, ConformanceExpectedOutcome) {
	candidate := expected
	candidate.Candidate = nil
	reference := candidate
	if expected.Candidate != nil {
		candidate = *expected.Candidate
		candidate.Candidate = nil
	}
	return reference, candidate
}

func assertConformanceDifferences(expected, actual []ConformanceDifference) []string {
	failures := []string{}
	expectedByPath := make(map[string]ConformanceDifference, len(expected))
	for _, difference := range expected {
		expectedByPath[difference.Path] = difference
	}
	seen := make(map[string]bool, len(actual))
	for _, difference := range actual {
		declared, exists := expectedByPath[difference.Path]
		if !exists {
			failures = append(failures, fmt.Sprintf("comparison[%s]: difference was not declared", difference.Path))
			continue
		}
		seen[difference.Path] = true
		if difference.Reference != declared.Reference || difference.Candidate != declared.Candidate {
			failures = append(failures, fmt.Sprintf("comparison[%s]: values did not match the declared difference", difference.Path))
		}
	}
	for _, difference := range expected {
		if !seen[difference.Path] {
			failures = append(failures, fmt.Sprintf("acceptedDifferences[%s]: declared difference was absent", difference.Path))
		}
	}
	return failures
}

func conformanceLaneOverridePaths(overrides *ConformanceLaneOverrides) map[string]bool {
	paths := map[string]bool{}
	if overrides == nil {
		return paths
	}
	for _, file := range overrides.Reference {
		paths[file.Path] = true
	}
	return paths
}

func equivalentConformanceSnapshots(reference, candidate []ConformanceFileIdentity, variants map[string]bool) bool {
	if len(reference) != len(candidate) {
		return false
	}
	for index := range reference {
		if reference[index].Path != candidate[index].Path || reference[index].Mode != candidate[index].Mode {
			return false
		}
		if !variants[reference[index].Path] && reference[index].SHA256 != candidate[index].SHA256 {
			return false
		}
	}
	return true
}

func equivalentConformanceGit(reference, candidate ConformanceGitIdentity, variant bool) bool {
	if variant {
		reference.Head = ""
		candidate.Head = ""
	}
	return reflect.DeepEqual(reference, candidate)
}

func executeConformanceLane(ctx context.Context, fixture ConformanceFixture, executable, policyRoot, gitExecutable, root string, before []ConformanceFileIdentity, beforeGit ConformanceGitIdentity, environment []string, executor conformanceExecutor) (ConformanceRunEvidence, error) {
	execution, err := executor.Run(ctx, executable, policyRoot, root, slices.Clone(fixture.Arguments), fixture.TimeoutSeconds, environment)
	if err != nil {
		return ConformanceRunEvidence{}, err
	}
	if len(execution.Stdout) == 0 || len(execution.Stdout) > 8<<20 {
		stderr := string(execution.Stderr)
		if len(stderr) > maximumConformanceDifferenceBytes {
			stderr = stderr[:maximumConformanceDifferenceBytes] + "..."
		}
		return ConformanceRunEvidence{}, fmt.Errorf("CLI report is empty or exceeds 8 MiB (exit %d, stderr %q)", execution.ExitStatus, stderr)
	}
	if err := schema.NewValidator("https://code-polishy.dev/schema/code-polishy-report.schema.json").Validate(execution.Stdout); err != nil {
		return ConformanceRunEvidence{}, fmt.Errorf("invalid CLI report: %w", err)
	}
	after, err := conformanceSnapshot(root)
	if err != nil {
		return ConformanceRunEvidence{}, err
	}
	afterGit, err := conformanceGitSnapshot(ctx, gitExecutable, root)
	if err != nil {
		return ConformanceRunEvidence{}, err
	}
	return ConformanceRunEvidence{
		ExitStatus: execution.ExitStatus,
		Report:     append(json.RawMessage(nil), execution.Stdout...),
		Stderr:     string(execution.Stderr),
		Before:     before,
		After:      after,
		BeforeGit:  beforeGit,
		AfterGit:   afterGit,
	}, nil
}

func (osConformanceExecutor) Run(ctx context.Context, executable, policyRoot, root string, arguments []string, timeout int, environment []string) (conformanceExecution, error) {
	command := policy.Command{
		Name:                 "language-conformance",
		Argv:                 append([]string{executable, "--repo-root", root, "--policy-root", policyRoot}, arguments...),
		Cwd:                  ".",
		TimeoutSeconds:       timeout,
		ExclusiveResources:   []string{},
		EnvironmentOverrides: slices.Clone(environment),
	}
	result, output, err := (runner.OSRunner{}).RunStructured(ctx, root, command)
	if err != nil && result.FailureCategory != runner.FailureCommandExit {
		return conformanceExecution{}, err
	}
	return conformanceExecution{ExitStatus: result.ExitStatus, Stdout: output.Stdout, Stderr: output.Stderr}, nil
}

func conformanceSnapshot(root string) ([]ConformanceFileIdentity, error) {
	files := []ConformanceFileIdentity{}
	total := int64(0)
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative == ".git" || relative == ".code-polishy-reports" || relative == ".code-polishy-artifacts" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("conformance repository contains a non-regular file at %s", relative)
		}
		total += info.Size()
		if len(files) >= maximumConformanceFiles || total > maximumConformanceFixtureBytes {
			return errors.New("conformance repository exceeds its file or byte limit")
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(data)
		files = append(files, ConformanceFileIdentity{Path: relative, Mode: fmt.Sprintf("%04o", info.Mode().Perm()), SHA256: hex.EncodeToString(digest[:])})
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(left, right ConformanceFileIdentity) int { return strings.Compare(left.Path, right.Path) })
	return files, nil
}

func assertConformanceOutcome(lane string, expected ConformanceExpectedOutcome, run ConformanceRunEvidence) []string {
	failures := []string{}
	if run.AfterGit.Head != run.BeforeGit.Head || run.AfterGit.Branch != run.BeforeGit.Branch || run.AfterGit.IndexDiffSHA256 != run.BeforeGit.IndexDiffSHA256 {
		failures = append(failures, lane+" Git: command changed HEAD, branch, or index")
	}
	if run.ExitStatus != expected.ExitStatus {
		failures = append(failures, fmt.Sprintf("%s exitStatus: expected %d, received %d", lane, expected.ExitStatus, run.ExitStatus))
	}
	envelope := struct {
		Summary struct {
			Status string `json:"status"`
		} `json:"summary"`
		Findings []struct {
			Check string `json:"ruleId"`
		} `json:"findings"`
		AnalysisContext []struct {
			Paths []string `json:"paths"`
		} `json:"analysisContext"`
		SourceDependencyGraph struct {
			Nodes []struct {
				Path string `json:"path"`
			} `json:"nodes"`
		} `json:"sourceDependencyGraph"`
	}{}
	if err := json.Unmarshal(run.Report, &envelope); err != nil {
		return append(failures, lane+" report could not be decoded after schema validation")
	}
	if envelope.Summary.Status != expected.ReportStatus {
		failures = append(failures, fmt.Sprintf("%s summary.status: expected %s, received %s", lane, expected.ReportStatus, envelope.Summary.Status))
	}
	for _, rule := range expected.RequiredRules {
		if !slices.ContainsFunc(envelope.Findings, func(finding struct {
			Check string `json:"ruleId"`
		}) bool {
			return finding.Check == rule
		}) {
			failures = append(failures, fmt.Sprintf("%s findings: required rule %s was absent", lane, rule))
		}
	}
	coverage := []string{}
	for _, context := range envelope.AnalysisContext {
		coverage = append(coverage, context.Paths...)
	}
	for _, node := range envelope.SourceDependencyGraph.Nodes {
		coverage = append(coverage, node.Path)
	}
	for _, path := range expected.RequiredCoverage {
		if !slices.Contains(coverage, path) {
			failures = append(failures, fmt.Sprintf("%s analysisContext: required path %s was absent", lane, path))
		}
	}
	before := conformanceFilesByPath(run.Before)
	after := conformanceFilesByPath(run.After)
	allowedWrites := map[string]bool{}
	for _, write := range expected.Writes {
		allowedWrites[write.Path] = true
		file, exists := after[write.Path]
		if !exists || file.SHA256 != write.SHA256 {
			failures = append(failures, fmt.Sprintf("%s writes[%s]: expected SHA-256 %s", lane, write.Path, write.SHA256))
		}
	}
	for _, path := range expected.Protected {
		if before[path] != after[path] {
			failures = append(failures, fmt.Sprintf("%s protected[%s]: file identity changed", lane, path))
		}
	}
	for path, initial := range before {
		if final, exists := after[path]; (!exists || final != initial) && !allowedWrites[path] {
			failures = append(failures, fmt.Sprintf("%s filesystem[%s]: unauthorized change", lane, path))
		}
	}
	for path := range after {
		if _, exists := before[path]; !exists && !allowedWrites[path] {
			failures = append(failures, fmt.Sprintf("%s filesystem[%s]: unauthorized addition", lane, path))
		}
	}
	return failures
}

func conformanceFilesByPath(files []ConformanceFileIdentity) map[string]ConformanceFileIdentity {
	result := make(map[string]ConformanceFileIdentity, len(files))
	for _, file := range files {
		result[file.Path] = file
	}
	return result
}
