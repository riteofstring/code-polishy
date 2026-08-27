package testing

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
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
	want := "Running 1 validation command at focused level.\n[1/1] domain-unit\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q\nwant = %q", got, want)
	}
}

func TestExecutionPlanForDirectTestUsesTheResolvedSelectionBase(t *testing.T) {
	t.Parallel()
	base := "0123456789abcdef0123456789abcdef01234567"
	plan, err := BuildPlan(repository.Repository{Config: planningConfig()}, Request{Changed: repository.Selection{
		Base: base, Files: []string{"domain/model.go"},
	}})
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
	plan, err := BuildPlan(repo, Request{Changed: repository.Selection{Files: []string{"domain/model.go"}}})
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
	plan, err := BuildPlan(repo, Request{Changed: repository.Selection{Files: []string{"api/handler.go"}}})
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

func TestAdviceListsOnlyImpactRelevantSupplementalSuites(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	for _, module := range []string{"domain", "api", "web"} {
		config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{
			Name: module + "-mutation", Kind: "mutation", Scope: "module", Modules: []string{module}, RunOn: []string{"supplemental"},
		})
	}
	repo := repository.Repository{Config: config}
	advice, err := BuildAdvice(repo, repository.Selection{Files: []string{"web/view.ts"}})
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
	advice, err := BuildAdvice(repo, repository.Selection{Files: []string{"web/view.ts"}})
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
	advice, err := BuildAdvice(repo, repository.Selection{Files: []string{"domain/model.go"}})
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
	advice, err := BuildAdvice(repo, repository.Selection{Files: paths})
	if err != nil {
		t.Fatal(err)
	}
	if advice.Suggested != "full" || advice.Reasons[0] != "the change spans 20 governed paths" {
		t.Fatalf("advice = %+v", advice)
	}
}

func TestAdviceUsesFullAtExactlyTwoThirdsDependencyImpact(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: planningConfig()}
	advice, err := BuildAdvice(repo, repository.Selection{Files: []string{"api/handler.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if advice.Suggested != "full" || advice.Reasons[0] != "the change impacts 2 of 3 modules through the dependency graph" {
		t.Fatalf("advice = %+v", advice)
	}
}

func TestMergeDecisionRequiresOptIn(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: planningConfig()}
	decision, err := BuildMergeDecision(repo, repository.Selection{Files: []string{"web/view.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Level != MergeLevelFull || decision.Reasons[0] != "adaptive merge verification is not configured" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMergeDecisionAllowsNarrowConfiguredModule(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Verification.MergeGate = &policy.MergeGate{RecommendedModules: []string{"web"}}
	decision, err := BuildMergeDecision(repository.Repository{Config: config}, repository.Selection{Files: []string{"web/view.ts"}})
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
	decision, err := BuildMergeDecision(repository.Repository{Config: config}, repository.Selection{Files: []string{"api/handler.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Level != MergeLevelFull || decision.Reasons[0] != `changed module "api" is not configured for recommended merge verification` {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMergeDecisionFailsClosedForUnownedOrAmbiguousPath(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Verification.MergeGate = &policy.MergeGate{RecommendedModules: []string{"web"}}
	decision, err := BuildMergeDecision(repository.Repository{Config: config}, repository.Selection{Files: []string{"README.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Level != MergeLevelFull || !strings.Contains(decision.Reasons[0], "belongs to 0 modules") {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMergeDecisionHonorsRepositoryWideExpansion(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Verification.MergeGate = &policy.MergeGate{RecommendedModules: []string{"web"}}
	decision, err := BuildMergeDecision(repository.Repository{Config: config}, repository.Selection{Files: []string{"web/view.ts"}, All: true})
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
	selection := repository.Selection{Files: []string{"a.go", "b.go", "c.go"}}
	plan := Plan{ChangedModules: []string{"a", "b", "c"}, ImpactedModules: []string{"a", "b", "c"}}
	suggested, reasons := recommendScope(config, selection, plan)
	if suggested != "full" || reasons[0] != "the change directly crosses 3 module boundaries" {
		t.Fatalf("suggested=%q reasons=%v", suggested, reasons)
	}
}

func TestRecommendedRepositorySuiteWithoutPathsRunsForAnyChange(t *testing.T) {
	t.Parallel()
	config := planningConfig()
	config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: "global-contract", Scope: "repository", RunOn: []string{"recommended", "full"}})
	repo := repository.Repository{Config: config}
	plan, err := BuildPlan(repo, Request{Recommended: true, Changed: repository.Selection{Files: []string{"web/view.ts"}}})
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
