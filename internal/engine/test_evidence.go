package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/testartifact"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

const (
	ChangedTestComparisonWorkingTree = "working-tree-vs-head"
	ChangedTestComparisonMergeBase   = "working-tree-vs-merge-base"

	TestDiagnosticIntermittentObserved = "intermittent-observed"
	TestDiagnosticCandidateRegression  = "candidate-regression"
	TestDiagnosticBaselineReproduced   = "baseline-reproduced"
	TestDiagnosticBaselineUnavailable  = "baseline-unavailable"
)

type ChangedTestScope struct {
	Comparison        string `json:"comparison"`
	RequestedBase     string `json:"requestedBase,omitempty"`
	ExactBase         string `json:"exactBase"`
	Candidate         string `json:"candidate"`
	GovernedPathCount int    `json:"governedPathCount"`
}

type TestCommandEvidence struct {
	Name                  string                 `json:"name"`
	Kind                  string                 `json:"kind"`
	Scope                 string                 `json:"scope"`
	Cost                  string                 `json:"cost"`
	Target                string                 `json:"target"`
	SuiteModules          []string               `json:"suiteModules"`
	SuitePaths            []string               `json:"suitePaths"`
	ChangedModules        []string               `json:"changedModules"`
	ImpactedModules       []string               `json:"impactedModules"`
	ChangedModuleOverlap  []string               `json:"changedModuleOverlap"`
	ImpactedModuleOverlap []string               `json:"impactedModuleOverlap"`
	ChangedPathOverlap    []string               `json:"changedPathOverlap"`
	Result                runner.Result          `json:"result"`
	FailureCategory       runner.FailureCategory `json:"failureCategory,omitempty"`
	FailureMessage        string                 `json:"failureMessage,omitempty"`
	Attempt               int                    `json:"attempt"`
	LogPath               string                 `json:"logPath,omitempty"`
	Artifacts             []testartifact.Record  `json:"artifacts"`
	Reused                bool                   `json:"reused"`
	ReceiptPath           string                 `json:"receiptPath,omitempty"`
	ReceiptSHA256         string                 `json:"receiptSha256,omitempty"`
}

type TestFailureDiagnostic struct {
	Suite          string               `json:"suite"`
	State          string               `json:"state"`
	CandidateRetry *TestCommandEvidence `json:"candidateRetry,omitempty"`
	BaselineReplay *TestCommandEvidence `json:"baselineReplay,omitempty"`
}

type TestDiagnosticRunnerProvider interface {
	TestDiagnosticRunner() runner.Runner
}

func NewChangedTestScope(request testpolicy.Request) *ChangedTestScope {
	comparison := ChangedTestComparisonWorkingTree
	if request.RequestedBase != "" {
		comparison = ChangedTestComparisonMergeBase
	}
	return &ChangedTestScope{
		Comparison: comparison, RequestedBase: request.RequestedBase, ExactBase: request.Changed.Base,
		Candidate: "working-tree", GovernedPathCount: len(request.Changed.Candidate.Paths()),
	}
}

func (engine *Engine) testCommandEvidence(plan testpolicy.Plan, selection repository.Selection, executions []testpolicy.SuiteExecution, target string) []TestCommandEvidence {
	changedPaths := selection.Candidate.Paths()
	result := make([]TestCommandEvidence, 0, len(executions))
	for _, execution := range executions {
		suite := execution.Suite
		evidence := TestCommandEvidence{
			Name: suite.Name, Kind: suite.Kind, Scope: suite.Scope, Cost: suite.Cost, Target: target,
			SuiteModules: sortedUniqueStrings(suite.Modules), SuitePaths: sortedUniqueStrings(suite.Paths),
			ChangedModules: sortedUniqueStrings(plan.ChangedModules), ImpactedModules: sortedUniqueStrings(plan.ImpactedModules),
			ChangedModuleOverlap:  stringIntersection(suite.Modules, plan.ChangedModules),
			ImpactedModuleOverlap: stringIntersection(suite.Modules, plan.ImpactedModules),
			ChangedPathOverlap:    engine.suiteChangedPathOverlap(suite, changedPaths),
			Result:                execution.Result, FailureCategory: execution.FailureCategory, FailureMessage: execution.FailureMessage,
			Attempt:   execution.Attempt,
			Artifacts: append([]testartifact.Record{}, execution.Artifacts...),
			Reused:    execution.Reused, ReceiptPath: execution.ReceiptPath, ReceiptSHA256: execution.ReceiptSHA256,
		}
		result = append(result, evidence)
	}
	return result
}

func (engine *Engine) suiteChangedPathOverlap(suite policy.TestSuite, changedPaths []string) []string {
	paths := []string{}
	for _, path := range changedPaths {
		if suite.Scope == "repository" {
			if len(suite.Paths) == 0 || policy.MatchesAny(path, suite.Paths) {
				paths = append(paths, path)
			}
			continue
		}
		if len(stringIntersection(engine.Repository.OwnerModuleNames(path), suite.Modules)) > 0 {
			paths = append(paths, path)
		}
	}
	return sortedUniqueStrings(paths)
}

func (engine *Engine) testFailureDiagnostics(ctx context.Context, plan testpolicy.Plan, selection repository.Selection, executions []testpolicy.SuiteExecution) ([]TestFailureDiagnostic, []TestCommandEvidence) {
	if plan.Level == "supplemental" {
		return nil, nil
	}
	diagnostics := []TestFailureDiagnostic{}
	evidence := []TestCommandEvidence{}
	commandRunner := testDiagnosticRunner(engine.Runner)
	for _, original := range executions {
		if !original.Failed() {
			continue
		}
		retry := testpolicy.ExecuteSuite(ctx, engine.Repository.Root, commandRunner, original.Suite, original.Attempt+1)
		retryEvidence := engine.testCommandEvidence(plan, selection, []testpolicy.SuiteExecution{retry}, "working-tree")[0]
		evidence = append(evidence, retryEvidence)
		diagnostic := TestFailureDiagnostic{Suite: original.Suite.Name, CandidateRetry: &retryEvidence}
		if !retry.Failed() {
			diagnostic.State = TestDiagnosticIntermittentObserved
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		if retry.FailureCategory != runner.FailureCommandExit {
			diagnostic.State = TestDiagnosticBaselineUnavailable
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		baseline, available := engine.replayTestBaseline(ctx, selection.Base, commandRunner, original.Suite, original.Attempt+2)
		if baseline != nil {
			baselineEvidence := engine.testCommandEvidence(plan, selection, []testpolicy.SuiteExecution{*baseline}, selection.Base)[0]
			evidence = append(evidence, baselineEvidence)
			diagnostic.BaselineReplay = &baselineEvidence
		}
		if !available || baseline == nil {
			diagnostic.State = TestDiagnosticBaselineUnavailable
		} else if !baseline.Failed() {
			diagnostic.State = TestDiagnosticCandidateRegression
		} else if baseline.FailureCategory == runner.FailureCommandExit {
			diagnostic.State = TestDiagnosticBaselineReproduced
		} else {
			diagnostic.State = TestDiagnosticBaselineUnavailable
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics, evidence
}

func testDiagnosticRunner(commandRunner runner.Runner) runner.Runner {
	if provider, ok := commandRunner.(TestDiagnosticRunnerProvider); ok {
		if diagnosticRunner := provider.TestDiagnosticRunner(); diagnosticRunner != nil {
			return diagnosticRunner
		}
	}
	return commandRunner
}

func (engine *Engine) replayTestBaseline(ctx context.Context, base string, commandRunner runner.Runner, suite policy.TestSuite, attempt int) (*testpolicy.SuiteExecution, bool) {
	if base == "" {
		return nil, false
	}
	parent, err := os.MkdirTemp("", "code-polishy-test-diagnostic-")
	if err != nil {
		return nil, false
	}
	worktree := filepath.Join(parent, "baseline")
	if err := engine.Repository.AddDetachedWorktree(ctx, worktree, base); err != nil {
		_ = os.Remove(parent)
		return nil, false
	}
	execution, available := engine.executeBaselineSuite(ctx, worktree, commandRunner, suite, attempt)
	if err := engine.Repository.RemoveWorktree(context.Background(), worktree); err != nil {
		return execution, false
	}
	if err := os.Remove(parent); err != nil {
		return execution, false
	}
	return execution, available
}

func (engine *Engine) executeBaselineSuite(ctx context.Context, worktree string, commandRunner runner.Runner, candidate policy.TestSuite, attempt int) (*testpolicy.SuiteExecution, bool) {
	baseline, available := engine.baselineSuite(worktree, candidate)
	if !available {
		return nil, false
	}
	execution := testpolicy.ExecuteSuite(ctx, worktree, commandRunner, baseline, attempt)
	return &execution, true
}

func (engine *Engine) baselineSuite(worktree string, candidate policy.TestSuite) (policy.TestSuite, bool) {
	if err := release.RequireLockedRelease(worktree, engine.Repository.PolicyRoot); err != nil {
		return policy.TestSuite{}, false
	}
	baselineEngine, err := Open(worktree, engine.Repository.PolicyRoot, "")
	if err != nil || len(baselineEngine.PolicyModuleFindings) != 0 {
		return policy.TestSuite{}, false
	}
	plan, err := testpolicy.BuildPlan(baselineEngine.Repository, testpolicy.Request{Suites: []string{candidate.Name}})
	if err != nil || len(plan.Suites) != 1 || !sameTestSuiteSemantics(candidate, plan.Suites[0]) {
		return policy.TestSuite{}, false
	}
	return plan.Suites[0], true
}

func sameTestSuiteSemantics(candidate, baseline policy.TestSuite) bool {
	return sameTestSuiteIdentity(candidate, baseline) && sameTestSuiteCollections(candidate, baseline)
}

func sameTestSuiteIdentity(candidate, baseline policy.TestSuite) bool {
	return candidate.Name == baseline.Name && candidate.Kind == baseline.Kind && candidate.Scope == baseline.Scope &&
		candidate.Cost == baseline.Cost && candidate.Cwd == baseline.Cwd && candidate.TimeoutSeconds == baseline.TimeoutSeconds
}

func sameTestSuiteCollections(candidate, baseline policy.TestSuite) bool {
	return slices.Equal(candidate.Modules, baseline.Modules) && slices.Equal(candidate.Argv, baseline.Argv) &&
		slices.Equal(candidate.Paths, baseline.Paths) && slices.Equal(candidate.RunOn, baseline.RunOn) &&
		slices.Equal(candidate.ExtraInputs, baseline.ExtraInputs) &&
		slices.Equal(candidate.Covers, baseline.Covers) &&
		slices.Equal(candidate.Environment, baseline.Environment) && slices.Equal(candidate.ExclusiveResources, baseline.ExclusiveResources) &&
		slices.Equal(candidate.Artifacts, baseline.Artifacts)
}

func AttachTestCommandLogPath(report *Report, name string, attempt int, logPath string) bool {
	if report == nil || name == "" || attempt < 1 || logPath == "" {
		return false
	}
	matched := false
	for index := range report.TestCommands {
		if report.TestCommands[index].Name == name && report.TestCommands[index].Attempt == attempt {
			report.TestCommands[index].LogPath = logPath
			matched = true
		}
	}
	for index := range report.TestDiagnostics {
		attachDiagnosticLogPath(report.TestDiagnostics[index].CandidateRetry, name, attempt, logPath)
		attachDiagnosticLogPath(report.TestDiagnostics[index].BaselineReplay, name, attempt, logPath)
	}
	return matched
}

func attachDiagnosticLogPath(evidence *TestCommandEvidence, name string, attempt int, logPath string) {
	if evidence != nil && evidence.Name == name && evidence.Attempt == attempt {
		evidence.LogPath = logPath
	}
}

func sortedUniqueStrings(values []string) []string {
	ordered := append([]string{}, values...)
	sort.Strings(ordered)
	result := make([]string, 0, len(ordered))
	for _, value := range ordered {
		if value != "" && (len(result) == 0 || result[len(result)-1] != value) {
			result = append(result, value)
		}
	}
	return result
}

func stringIntersection(left, right []string) []string {
	rightSet := map[string]bool{}
	for _, value := range right {
		rightSet[value] = true
	}
	result := []string{}
	for _, value := range left {
		if rightSet[value] {
			result = append(result, value)
		}
	}
	return sortedUniqueStrings(result)
}
