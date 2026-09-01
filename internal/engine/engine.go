package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	agentpolicy "github.com/riteofstring/code-polishy/internal/agents"
	"github.com/riteofstring/code-polishy/internal/architecture"
	"github.com/riteofstring/code-polishy/internal/artifactsecurity"
	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/policymodule"
	"github.com/riteofstring/code-polishy/internal/portability"
	"github.com/riteofstring/code-polishy/internal/quality"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/supplychain"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

type Engine struct {
	Repository           repository.Repository
	Runner               runner.Runner
	Output               io.Writer
	Verbose              bool
	PolicyModuleFindings []policy.Finding
	PolicyModuleNotes    []string
	PackDataRoot         string
}

type Report struct {
	BehaviorReview      *BehaviorReviewStatus
	MergePolicy         *MergePolicy
	CheckpointPolicy    *CheckpointPolicy
	GateRunPolicy       *GateRunPolicy
	ChangeBoundary      *ChangeBoundary
	ChangedTestScope    *ChangedTestScope
	TestQualityReminder *TestQualityReminder
	TestCommands        []TestCommandEvidence
	TestDiagnostics     []TestFailureDiagnostic
	Findings            []policy.Finding
	Advisories          []policy.Advisory
	Suppressed          []policy.Suppressed
	Assessed            []policy.AssessedVulnerability
	ReleaseAges         []policy.AssessedReleaseAge
	Tables              []Table
	Notes               []string
}

type MergePolicy struct {
	Level   string
	Base    string
	Reasons []string
}

type CheckpointPolicy struct {
	Scope       string
	Base        string
	Candidate   string
	ReceiptPath string
}

type GateRunPolicy struct {
	Status       string
	ReportPath   string
	ReusedPhases []string
}

type ChangeBoundary struct {
	Base     string
	Modules  []string
	Paths    []string
	NewPaths []string
}

type TestQualityReminder struct {
	ChangedTestPaths     []string
	TaskBase             string
	TaskChangedTestPaths []string
}

type Table struct {
	Title   string
	Columns []string
	Rows    [][]string
}

func Open(repoRoot, policyRoot, configPath string) (*Engine, error) {
	config, err := policy.Load(repoRoot, configPath)
	if err != nil {
		return nil, err
	}
	repo, err := repository.Open(repoRoot, policyRoot, config)
	if err != nil {
		return nil, err
	}
	dataRoot, dataRootErr := pack.UserDataRoot()
	packResolution := pack.Resolve(config.Packs, dataRoot)
	if dataRootErr != nil && len(config.Packs) > 0 {
		packResolution = pack.Unavailable(config.Packs, dataRootErr)
	}
	pack.Apply(&repo.Config, packResolution)
	files, err := repo.AllFiles()
	if err != nil {
		return nil, err
	}
	moduleResolution := policymodule.Resolve(repo, files)
	policymodule.Apply(&repo.Config, moduleResolution)

	pathEntries := repo.CommandEnvironment().PathEntries
	commandRunner := runner.OSRunner{Stdout: os.Stdout, Stderr: os.Stderr, PathEntries: pathEntries}
	return &Engine{
		Repository: repo, Runner: commandRunner, Output: os.Stdout,
		PolicyModuleFindings: append(moduleResolution.Findings, packResolution.Findings...),
		PolicyModuleNotes:    append(policymodule.Notes(moduleResolution.Active), packResolution.Notes...),
		PackDataRoot:         dataRoot,
	}, nil
}

func CheckChangeBoundary(repoRoot, policyRoot, configPath, base string, modules, paths, newPaths []string) (Report, error) {
	result, err := repository.CheckChangeBoundary(repoRoot, policyRoot, configPath, base, modules, paths, newPaths)
	if err != nil {
		return Report{}, err
	}
	return Report{
		ChangeBoundary: &ChangeBoundary{Base: result.Base, Modules: result.AllowedModules, Paths: result.AllowedPaths, NewPaths: result.AllowedNewPaths},
		Findings:       result.Findings,
		Notes:          []string{fmt.Sprintf("evaluated %d changed paths against trusted-base module ownership", len(result.ChangedPaths))},
	}, nil
}

func (engine *Engine) Select(mode string, files []string) (repository.Selection, error) {
	return engine.Repository.Select(mode, files)
}

func (engine *Engine) SelectBase(base string) (repository.Selection, error) {
	return engine.Repository.SelectBase(base)
}

func (engine *Engine) Doctor(ctx context.Context) (Report, error) {
	files, err := engine.Repository.AllFiles()
	if err != nil {
		return Report{}, err
	}
	rawFiles, err := engine.Repository.RawFiles()
	if err != nil {
		return Report{}, err
	}
	findings := []policy.Finding{}
	findings = append(findings, engine.Repository.DesignDocumentFindings()...)
	if filepath.Clean(engine.Repository.Root) != filepath.Clean(engine.Repository.PolicyRoot) {
		agentStatus := agentpolicy.Check(engine.Repository.Root, engine.Repository.PolicyRoot)
		if !agentStatus.Current {
			for _, issue := range agentStatus.Issues {
				findings = append(findings, policy.Finding{
					Check: issue.Check, Path: issue.Path, Subject: issue.Subject, Message: issue.Message,
				})
			}
		}
	}
	findings = append(findings, hiddenInputFindings(engine.Repository, rawFiles)...)
	findings = append(findings, quality.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, architecture.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, portability.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, testpolicy.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, testpolicy.SourceFindings(engine.Repository, files)...)
	findings = append(findings, supplychain.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, artifactsecurity.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, quality.ToolFindings(engine.Repository, files)...)
	findings = append(findings, supplychain.ToolFindings(engine.Repository, files)...)
	findings = append(findings, artifactsecurity.ToolFindings(engine.Repository)...)
	findings = append(findings, configuredToolFindings(engine.Repository)...)
	findings = append(findings, supplychain.Static(ctx, engine.Repository, files)...)
	findings = append(findings, engine.PolicyModuleFindings...)
	if len(engine.Repository.Config.Packs) > 0 && engine.PackDataRoot != "" {
		findings = append(findings, pack.FullTreeFindings(engine.Repository.Config.Packs, engine.PackDataRoot)...)
	}
	javascriptFindings, javascriptNotes := quality.JavaScriptBundleStatus(engine.Repository, files)
	findings = append(findings, javascriptFindings...)
	notes := []string{fmt.Sprintf("inventory: %d governed files across %d modules", len(files), len(engine.Repository.Config.Modules))}
	if discovery := engine.Repository.CommandDiscoveryNote(); discovery != "" {
		notes = append(notes, discovery)
	}
	notes = append(notes, engine.PolicyModuleNotes...)
	notes = append(notes, javascriptNotes...)
	notes = append(notes, testpolicy.Notes(engine.Repository, files)...)
	return engine.finishWithAdvisories(findings, portability.Advisories(engine.Repository, files), notes), nil
}

func (engine *Engine) Check(ctx context.Context, selection repository.Selection, profile string) Report {
	findings := engine.coverageFindings()
	findings = append(findings, quality.Check(ctx, engine.Repository, selection, engine.Runner, profile)...)
	findings = append(findings, testpolicy.SourceFindings(engine.Repository, selection.Files)...)
	findings = append(findings, architecture.Check(ctx, engine.Repository, selection.Files)...)
	findings = append(findings, supplychain.Static(ctx, engine.Repository, selection.Files)...)
	notes := []string{fmt.Sprintf("checked %d files", len(selection.Files))}
	if len(selection.Candidate.Deleted) > 0 {
		notes = append(notes, fmt.Sprintf("%d deletions participated in command selection", len(selection.Candidate.Deleted)))
	}
	return engine.finishWithAdvisories(findings, portability.Advisories(engine.Repository, selection.Files), notes)
}

func (engine *Engine) CheckChangeAware(ctx context.Context, selection repository.Selection, profile string) Report {
	return engine.withTestQualityReminder(engine.Check(ctx, selection, profile), selection.Candidate)
}

func (engine *Engine) CheckNamed(ctx context.Context, names []string) (Report, error) {
	findings, err := quality.RunNamedCommands(ctx, engine.Repository, engine.Runner, names)
	if err != nil {
		return Report{}, err
	}
	return engine.finish(findings, []string{fmt.Sprintf("ran %d exact configured checks", len(names))}), nil
}

func (engine *Engine) coverageFindings() []policy.Finding {
	files, err := engine.Repository.AllFiles()
	if err != nil {
		return []policy.Finding{{Check: "policy.inventory", Path: "repository", Subject: "files", Message: err.Error()}}
	}
	rawFiles, err := engine.Repository.RawFiles()
	if err != nil {
		return []policy.Finding{{Check: "policy.inventory", Path: "repository", Subject: "raw-files", Message: err.Error()}}
	}
	findings := engine.Repository.DesignDocumentFindings()
	findings = append(findings, hiddenInputFindings(engine.Repository, rawFiles)...)
	findings = append(findings, quality.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, architecture.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, portability.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, testpolicy.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, supplychain.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, artifactsecurity.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, engine.PolicyModuleFindings...)
	return findings
}

func (engine *Engine) Architecture(ctx context.Context, selection repository.Selection) Report {
	return engine.finish(architecture.Check(ctx, engine.Repository, selection.Files), []string{fmt.Sprintf("checked architecture for %d files", len(selection.Files))})
}

func (engine *Engine) Format(ctx context.Context, selection repository.Selection) Report {
	findings := append([]policy.Finding{}, engine.PolicyModuleFindings...)
	findings = append(findings, quality.Format(ctx, engine.Repository, selection, engine.Runner)...)
	return engine.finish(findings, nil)
}

func (engine *Engine) Test(ctx context.Context, request testpolicy.Request) (Report, error) {
	report, err := engine.test(ctx, request, false)
	if err != nil || !usesChangedTestSelection(request) {
		return report, err
	}
	report.ChangedTestScope = NewChangedTestScope(request)
	if !isChangedScopeTestRequest(request) {
		return report, nil
	}
	return engine.withTestQualityReminder(report, request.Changed.Candidate), nil
}

func isChangedScopeTestRequest(request testpolicy.Request) bool {
	return !request.Recommended && usesChangedTestSelection(request)
}

func usesChangedTestSelection(request testpolicy.Request) bool {
	return !request.Full && !request.Supplemental && len(request.Modules) == 0 && len(request.Suites) == 0
}

func (engine *Engine) test(ctx context.Context, request testpolicy.Request, stopAfterFailure bool) (Report, error) {
	plan, err := testpolicy.BuildPlan(engine.Repository, request)
	if err != nil {
		return Report{}, err
	}
	return engine.testExactPlan(ctx, plan, request.Changed, stopAfterFailure)
}

func (engine *Engine) testExactPlan(ctx context.Context, plan testpolicy.Plan, selection repository.Selection, stopAfterFailure bool) (Report, error) {
	notes := []string{}
	if len(plan.ChangedModules) > 0 {
		label := "changed modules: "
		if selection.All {
			label = "modules selected after repository-wide expansion: "
		}
		notes = append(notes, label+strings.Join(plan.ChangedModules, ", "))
	}
	if len(plan.ImpactedModules) > 0 {
		notes = append(notes, "impacted modules: "+strings.Join(plan.ImpactedModules, ", "))
	}
	if len(plan.Suites) == 0 {
		if plan.Level == testpolicy.MergeLevelDocumentation {
			notes = append(notes, "documentation-only candidate selected zero application test suites")
		}
		notes = append(notes, "no test suites matched the requested scope")
		return engine.finish(nil, notes), nil
	}
	reporter := testpolicy.NewDirectExecutionReporter(engine.Output, plan, engine.Verbose)
	runSuites := testpolicy.RunWithEvidence
	if stopAfterFailure {
		runSuites = testpolicy.RunUntilFailureWithEvidence
	}
	runResult := runSuites(ctx, engine.Repository, engine.Runner, plan, reporter)
	notes = append(notes, fmt.Sprintf("ran %d test suites", len(plan.Suites)))
	report := engine.finish(runResult.Findings, notes)
	report.TestCommands = engine.testCommandEvidence(plan, selection, runResult.Executions, "working-tree")
	if stopAfterFailure {
		diagnostics, diagnosticEvidence := engine.testFailureDiagnostics(ctx, plan, selection, runResult.Executions)
		report.TestDiagnostics = diagnostics
		report.TestCommands = append(report.TestCommands, diagnosticEvidence...)
	}
	return report, nil
}

func (engine *Engine) TestPlan(base string) (Report, error) {
	selection, err := engine.testPlanSelection(base)
	if err != nil {
		return Report{}, err
	}
	advice, err := testpolicy.BuildAdvice(engine.Repository, selection)
	if err != nil {
		return Report{}, err
	}
	selectedLevel, mergePolicy, ordinaryNotes, err := engine.testPlanMergePolicy(base, selection, advice)
	if err != nil {
		return Report{}, err
	}
	report := engine.testPlanReport(base, selection, advice, selectedLevel, mergePolicy, ordinaryNotes)
	report, err = engine.withTestPlanBehaviorReview(base, report)
	if err != nil {
		return Report{}, err
	}
	return engine.withTestQualityReminder(report, selection.Candidate), nil
}

func (engine *Engine) testPlanReport(
	base string, selection repository.Selection, advice testpolicy.Advice, selectedLevel string,
	mergePolicy *MergePolicy, ordinaryNotes []string,
) Report {
	report := engine.finish(nil, testPlanNotes(base, selection, advice, ordinaryNotes))
	report.MergePolicy = mergePolicy
	report.Tables = []Table{testLevelsTable(advice, selectedLevel, base != "")}
	return report
}

func testPlanNotes(base string, selection repository.Selection, advice testpolicy.Advice, ordinaryNotes []string) []string {
	notes := []string{}
	if base == "" {
		notes = append(notes, "planning current uncommitted Git changes")
	} else {
		notes = append(notes, fmt.Sprintf("planning changes from merge-base(%s, HEAD) through the current working tree", base))
	}
	if selection.All && len(advice.ChangedModules) > 0 {
		notes = append(notes, "all modules selected after repository-wide impact expansion")
	} else if len(advice.ChangedModules) > 0 {
		notes = append(notes, "changed modules: "+strings.Join(advice.ChangedModules, ", "))
	}
	if len(advice.ImpactedModules) > 0 {
		notes = append(notes, "impacted modules: "+strings.Join(advice.ImpactedModules, ", "))
	}
	notes = append(notes, ordinaryNotes...)
	if len(advice.Supplemental) > 0 {
		notes = append(notes, "supplemental quality (run separately with test --supplemental): "+suiteSummary(advice.Supplemental))
		notes = append(notes, "supplemental suites are excluded from ordinary recommended, full, verify, and gate")
	}
	return notes
}

func (engine *Engine) withTestPlanBehaviorReview(base string, report Report) (Report, error) {
	if base == "" {
		return report, nil
	}
	status, err := engine.BehaviorReviewStatus(context.Background(), base)
	if err != nil {
		return Report{}, err
	}
	return withBehaviorReview(report, status), nil
}

func (engine *Engine) testPlanMergePolicy(
	base string, selection repository.Selection, advice testpolicy.Advice,
) (string, *MergePolicy, []string, error) {
	if base == "" {
		decision, err := testpolicy.BuildMergeDecision(engine.Repository, selection)
		if err != nil {
			return "", nil, nil, err
		}
		if decision.Level == testpolicy.MergeLevelDocumentation {
			notes := []string{"diagnostic documentation advice: " + decision.Level}
			for _, reason := range decision.Reasons {
				notes = append(notes, "reason: "+reason)
			}
			notes = append(notes, "resolve a trusted merge target before documentation merge verification")
			return decision.Level, nil, notes, nil
		}
		notes := []string{"diagnostic ordinary advice: " + advice.Suggested}
		for _, reason := range advice.Reasons {
			notes = append(notes, "reason: "+reason)
		}
		notes = append(notes, "resolve a trusted merge target before ordinary merge verification")
		return advice.Suggested, nil, notes, nil
	}
	decision, err := testpolicy.BuildMergeDecision(engine.Repository, selection)
	if err != nil {
		return "", nil, nil, err
	}
	mergePolicy := &MergePolicy{
		Level: decision.Level, Base: base, Reasons: append([]string{}, decision.Reasons...),
	}
	execution := "ordinary"
	if decision.Level == testpolicy.MergeLevelDocumentation {
		execution = "documentation"
	}
	notes := []string{execution + " merge execution: code-polishy merge-gate --base " + base}
	return decision.Level, mergePolicy, notes, nil
}

func (engine *Engine) testPlanSelection(base string) (repository.Selection, error) {
	if base != "" {
		return engine.Repository.SelectBase(base)
	}
	return engine.Repository.Select("changes", nil)
}

func suiteSummary(suites []policy.TestSuite) string {
	if len(suites) == 0 {
		return "no matching suites"
	}
	counts := map[string]int{}
	names := make([]string, 0, len(suites))
	for _, suite := range suites {
		counts[suite.Cost]++
		names = append(names, suite.Name)
	}
	costs := []string{}
	for _, cost := range []string{"quick", "standard", "expensive"} {
		if counts[cost] > 0 {
			costs = append(costs, fmt.Sprintf("%s=%d", cost, counts[cost]))
		}
	}
	return fmt.Sprintf("%d suites (%s): %s", len(suites), strings.Join(costs, ", "), strings.Join(names, ", "))
}

func testLevelsTable(advice testpolicy.Advice, selectedLevel string, hasMergeBase bool) Table {
	levelName := func(name string) string {
		if selectedLevel == name {
			return name + " *"
		}
		return name
	}
	marker := "advice"
	recommendedExecution := "merge target required"
	fullExecution := "merge target required"
	focusedExecution := "test --changed"
	ordinaryStatus := "advice"
	if hasMergeBase {
		marker = "selected"
		recommendedExecution = "merge-gate --base REF"
		fullExecution = "merge-gate --base REF"
		focusedExecution = "test --changed --base REF"
		ordinaryStatus = "automatic"
	}
	rows := [][]string{
		{"focused", suiteCostSummary(advice.Focused), "routine", focusedExecution},
		{levelName("recommended"), suiteCostSummary(advice.Recommended), ordinaryStatus, recommendedExecution},
		{levelName("full"), suiteCostSummary(advice.Full), ordinaryStatus, fullExecution},
		{"supplemental", suiteCostSummary(advice.AllSupplemental), "separate", "test --supplemental"},
	}
	if selectedLevel == testpolicy.MergeLevelDocumentation {
		documentationStatus := "advice"
		documentationExecution := "merge target required"
		if hasMergeBase {
			documentationStatus = "automatic"
			documentationExecution = "merge-gate --base REF"
		}
		rows = append([][]string{{
			levelName(testpolicy.MergeLevelDocumentation), "0 application suites", documentationStatus, documentationExecution,
		}}, rows...)
	}
	return Table{
		Title:   "TEST LEVELS (q=quick, s=standard, e=expensive; *=" + marker + ")",
		Columns: []string{"LEVEL", "SUITES", "STATUS", "EXECUTION"},
		Rows:    rows,
	}
}

func suiteCostSummary(suites []policy.TestSuite) string {
	if len(suites) == 0 {
		return "none"
	}
	counts := map[string]int{}
	for _, suite := range suites {
		cost := suite.Cost
		if cost == "" {
			cost = "quick"
		}
		counts[cost]++
	}
	costs := []string{}
	for _, cost := range []struct{ name, short string }{{"quick", "q"}, {"standard", "s"}, {"expensive", "e"}} {
		if counts[cost.name] > 0 {
			costs = append(costs, fmt.Sprintf("%s=%d", cost.short, counts[cost.name]))
		}
	}
	return fmt.Sprintf("%d (%s)", len(suites), strings.Join(costs, ", "))
}

func (engine *Engine) Verify(ctx context.Context, testsOnly bool) (Report, error) {
	return engine.verify(ctx, testsOnly, false)
}

func (engine *Engine) verify(ctx context.Context, testsOnly, stopAfterFailure bool) (Report, error) {
	report, err := engine.test(ctx, testpolicy.Request{Full: true}, stopAfterFailure)
	if err != nil || len(report.Findings) > 0 || testsOnly {
		return report, err
	}
	files, err := engine.Repository.AllFiles()
	if err != nil {
		return Report{}, err
	}
	selection := repository.Selection{Files: files, All: true}
	buildFindings := quality.RunCommands(ctx, engine.Repository, selection, engine.Runner, "build")
	return engine.combine(report, engine.finish(buildFindings, []string{"completed full build profile"})), nil
}

func (engine *Engine) SupplyChain(ctx context.Context, offline bool) (Report, error) {
	files, err := engine.Repository.AllFiles()
	if err != nil {
		return Report{}, err
	}
	findings := append([]policy.Finding{}, engine.PolicyModuleFindings...)
	findings = append(findings, supplychain.CoverageFindings(engine.Repository, files)...)
	findings = append(findings, supplychain.Static(ctx, engine.Repository, files)...)
	selection := repository.Selection{Files: files, All: true}
	mode := "online"
	notes := []string{}
	if offline {
		mode = "offline"
		findings = append(findings, quality.RunCommands(ctx, engine.Repository, selection, engine.Runner, "supply-chain")...)
	}
	if !offline {
		findings = append(findings, supplychain.Online(ctx, engine.Repository, files, engine.Runner)...)
		findings = append(findings, quality.RunCommandsForProfiles(ctx, engine.Repository, selection, engine.Runner, "supply-chain", "supply-chain-online", "security")...)
		artifactResult := artifactsecurity.Run(ctx, engine.Repository, engine.Runner)
		findings = append(findings, artifactResult.Findings...)
		notes = append(notes, artifactResult.Notes...)
		findings = supplychain.ClassifyKnownExploited(ctx, engine.Repository, findings)
	}
	notes = append([]string{"completed " + mode + " supply-chain profile"}, notes...)
	if offline {
		return engine.finish(findings, notes), nil
	}
	return engine.finishWithAssessments(findings, notes, true), nil
}

func (engine *Engine) DependencyReview(ctx context.Context, base string) (Report, error) {
	mergeBase, err := engine.Repository.MergeBase(base)
	if err != nil {
		return Report{}, err
	}
	changes, err := supplychain.DependencyChanges(ctx, engine.Repository, mergeBase)
	if err != nil {
		return Report{}, err
	}
	report, err := engine.SupplyChain(ctx, false)
	if err != nil {
		return report, err
	}
	report.Advisories = mergeAdvisories(report.Advisories, supplychain.NewDependencyAgeAdvisories(ctx, engine.Repository, changes))
	report.Tables = append([]Table{dependencyReviewTable(changes)}, report.Tables...)
	report.Notes = append([]string{fmt.Sprintf("dependency review base: %s (%s)", base, mergeBase)}, report.Notes...)
	return report, nil
}

func dependencyReviewTable(changes []supplychain.DependencyChange) Table {
	rows := make([][]string, 0, len(changes))
	for _, change := range changes {
		rows = append(rows, []string{
			change.Ecosystem + "/" + change.Directness, change.Usage, change.Change,
			change.Package, change.From, change.To, change.Scope,
		})
	}
	return Table{
		Title: "DEPENDENCY REVIEW", Columns: []string{"KIND", "USE", "CHANGE", "PACKAGE", "FROM", "TO", "SCOPE"}, Rows: rows,
	}
}

func (engine *Engine) ArtifactSecurity(ctx context.Context) Report {
	result := artifactsecurity.Run(ctx, engine.Repository, engine.Runner)
	return engine.finishWithAssessments(result.Findings, result.Notes, false)
}

func withCheckpointPolicy(report Report, scope, base, candidate, receiptPath string) Report {
	report.CheckpointPolicy = &CheckpointPolicy{Scope: scope, Base: base, Candidate: candidate, ReceiptPath: receiptPath}
	return report
}

func (engine *Engine) finish(findings []policy.Finding, notes []string) Report {
	return engine.finishWithAdvisories(findings, nil, notes)
}

func (engine *Engine) finishWithAdvisories(findings []policy.Finding, advisories []policy.Advisory, notes []string) Report {
	report := engine.finishWithAssessments(findings, notes, false)
	report.Advisories = mergeAdvisories(nil, advisories)
	return report
}

func (engine *Engine) finishWithAssessments(findings []policy.Finding, notes []string, enforceUnused bool) Report {
	ordered := append([]policy.Finding{}, findings...)
	sortFindings(ordered)
	kept, releaseAges := policy.ApplyReleaseAgeAssessments(
		ordered, engine.Repository.Config.SupplyChain.ReleaseAgeAssessments, time.Now().UTC(), enforceUnused,
	)
	kept, assessed := policy.ApplyVulnerabilityAssessments(
		kept, engine.Repository.Config.SupplyChain.VulnerabilityAssessments, time.Now().UTC(), enforceUnused,
	)
	kept, suppressed := policy.ApplyExceptions(kept, engine.Repository.Config.Exceptions, time.Now().UTC())
	sortFindings(kept)
	sort.Slice(suppressed, func(left, right int) bool {
		return findingKey(suppressed[left].Finding)+suppressed[left].Exception.ID < findingKey(suppressed[right].Finding)+suppressed[right].Exception.ID
	})
	sort.Slice(assessed, func(left, right int) bool {
		return findingKey(assessed[left].Finding)+assessed[left].Assessment.ID < findingKey(assessed[right].Finding)+assessed[right].Assessment.ID
	})
	sort.Slice(releaseAges, func(left, right int) bool {
		return findingKey(releaseAges[left].Finding)+releaseAges[left].Assessment.ID < findingKey(releaseAges[right].Finding)+releaseAges[right].Assessment.ID
	})
	return Report{Findings: kept, Suppressed: suppressed, Assessed: assessed, ReleaseAges: releaseAges, Notes: notes}
}

func sortFindings(findings []policy.Finding) {
	sort.Slice(findings, func(left, right int) bool {
		return findingKey(findings[left]) < findingKey(findings[right])
	})
}

func findingKey(finding policy.Finding) string {
	return fmt.Sprintf("%s\x00%s\x00%09d\x00%09d\x00%s\x00%s", finding.Check, finding.Path, finding.Line, finding.Column, finding.Subject, finding.Message)
}

func advisoryKey(advisory policy.Advisory) string {
	return advisory.Check + "\x00" + advisory.Path + "\x00" + advisory.Subject + "\x00" + advisory.Message
}

func NewTestQualityReminder(paths []string) *TestQualityReminder {
	return newTestQualityReminder(paths, "", nil)
}

func NewTaskAwareTestQualityReminder(paths []string, taskBase string, taskPaths []string) *TestQualityReminder {
	return newTestQualityReminder(paths, taskBase, taskPaths)
}

func newTestQualityReminder(paths []string, taskBase string, taskPaths []string) *TestQualityReminder {
	ordered := orderedReminderPaths(paths)
	orderedTaskPaths := orderedReminderPaths(taskPaths)
	if len(ordered) == 0 && taskBase == "" {
		return nil
	}
	return &TestQualityReminder{ChangedTestPaths: ordered, TaskBase: taskBase, TaskChangedTestPaths: orderedTaskPaths}
}

func orderedReminderPaths(paths []string) []string {
	ordered := make([]string, 0, len(paths))
	for _, path := range paths {
		if path != "" {
			ordered = append(ordered, path)
		}
	}
	sort.Strings(ordered)
	return slices.Compact(ordered)
}

func combineTestQualityReminders(left, right *TestQualityReminder) *TestQualityReminder {
	paths := []string{}
	taskBase := ""
	taskPaths := []string{}
	if left != nil {
		paths = append(paths, left.ChangedTestPaths...)
		taskBase = left.TaskBase
		taskPaths = append(taskPaths, left.TaskChangedTestPaths...)
	}
	if right != nil {
		paths = append(paths, right.ChangedTestPaths...)
		if right.TaskBase != "" {
			taskBase = right.TaskBase
			taskPaths = append([]string{}, right.TaskChangedTestPaths...)
		}
	}
	return NewTaskAwareTestQualityReminder(paths, taskBase, taskPaths)
}

func mergeAdvisories(left, right []policy.Advisory) []policy.Advisory {
	merged := append(append([]policy.Advisory{}, left...), right...)
	sort.Slice(merged, func(leftIndex, rightIndex int) bool {
		return advisoryKey(merged[leftIndex]) < advisoryKey(merged[rightIndex])
	})
	result := make([]policy.Advisory, 0, len(merged))
	last := ""
	for _, advisory := range merged {
		key := advisoryKey(advisory)
		if len(result) == 0 || key != last {
			result = append(result, advisory)
			last = key
		}
	}
	return result
}

func (engine *Engine) combine(left, right Report) Report {
	return Report{
		BehaviorReview:      combineBehaviorReview(left.BehaviorReview, right.BehaviorReview),
		MergePolicy:         combineMergePolicy(left.MergePolicy, right.MergePolicy),
		CheckpointPolicy:    combineCheckpointPolicy(left.CheckpointPolicy, right.CheckpointPolicy),
		GateRunPolicy:       combineGateRunPolicy(left.GateRunPolicy, right.GateRunPolicy),
		ChangedTestScope:    combineChangedTestScope(left.ChangedTestScope, right.ChangedTestScope),
		TestQualityReminder: combineTestQualityReminders(left.TestQualityReminder, right.TestQualityReminder),
		TestCommands:        append(append([]TestCommandEvidence{}, left.TestCommands...), right.TestCommands...),
		TestDiagnostics:     append(append([]TestFailureDiagnostic{}, left.TestDiagnostics...), right.TestDiagnostics...),
		Findings:            append(append([]policy.Finding{}, left.Findings...), right.Findings...),
		Advisories:          mergeAdvisories(left.Advisories, right.Advisories),
		Suppressed:          append(append([]policy.Suppressed{}, left.Suppressed...), right.Suppressed...),
		Assessed:            append(append([]policy.AssessedVulnerability{}, left.Assessed...), right.Assessed...),
		ReleaseAges:         append(append([]policy.AssessedReleaseAge{}, left.ReleaseAges...), right.ReleaseAges...),
		Tables:              append(append([]Table{}, left.Tables...), right.Tables...),
		Notes:               append(append([]string{}, left.Notes...), right.Notes...),
	}
}

func combineBehaviorReview(left, right *BehaviorReviewStatus) *BehaviorReviewStatus {
	if right != nil {
		return right
	}
	return left
}

func combineGateRunPolicy(left, right *GateRunPolicy) *GateRunPolicy {
	if right != nil {
		return right
	}
	return left
}

func combineCheckpointPolicy(left, right *CheckpointPolicy) *CheckpointPolicy {
	if right != nil {
		return right
	}
	return left
}

func (engine *Engine) withTestQualityReminder(report Report, candidate repository.CandidateDelta) Report {
	return engine.combine(report, Report{TestQualityReminder: NewTestQualityReminder(testpolicy.ChangedTestPaths(engine.Repository, candidate))})
}

func (engine *Engine) withMergeGateTestQualityReminder(ctx context.Context, report Report, selection repository.Selection) Report {
	report = engine.withTestQualityReminder(report, selection.Candidate)
	receipt, err := behaviorreview.ReadCheckpoint(ctx, engine.Repository)
	if err != nil {
		return report
	}
	taskSelection, err := engine.Repository.SelectBase(receipt.Base)
	if err != nil || taskSelection.Base != receipt.Base {
		return report
	}
	return engine.combine(report, Report{TestQualityReminder: NewTaskAwareTestQualityReminder(
		nil, receipt.Base, testpolicy.ChangedTestPaths(engine.Repository, taskSelection.Candidate),
	)})
}

func combineChangedTestScope(left, right *ChangedTestScope) *ChangedTestScope {
	if right != nil {
		return right
	}
	return left
}

func combineMergePolicy(left, right *MergePolicy) *MergePolicy {
	if left == nil {
		return right
	}
	if right == nil || left.Level == testpolicy.MergeLevelFull {
		return left
	}
	return right
}

func hiddenInputFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range files {
		if !policy.MatchesAny(path, repo.Config.Scope.Exclude) {
			continue
		}
		protected := repo.IsSourceCommentSource(path) || supplychain.IsGovernedInput(path) || packOwnsManifest(repo.Config, path)
		if protected {
			findings = append(findings, policy.Finding{Check: "policy.scope", Path: path, Subject: "exclude", Message: "target scope cannot exclude protected source, manifests, lockfiles, or workflows"})
		}
	}
	return findings
}

func packOwnsManifest(config policy.Config, path string) bool {
	for _, rule := range config.PackManifests {
		if policy.MatchesAny(path, rule.Paths) {
			return true
		}
	}
	return false
}

func configuredToolFindings(repo repository.Repository) []policy.Finding {
	findings := []policy.Finding{}
	commands := append([]policy.Command{}, repo.Config.Checks...)
	for _, suite := range repo.Config.Tests.Suites {
		commands = append(commands, policy.Command{Name: suite.Name, Argv: suite.Argv, Cwd: suite.Cwd, TimeoutSeconds: suite.TimeoutSeconds})
	}
	for _, command := range commands {
		if command.Managed {
			continue
		}
		if finding, exists := configuredCommandFinding(repo, command); exists {
			findings = append(findings, finding)
		}
	}
	return findings
}

func configuredCommandFinding(repo repository.Repository, command policy.Command) (policy.Finding, bool) {
	cwd := filepath.Join(repo.Root, filepath.FromSlash(command.Cwd))
	resolved, err := filepath.EvalSymlinks(cwd)
	if err != nil || !inside(repo.Root, resolved) {
		message := "command working directory is missing or resolves outside the repository"
		return policy.Finding{Check: "policy.command", Path: policy.ConfigFilename, Subject: command.Name, Message: message}, true
	}
	executable := command.Argv[0]
	if strings.ContainsAny(executable, "/\\") {
		return checkedInCommandFinding(repo, command.Name, executable)
	}
	if _, lookupErr := exec.LookPath(configuredToolPath(repo, executable)); lookupErr != nil {
		message := fmt.Sprintf("command %q requires unavailable executable %q", command.Name, executable)
		return policy.Finding{Check: "policy.tool", Path: policy.ConfigFilename, Subject: executable, Message: message}, true
	}
	return policy.Finding{}, false
}

func checkedInCommandFinding(repo repository.Repository, name, executable string) (policy.Finding, bool) {
	resolved, resolveErr := filepath.EvalSymlinks(filepath.Join(repo.Root, filepath.FromSlash(executable)))
	info, statErr := os.Stat(resolved)
	notExecutable := statErr != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0
	if resolveErr == nil && inside(repo.Root, resolved) && !notExecutable {
		return policy.Finding{}, false
	}
	message := "checked-in command is missing, non-executable, or escapes the repository"
	return policy.Finding{Check: "policy.command", Path: policy.ConfigFilename, Subject: name, Message: message}, true
}

func configuredToolPath(repo repository.Repository, executable string) string {
	if executable == "go" || executable == "gofmt" {
		return repo.GoTool(executable)
	}
	return executable
}

func inside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func HasFindings(report Report) bool {
	return len(report.Findings) > 0
}

func IncludesCapability(config policy.Config, capability string) bool {
	return slices.Contains(config.Project.Capabilities, capability)
}
