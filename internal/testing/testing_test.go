package testing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRunPreservesSuiteExclusiveResources(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir()}
	recorder := &recordingTestingRunner{}
	plan := Plan{Suites: []policy.TestSuite{{
		Name: "performance", Kind: "performance", Scope: "repository", Argv: []string{"./performance.sh"}, Cwd: ".",
		ExclusiveResources: []string{"database", "performance"}, TimeoutSeconds: 30,
	}}}
	if findings := Run(context.Background(), repo, recorder, plan, nil); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if !slices.Equal(recorder.command.ExclusiveResources, []string{"database", "performance"}) {
		t.Fatalf("command resources = %v", recorder.command.ExclusiveResources)
	}
}

func TestRunWithEvidencePreservesSuiteAttemptAndObservedFailure(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir()}
	suite := policy.TestSuite{
		Name: "domain-unit", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{"domain"}, Paths: []string{"domain/**"},
		Argv: []string{"false"}, Cwd: ".", TimeoutSeconds: 30,
	}
	run := RunUntilFailureWithEvidence(context.Background(), repo, &evidenceTestingRunner{
		result: runner.Result{ExitStatus: 17, FailureCategory: runner.FailureCommandExit}, err: errors.New("suite failed"),
	}, Plan{Suites: []policy.TestSuite{suite}}, nil)
	if len(run.Findings) != 1 || run.Findings[0].Subject != suite.Name {
		t.Fatalf("findings = %+v", run.Findings)
	}
	if len(run.Executions) != 1 {
		t.Fatalf("executions = %+v", run.Executions)
	}
	execution := run.Executions[0]
	if execution.Attempt != 1 || execution.FailureCategory != runner.FailureCommandExit || execution.Result.ExitStatus != 17 ||
		!slices.Equal(execution.Suite.Modules, suite.Modules) || !slices.Equal(execution.Suite.Paths, suite.Paths) || !execution.Failed() {
		t.Fatalf("execution = %+v", execution)
	}
}

func TestRunUsesVerboseReporterForDetailedDirectExecution(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir()}
	plan := Plan{
		Level: "focused", SelectionBase: "base", Reasons: []string{"direct focused selection"}, ImpactedModules: []string{"domain"},
		Suites: []policy.TestSuite{{
			Name: "domain-unit", Kind: "unit", Scope: "module", Cost: "quick", Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 30,
		}},
	}
	var output bytes.Buffer
	if findings := Run(context.Background(), repo, &recordingTestingRunner{}, plan, NewDirectExecutionReporter(&output, plan, true)); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if !strings.Contains(output.String(), "EXECUTION PLAN stage=\"direct\" level=\"focused\" trusted-base=\"base\" candidate=\"working-tree\"") ||
		!strings.Contains(output.String(), "EXECUTION PROGRESS command=1/1") || !strings.Contains(output.String(), "kind=\"unit\"") ||
		!strings.Contains(output.String(), "EXECUTION RESULT command=1/1 status=\"passed\"") {
		t.Fatalf("direct output = %q", output.String())
	}
}

func TestRunUsesConciseReporterForHumanDirectExecution(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Root: t.TempDir()}
	plan := Plan{
		Level:  "focused",
		Suites: []policy.TestSuite{{Name: "domain-unit", Kind: "unit", Scope: "module", Cost: "quick", Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 30}},
	}
	var output bytes.Buffer
	if findings := Run(context.Background(), repo, &recordingTestingRunner{}, plan, NewDirectExecutionReporter(&output, plan, false)); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	got := output.String()
	if !strings.Contains(got, "1 validation command") || !strings.Contains(got, "focused") || !strings.Contains(got, "domain-unit") {
		t.Fatalf("concise output omitted execution facts: %q", got)
	}
}

func TestExecutionPlanForDirectTestUsesTheResolvedSelectionBase(t *testing.T) {
	t.Parallel()
	base := "0123456789abcdef0123456789abcdef01234567"
	selection := selectionForPaths("domain/model.go")
	selection.Base = base
	plan, err := BuildPlan(repository.Repository{Config: planningConfig()}, Request{Changed: selection})
	if err != nil {
		t.Fatal(err)
	}
	execution := ExecutionPlanForDirectTest(plan)
	if execution.TrustedBase != base || execution.Candidate != "working-tree" {
		t.Fatalf("direct execution identity = %+v", execution)
	}
}

func TestExplicitModulePlanStaysFocused(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	repo := repository.Repository{Config: config}
	plan, err := BuildPlan(repo, Request{Modules: []string{"domain"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan.ImpactedModules, []string{"domain"}) {
		t.Fatalf("impacted modules = %v", plan.ImpactedModules)
	}
	if got := suiteNames(plan.Suites); !slices.Equal(got, []string{"domain-unit"}) {
		t.Fatalf("suites = %v", got)
	}
}

func TestChangedFoundationIncludesReverseDependents(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: planningConfig()}
	plan, err := BuildPlan(repo, Request{Changed: selectionForPaths("domain/model.go")})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan.ImpactedModules, []string{"api", "domain", "web"}) {
		t.Fatalf("impacted modules = %v", plan.ImpactedModules)
	}
}

func TestChangedFileMapsToModule(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: planningConfig()}
	plan, err := BuildPlan(repo, Request{Changed: selectionForPaths("api/handler.go")})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan.ChangedModules, []string{"api"}) || !slices.Equal(plan.ImpactedModules, []string{"api", "web"}) {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestFullPlanRunsFullProfileOnly(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: planningConfig()}
	plan, err := BuildPlan(repo, Request{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(suiteNames(plan.Suites), "full") {
		t.Fatalf("full suite missing: %v", suiteNames(plan.Suites))
	}
	if slices.Contains(suiteNames(plan.Suites), "mutation") {
		t.Fatalf("supplemental suite leaked into full: %v", suiteNames(plan.Suites))
	}
}

func TestSupplementalPlanIsSeparateFromFull(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Tests.RequiredSupplementalKinds = []string{"mutation"}
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: "mutation", Kind: "mutation", Scope: "repository", Cost: "expensive", RunOn: []string{"supplemental"}})
	repo := repository.Repository{Config: config}
	plan, err := BuildPlan(repo, Request{Supplemental: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := suiteNames(plan.Suites); !slices.Equal(got, []string{"mutation"}) {
		t.Fatalf("supplemental suites = %v", got)
	}
	full, err := BuildPlan(repo, Request{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(suiteNames(full.Suites), "mutation") {
		t.Fatalf("full suites = %v", suiteNames(full.Suites))
	}
}

func TestDeclaredSupplementalKindsDoNotSelectOrdinaryPlans(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Tests.RequiredSupplementalKinds = []string{"mutation"}
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{
		Name: "mutation", Kind: "mutation", Scope: "repository", Cost: "expensive", RunOn: []string{"supplemental"},
	})
	repo := repository.Repository{Config: config}
	requests := map[string]Request{
		"changed":     {Changed: selectionForPaths("domain/model.go")},
		"recommended": {Recommended: true, Changed: selectionForPaths("domain/model.go")},
		"full":        {Full: true},
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			plan, err := BuildPlan(repo, request)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(suiteNames(plan.Suites), "mutation") {
				t.Fatalf("ordinary %s plan selected mutation: %v", name, suiteNames(plan.Suites))
			}
		})
	}

	explicit, err := BuildPlan(repo, Request{Supplemental: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := suiteNames(explicit.Suites); !slices.Equal(got, []string{"mutation"}) {
		t.Fatalf("explicit supplemental plan = %v", got)
	}
}

func TestAdviceListsOnlyImpactRelevantSupplementalSuites(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	for _, module := range []string{"domain", "api", "web"} {
		config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{
			Name: module + "-mutation", Kind: "mutation", Scope: "module", Modules: []string{module}, RunOn: []string{"supplemental"},
		})
	}
	repo := repository.Repository{Config: config}
	advice, err := BuildAdvice(repo, selectionForPaths("web/view.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if got := suiteNames(advice.Supplemental); !slices.Equal(got, []string{"web-mutation"}) {
		t.Fatalf("supplemental suites = %v", got)
	}
	if got := suiteNames(advice.AllSupplemental); !slices.Equal(got, []string{"domain-mutation", "api-mutation", "web-mutation"}) {
		t.Fatalf("all supplemental suites = %v", got)
	}
	if got := suiteNames(advice.Focused); !slices.Equal(got, []string{"web-unit"}) {
		t.Fatalf("focused suites = %v", got)
	}
}

func TestRecommendedPlanAddsRelevantStandardSuitesButNotExpensiveSuites(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Tests.Suites = append(config.Tests.Suites,
		policy.TestSuite{Name: "web-contract", Kind: "contract", Scope: "repository", Cost: "standard", Paths: []string{"web/**"}, RunOn: []string{"recommended", "full"}},
		policy.TestSuite{Name: "browser", Kind: "browser", Scope: "repository", Cost: "expensive", RunOn: []string{"full"}},
	)
	repo := repository.Repository{Config: config}
	advice, err := BuildAdvice(repo, selectionForPaths("web/view.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if got := suiteNames(advice.Recommended); !slices.Equal(got, []string{"web-unit", "web-contract"}) {
		t.Fatalf("recommended suites = %v", got)
	}
	if slices.Contains(suiteNames(advice.Recommended), "browser") || !slices.Contains(suiteNames(advice.Full), "browser") {
		t.Fatalf("advice = %+v", advice)
	}
	if advice.Suggested != "recommended" {
		t.Fatalf("suggested = %q, reasons = %v", advice.Suggested, advice.Reasons)
	}
}

func TestAdviceSuggestsFullForBroadDependencyImpact(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: planningConfig()}
	advice, err := BuildAdvice(repo, selectionForPaths("domain/model.go"))
	if err != nil {
		t.Fatal(err)
	}
	if advice.Suggested != "full" || len(advice.ImpactedModules) != 3 {
		t.Fatalf("advice = %+v", advice)
	}
}

func TestAdviceUsesFullAtTwentyChangedPaths(t *testing.T) {
	t.Parallel()
	paths := make([]string, 20)
	for index := range paths {
		paths[index] = "domain/file-" + string(rune('a'+index)) + ".go"
	}
	repo := repository.Repository{Config: planningConfig()}
	advice, err := BuildAdvice(repo, selectionForPaths(paths...))
	if err != nil {
		t.Fatal(err)
	}
	if advice.Suggested != "full" || len(advice.Reasons) == 0 || !strings.Contains(advice.Reasons[0], "20 governed paths") {
		t.Fatalf("advice = %+v", advice)
	}
}

func TestAdviceUsesFullAtExactlyTwoThirdsDependencyImpact(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: planningConfig()}
	advice, err := BuildAdvice(repo, selectionForPaths("api/handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	if advice.Suggested != "full" || len(advice.Reasons) == 0 || !strings.Contains(advice.Reasons[0], "2 of 3 modules") {
		t.Fatalf("advice = %+v", advice)
	}
}

func TestMergeDecisionRequiresOptIn(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: planningConfig()}
	decision, err := BuildMergeDecision(repo, selectionForPaths("web/view.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Level != MergeLevelFull || len(decision.Reasons) == 0 || !strings.Contains(decision.Reasons[0], "not configured") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMergeDecisionAllowsNarrowConfiguredModule(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Verification.MergeGate = &policy.MergeGate{RecommendedModules: []string{"web"}}
	decision, err := BuildMergeDecision(repository.Repository{Config: config}, selectionForPaths("web/view.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Level != MergeLevelRecommended {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMergeDecisionFailsClosedOutsideConfiguredModules(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Verification.MergeGate = &policy.MergeGate{RecommendedModules: []string{"web"}}
	decision, err := BuildMergeDecision(repository.Repository{Config: config}, selectionForPaths("api/handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Level != MergeLevelFull || len(decision.Reasons) == 0 ||
		!strings.Contains(decision.Reasons[0], "api") || !strings.Contains(decision.Reasons[0], "not configured") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMergeDecisionFailsClosedForUnownedNonMarkdownPath(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Verification.MergeGate = &policy.MergeGate{RecommendedModules: []string{"web"}}
	decision, err := BuildMergeDecision(repository.Repository{Config: config}, selectionForPaths("README.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Level != MergeLevelFull || !strings.Contains(decision.Reasons[0], "belongs to 0 modules") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestDocumentationMergeDecisionPrecedesAdaptiveVerification(t *testing.T) {
	t.Parallel()
	paths := make([]string, 20)
	for index := range paths {
		paths[index] = fmt.Sprintf("docs/guide-%02d.md", index)
	}
	cases := []struct {
		name      string
		selection repository.Selection
	}{
		{name: "root README", selection: selectionForPaths("README.md")},
		{name: "nested README", selection: selectionForPaths("guides/README.MARKDOWN")},
		{
			name: "deletion after analysis expansion",
			selection: repository.Selection{
				Candidate: repository.CandidateDelta{Deleted: []string{"docs/deleted.md"}},
				Files:     []string{"domain/model.go", "api/handler.go", "web/view.ts"}, All: true,
			},
		},
		{
			name: "rename after analysis expansion",
			selection: repository.Selection{
				Candidate: repository.CandidateDelta{AddedOrModified: []string{"docs/new-name.md"}, Deleted: []string{"docs/old-name.md"}},
				Files:     []string{"domain/model.go", "api/handler.go", "web/view.ts"}, All: true,
			},
		},
		{name: "twenty documents", selection: selectionForPaths(paths...)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := repository.Repository{Config: planningConfig()}
			decision, err := BuildMergeDecision(repo, testCase.selection)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Level != MergeLevelDocumentation || !slices.Equal(decision.Reasons, []string{"candidate contains ordinary Markdown only"}) {
				t.Fatalf("decision = %+v", decision)
			}
		})
	}
}

func TestDocumentationMergeDecisionEscalatesControlProductAndMixedInputs(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Documentation.ProductInputs = []string{"docs/product-input.md"}
	cases := []struct {
		name      string
		selection repository.Selection
	}{
		{name: "mixed source", selection: selectionForPaths("README.md", "web/view.ts")},
		{name: "agent guidance", selection: selectionForPaths("AGENTS.md")},
		{name: "assistant guidance", selection: selectionForPaths("CLAUDE.md")},
		{name: "skill guidance", selection: selectionForPaths("SKILL.md")},
		{name: "skills directory", selection: selectionForPaths("skills/guide.md")},
		{name: "templates directory", selection: selectionForPaths("templates/guide.md")},
		{name: "product input", selection: selectionForPaths("docs/product-input.md")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			decision, err := BuildMergeDecision(repository.Repository{Config: config}, testCase.selection)
			if err != nil {
				t.Fatal(err)
			}
			if decision.Level != MergeLevelFull {
				t.Fatalf("decision = %+v", decision)
			}
		})
	}
}

func TestDocumentationChangedPlanSelectsZeroApplicationSuites(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{
		Name: "global-contract", Kind: "contract", Scope: "repository", RunOn: []string{"focused", "recommended", "full"},
	})
	plan, err := BuildPlan(repository.Repository{Config: config}, Request{Changed: selectionForPaths("README.md")})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Level != MergeLevelDocumentation || len(plan.Suites) != 0 || !slices.Equal(plan.Reasons, []string{"candidate contains ordinary Markdown only"}) {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestExpandedAnalysisSelectionPreservesChangedTestBreadth(t *testing.T) {
	t.Parallel()
	selection := repository.Selection{
		Candidate: repository.CandidateDelta{AddedOrModified: []string{policy.ConfigFilename}},
		Files:     []string{"domain/model.go", "api/handler.go", "web/view.ts"},
		All:       true,
	}
	plan, err := BuildPlan(repository.Repository{Config: planningConfig()}, Request{Changed: selection})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan.ChangedModules, []string{"api", "domain", "web"}) ||
		!slices.Equal(suiteNames(plan.Suites), []string{"domain-unit", "api-unit", "web-unit"}) {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestMergeDecisionHonorsRepositoryWideExpansion(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Verification.MergeGate = &policy.MergeGate{RecommendedModules: []string{"web"}}
	selection := selectionForPaths("web/view.ts")
	selection.All = true
	decision, err := BuildMergeDecision(repository.Repository{Config: config}, selection)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Level != MergeLevelFull || !strings.Contains(decision.Reasons[0], "whole repository") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestRecommendationUsesFullAtExactlyThreeDirectModules(t *testing.T) {
	t.Parallel()
	config := policy.Config{Modules: make([]policy.Module, 6)}
	selection := selectionForPaths("a.go", "b.go", "c.go")
	plan := Plan{ChangedModules: []string{"a", "b", "c"}, ImpactedModules: []string{"a", "b", "c"}}
	suggested, reasons := recommendScope(config, selection, plan)
	if suggested != "full" || len(reasons) == 0 || !strings.Contains(reasons[0], "3 module boundaries") {
		t.Fatalf("suggested=%q reasons=%v", suggested, reasons)
	}
}

func TestRecommendedRepositorySuiteWithoutPathsRunsForAnyChange(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: "global-contract", Scope: "repository", RunOn: []string{"recommended", "full"}})
	repo := repository.Repository{Config: config}
	plan, err := BuildPlan(repo, Request{Recommended: true, Changed: selectionForPaths("web/view.ts")})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(suiteNames(plan.Suites), "global-contract") {
		t.Fatalf("suites = %v", suiteNames(plan.Suites))
	}
}

func TestRecommendedRepositorySuiteWithoutPathsDoesNotRunWithoutChanges(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: "global-contract", Scope: "repository", RunOn: []string{"recommended", "full"}})
	repo := repository.Repository{Config: config}
	plan, err := BuildPlan(repo, Request{Recommended: true})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(suiteNames(plan.Suites), "global-contract") {
		t.Fatalf("suites = %v", suiteNames(plan.Suites))
	}
}

func TestRepositoryPolicySelectsOwningModuleToolingContracts(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	config, err := policy.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.Repository{Root: root, Config: config}
	toolingSuites := []string{
		"mutation-wrapper-contract", "install-contract", "release-preflight-contract",
		"javascript-runtime-contract", "javascript-bundle-contract", "javascript-runner-contract",
		"javascript-project-contract", "installed-release-contract",
	}

	tests := []struct {
		path string
		want []string
	}{
		{path: "scripts/install.sh", want: toolingSuites},
		{path: "scripts/release-preflight.sh", want: toolingSuites},
		{path: "scripts/test-mutation-wrapper.sh", want: toolingSuites},
		{path: "internal/quality/vulture.go", want: []string{"quality-unit", "engine-unit", "cli-contract"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.path, func(t *testing.T) {
			plan, planErr := BuildPlan(repo, Request{Recommended: true, Changed: selectionForPaths(testCase.path)})
			if planErr != nil {
				t.Fatal(planErr)
			}
			if got := suiteNames(plan.Suites); !slices.Equal(got, testCase.want) {
				t.Fatalf("suites = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestCoverageRequiresFocusedAndRepositorySuites(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Tests.Suites = config.Tests.Suites[:2]
	findings := CoverageFindings(repository.Repository{Config: config}, nil)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestUICapabilityRequiresBrowserOrE2E(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Project.Capabilities = []string{"ui"}
	findings := CoverageFindings(repository.Repository{Config: config}, nil)
	if len(findings) != 1 || findings[0].Subject != "ui" {
		t.Fatalf("findings = %+v", findings)
	}
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: "browser", Kind: "browser", Scope: "repository", RunOn: []string{"full"}})
	if findings = CoverageFindings(repository.Repository{Config: config}, nil); len(findings) != 0 {
		t.Fatalf("browser coverage should satisfy UI: %+v", findings)
	}
}

func TestProjectCapabilityNeedsRepositoryWideEvidence(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Project.Capabilities = []string{"backend"}
	config.Tests.Suites[len(config.Tests.Suites)-1].Kind = "unit"
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: "api-contract", Kind: "contract", Scope: "module", Modules: []string{"api"}, RunOn: []string{"full"}})
	findings := CoverageFindings(repository.Repository{Config: config}, nil)
	if len(findings) != 1 || findings[0].Subject != "backend" {
		t.Fatalf("module-only evidence must not satisfy project capability: %+v", findings)
	}
}

func TestUnknownProjectCapabilityAddsNoInventedTestRequirement(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Project.Capabilities = []string{"scheduler"}
	if findings := capabilityCoverageFindings(config); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestMutationEvidenceIsOptionalUnlessExplicitlyRequired(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	repo := repository.Repository{Config: config}
	files := []string{"domain/model.go", "api/handler.py", "web/view.ts"}
	if findings := CoverageFindings(repo, files); len(findings) != 0 {
		t.Fatalf("production source must not require mutation evidence by default: %+v", findings)
	}

	config.Tests.RequiredSupplementalKinds = []string{"mutation"}
	if findings := requiredSupplementalKindFindings(config); len(findings) != 1 {
		t.Fatalf("explicit mutation requirement must remain enforceable: %+v", findings)
	}
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{
		Name: "mutation", Kind: "mutation", Scope: "repository", RunOn: []string{"supplemental"},
	})
	if findings := requiredSupplementalKindFindings(config); len(findings) != 0 {
		t.Fatalf("declared mutation suite should satisfy explicit opt-in: %+v", findings)
	}
}

func TestGherkinRequiresExecutableAcceptance(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	findings := gherkinCoverageFindings(config.Tests.Suites, []string{"features/register.feature"})
	if len(findings) != 1 || findings[0].Subject != "gherkin:acceptance" {
		t.Fatalf("findings = %+v", findings)
	}
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: "acceptance", Kind: "acceptance", Scope: "repository", RunOn: []string{"full"}})
	if findings = gherkinCoverageFindings(config.Tests.Suites, []string{"features/register.feature"}); len(findings) != 0 {
		t.Fatalf("acceptance evidence should satisfy Gherkin policy: %+v", findings)
	}
}

func TestConditionalTestStrengthNotesExposeDetectedLanguages(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: planningConfig()}
	notes := Notes(repo, []string{"domain/model.go", "web/view.ts", "features/login.feature"})
	want := []string{
		"conditional executable-specification policy: governed .feature files",
	}
	if !slices.Equal(notes, want) {
		t.Fatalf("notes = %v", notes)
	}
}

func TestDetectedReactAndElectronModulesRequireBehaviorEvidence(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.ActivePolicyModules = []policy.ActivePolicyModule{
		{Name: "react", Root: "web"},
		{Name: "electron", Root: "desktop"},
	}
	findings := conditionalModuleCoverageFindings(config)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: "desktop-e2e", Kind: "e2e", Scope: "repository", RunOn: []string{"full"}})
	if findings = conditionalModuleCoverageFindings(config); len(findings) != 0 {
		t.Fatalf("e2e evidence should satisfy both modules: %+v", findings)
	}
}

func planningConfig() policy.Config {
	modules := []policy.Module{
		{Name: "domain", Paths: []string{"domain/**"}},
		{Name: "api", Paths: []string{"api/**"}, DependsOn: []string{"domain"}},
		{Name: "web", Paths: []string{"web/**"}, DependsOn: []string{"api"}},
	}
	return policy.Config{
		Project: policy.Project{Kind: "application"}, Modules: modules,
		ModuleByName: map[string]int{"domain": 0, "api": 1, "web": 2},
		Tests: policy.Testing{Suites: []policy.TestSuite{
			{Name: "domain-unit", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{"domain"}, RunOn: []string{"focused", "recommended", "full"}},
			{Name: "api-unit", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{"api"}, RunOn: []string{"focused", "recommended", "full"}},
			{Name: "web-unit", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{"web"}, RunOn: []string{"focused", "recommended", "full"}},
			{Name: "full", Kind: "integration", Scope: "repository", Cost: "standard", RunOn: []string{"full"}},
		}},
	}
}

func selectionForPaths(paths ...string) repository.Selection {
	return repository.Selection{
		Candidate: repository.CandidateDelta{AddedOrModified: append([]string{}, paths...)},
		Files:     append([]string{}, paths...),
	}
}

func suiteNames(suites []policy.TestSuite) []string {
	names := make([]string, 0, len(suites))
	for _, suite := range suites {
		names = append(names, suite.Name)
	}
	return names
}

type recordingTestingRunner struct {
	command policy.Command
}

func (runner *recordingTestingRunner) Run(_ context.Context, _ string, command policy.Command) error {
	runner.command = command
	return nil
}

type evidenceTestingRunner struct {
	result runner.Result
	err    error
}

func (testRunner *evidenceTestingRunner) Run(_ context.Context, _ string, _ policy.Command) error {
	return testRunner.err
}

func (testRunner *evidenceTestingRunner) RunWithResult(_ context.Context, _ string, _ policy.Command) (runner.Result, error) {
	return testRunner.result, testRunner.err
}
