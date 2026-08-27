package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestDoctorAcceptsCompleteContentRepository(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	report, err := policyEngine.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v", report.Findings)
	}
}

func TestCheckChangeBoundaryReportsSeparateNewArtifactPaths(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	output, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, "new-plan.md", "new plan\n", 0o600)

	report, err := CheckChangeBoundary(root, root, "", strings.TrimSpace(string(output)), []string{"content"}, nil, []string{"new-plan.md"})
	if err != nil {
		t.Fatal(err)
	}
	if report.ChangeBoundary == nil || !slices.Equal(report.ChangeBoundary.NewPaths, []string{"new-plan.md"}) || len(report.ChangeBoundary.Paths) != 0 {
		t.Fatalf("change boundary = %+v", report.ChangeBoundary)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v", report.Findings)
	}
}

func TestDoctorRejectsExcludedExecutableSource(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, []string{"scripts/**"})
	writeEngineFile(t, root, "scripts/tool.py", "print('hidden')\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	report, err := policyEngine.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range report.Findings {
		found = found || finding.Check == "policy.scope"
	}
	if !found {
		t.Fatalf("expected scope finding: %+v", report.Findings)
	}
}

func TestAdvisoriesDoNotFailReport(t *testing.T) {
	t.Parallel()
	report := (&Engine{}).finishWithAdvisories(nil, []policy.Advisory{{Check: "portability.machinePath", Path: "content/config.go", Subject: "2"}}, nil)
	if len(report.Findings) != 0 || len(report.Advisories) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if HasFindings(report) {
		t.Fatal("advisories must not affect exit status")
	}
}

func TestCombinedReportsDeduplicateAdvisories(t *testing.T) {
	t.Parallel()
	advisory := policy.Advisory{Check: "portability.machinePath", Path: "content/config.go", Subject: "2"}
	report := (&Engine{}).combine(Report{Advisories: []policy.Advisory{advisory}}, Report{Advisories: []policy.Advisory{advisory}})
	if len(report.Advisories) != 1 {
		t.Fatalf("advisories = %+v", report.Advisories)
	}
}

func TestSupplyChainProfilesSeparateOfflineAndOnlineProviders(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "README.md", "sample\n", 0o600)
	config := policy.Config{Checks: []policy.Command{
		{Name: "local", Provides: []string{"lock-sync"}, Argv: []string{"true"}, RunOn: []string{"supply-chain"}, TimeoutSeconds: 5},
		{Name: "freshness", Provides: []string{"release-age"}, Argv: []string{"true"}, RunOn: []string{"supply-chain-online"}, TimeoutSeconds: 5},
		{Name: "security", Provides: []string{"security"}, Argv: []string{"true"}, RunOn: []string{"security"}, TimeoutSeconds: 5},
	}}
	commandRunner := &recordingEngineRunner{}
	policyEngine := Engine{Repository: repository.Repository{Root: root, PolicyRoot: root, Config: config}, Runner: commandRunner}
	if _, err := policyEngine.SupplyChain(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(commandRunner.commands, []string{"local"}) {
		t.Fatalf("offline commands = %v", commandRunner.commands)
	}
	commandRunner.commands = nil
	if _, err := policyEngine.SupplyChain(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(commandRunner.commands, []string{"local", "freshness", "security"}) {
		t.Fatalf("online commands = %v", commandRunner.commands)
	}
}

func TestPlanMergeGateExecutionIncludesArtifactSecurityProcesses(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	base := gitEngineOutput(t, root, "rev-parse", "HEAD")
	config, err := os.ReadFile(filepath.Join(root, ".code-polishy.json"))
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(config), `"supplyChain": {}`, `"supplyChain":{"artifactSecurity":{"targets":[{"name":"archive","mode":"archive","archive":"content/image.tar"}]}}`, 1)
	if updated == string(config) {
		t.Fatal("artifact-security planner fixture did not update policy")
	}
	writeEngineFile(t, root, ".code-polishy.json", updated, 0o600)
	writeEngineFile(t, root, "content/image.tar", "archive", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := policyEngine.PlanMergeGateExecution(base)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Level != testpolicy.MergeLevelFull {
		t.Fatalf("merge level = %q", plan.Level)
	}
	names := mergeGateExecutionNames(plan.Commands)
	if !slices.Contains(names, "artifact-security-docker-preflight") || !slices.Contains(names, "artifact-security-docker-target-archive-observed-create") ||
		!slices.Contains(names, "artifact-security-docker-remove-scanner") {
		t.Fatalf("artifact-security processes were omitted from full plan: %v", names)
	}
}

func TestSupplyChainAggregatesProvidersAfterStaticFinding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "package.json", `{"packageManager":"npm@11.0.0","dependencies":{"react":"^19.2.6"}}`+"\n", 0o600)
	config := policy.Config{Checks: []policy.Command{
		{Name: "local", Provides: []string{"lock-sync"}, Argv: []string{"true"}, RunOn: []string{"supply-chain"}, TimeoutSeconds: 5},
	}}
	commandRunner := &recordingEngineRunner{}
	policyEngine := Engine{Repository: repository.Repository{Root: root, PolicyRoot: root, Config: config}, Runner: commandRunner}
	report, err := policyEngine.SupplyChain(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("expected the loose dependency to remain a finding")
	}
	if !slices.Equal(commandRunner.commands, []string{"local"}) {
		t.Fatalf("independent provider was skipped after a static finding: %v", commandRunner.commands)
	}
}

func TestSuiteSummaryExposesCostAndNames(t *testing.T) {
	t.Parallel()
	suites := []policy.TestSuite{
		{Name: "unit", Cost: "quick"},
		{Name: "integration", Cost: "standard"},
		{Name: "browser", Cost: "expensive"},
	}
	want := "3 suites (quick=1, standard=1, expensive=1): unit, integration, browser"
	if got := suiteSummary(suites); got != want {
		t.Fatalf("summary = %q", got)
	}
}

func TestMergeGateSuiteCommandsPreserveConfiguredKinds(t *testing.T) {
	t.Parallel()
	commands := mergeGateSuiteCommands([]policy.TestSuite{
		{Name: "unit", Kind: "unit", Scope: "module", Cost: "quick"},
		{Name: "browser", Kind: "browser", Scope: "repository", Cost: "expensive"},
	})
	if len(commands) != 2 || commands[0].Kind != "unit" || commands[1].Kind != "browser" {
		t.Fatalf("merge-gate suite kinds = %+v", commands)
	}
}

func TestTestPlanDoesNotExecuteSuites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "domain/model.go", "package domain\n", 0o600)
	config := policy.Config{
		Modules:      []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}},
		ModuleByName: map[string]int{"domain": 0},
		Tests: policy.Testing{Suites: []policy.TestSuite{
			{Name: "domain-unit", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{"domain"}, RunOn: []string{"focused", "recommended", "full"}},
			{Name: "repository-full", Kind: "integration", Scope: "repository", Cost: "standard", RunOn: []string{"full"}},
			{Name: "domain-mutation", Kind: "mutation", Scope: "module", Cost: "expensive", Modules: []string{"domain"}, RunOn: []string{"supplemental"}},
		}},
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine := Engine{Repository: repository.Repository{Root: root, PolicyRoot: root, Config: config}, Runner: commandRunner}
	report, err := policyEngine.TestPlan("")
	if err != nil {
		t.Fatal(err)
	}
	if len(commandRunner.commands) != 0 {
		t.Fatalf("planner executed suites: %v", commandRunner.commands)
	}
	if report.MergePolicy != nil {
		t.Fatalf("no-base planner reported merge policy: %+v", report.MergePolicy)
	}
	if !slices.Contains(report.Notes, "diagnostic ordinary advice: full") {
		t.Fatalf("plan notes = %v", report.Notes)
	}
	if strings.Contains(strings.Join(report.Notes, "\n"), "merge-gate --base") {
		t.Fatalf("no-base planner claimed a merge-gate command: %v", report.Notes)
	}
	if !slices.Contains(report.Notes, "supplemental quality (run separately with test --supplemental): 1 suites (expensive=1): domain-mutation") {
		t.Fatalf("supplemental plan note missing: %v", report.Notes)
	}
	if len(report.Tables) != 1 || report.Tables[0].Title != "TEST LEVELS (q=quick, s=standard, e=expensive; *=advice)" {
		t.Fatalf("test-level table = %+v", report.Tables)
	}
	wantRows := [][]string{
		{"focused", "1 (q=1)", "routine", "test --changed"},
		{"recommended", "1 (q=1)", "advice", "merge target required"},
		{"full *", "2 (q=1, s=1)", "advice", "merge target required"},
		{"supplemental", "1 (e=1)", "separate", "test --supplemental"},
	}
	if !slices.EqualFunc(report.Tables[0].Rows, wantRows, func(left, right []string) bool { return slices.Equal(left, right) }) {
		t.Fatalf("test-level rows = %v", report.Tables[0].Rows)
	}
	if width := tableWidth(report.Tables[0]); width > 80 {
		t.Fatalf("test-level table width = %d, want <= 80", width)
	}
}

func mergeGateExecutionNames(commands []MergeGateExecutionCommand) []string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Command.Name)
	}
	return names
}

func gitEngineOutput(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git %v: %v", arguments, err)
	}
	return strings.TrimSpace(string(output))
}

func TestTestPlanWithBaseReportsPolicySelectedMergeLevelWithoutExecutingSuites(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		change      func(t *testing.T, root string)
		wantLevel   string
		wantReasons []string
	}{
		{
			name: "recommended", change: func(t *testing.T, root string) {
				t.Helper()
				writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
			},
			wantLevel: "recommended",
			wantReasons: []string{
				"every changed path belongs to exactly one module configured for recommended merge verification",
				"the change is limited to 1 governed paths and impacts 1 of 1 modules",
			},
		},
		{
			name: "full", change: func(t *testing.T, root string) {
				t.Helper()
				configPath := filepath.Join(root, policy.ConfigFilename)
				contents, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				writeEngineFile(t, root, policy.ConfigFilename, string(contents)+"\n", 0o600)
			},
			wantLevel: "full",
			wantReasons: []string{
				"impact expanded to the whole repository because this is a new repository or the change includes a deletion or policy, dependency, workflow, or container input",
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := contentRepository(t, nil)
			initializeEngineGitRepository(t, root)
			testCase.change(t, root)
			policyEngine, err := Open(root, root, "")
			if err != nil {
				t.Fatal(err)
			}
			commandRunner := &recordingEngineRunner{}
			policyEngine.Runner = commandRunner

			report, err := policyEngine.TestPlan("main")
			if err != nil {
				t.Fatal(err)
			}
			if len(commandRunner.commands) != 0 {
				t.Fatalf("planner executed suites: %v", commandRunner.commands)
			}
			if report.MergePolicy == nil || report.MergePolicy.Level != testCase.wantLevel || report.MergePolicy.Base != "main" || !slices.Equal(report.MergePolicy.Reasons, testCase.wantReasons) {
				t.Fatalf("merge policy = %+v", report.MergePolicy)
			}
			if !slices.Contains(report.Notes, "ordinary merge execution: code-polishy merge-gate --base main") {
				t.Fatalf("plan notes = %v", report.Notes)
			}
			if len(report.Tables) != 1 || report.Tables[0].Title != "TEST LEVELS (q=quick, s=standard, e=expensive; *=selected)" {
				t.Fatalf("test-level table = %+v", report.Tables)
			}
			if width := tableWidth(report.Tables[0]); width > 80 {
				t.Fatalf("test-level table width = %d, want <= 80", width)
			}
			for _, row := range report.Tables[0].Rows {
				if slices.Contains(row, "choose") {
					t.Fatalf("planner retained a choice state: %v", report.Tables[0].Rows)
				}
			}
		})
	}
}

func tableWidth(table Table) int {
	width := 1
	for columnIndex, column := range table.Columns {
		columnWidth := len(column)
		for _, row := range table.Rows {
			if columnIndex < len(row) && len(row[columnIndex]) > columnWidth {
				columnWidth = len(row[columnIndex])
			}
		}
		width += columnWidth + 3
	}
	return width
}

func TestVerifyDoesNotRunSupplementalSuites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	config := policy.Config{Tests: policy.Testing{Suites: []policy.TestSuite{
		{Name: "full", Kind: "contract", Scope: "repository", Cost: "standard", RunOn: []string{"full"}},
		{Name: "mutation", Kind: "mutation", Scope: "repository", Cost: "expensive", RunOn: []string{"supplemental"}},
	}}}
	commandRunner := &recordingEngineRunner{}
	policyEngine := Engine{Repository: repository.Repository{Root: root, PolicyRoot: root, Config: config}, Runner: commandRunner}
	if _, err := policyEngine.Verify(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(commandRunner.commands, []string{"full"}) {
		t.Fatalf("verify commands = %v", commandRunner.commands)
	}
}

func TestMergeGateRunsRecommendedPipelineForConfiguredContentChange(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v", report.Findings)
	}
	if report.MergePolicy == nil || report.MergePolicy.Level != "recommended" || report.MergePolicy.Base != "main" {
		t.Fatalf("merge policy = %+v", report.MergePolicy)
	}
	if !slices.Equal(commandRunner.commands, []string{"content-check", "focused", "content-build", "offline-supply"}) {
		t.Fatalf("commands = %v", commandRunner.commands)
	}
}

func TestDocumentationMergeLaneSchedulesOnlyFocusedDocumentationWork(t *testing.T) {
	t.Parallel()
	root := documentationRepository(t)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "docs/guide.md", "# Guide\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}

	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Level != testpolicy.MergeLevelRecommended {
		t.Fatalf("merge level = %q", plan.Level)
	}
	if got, want := mergeGateExecutionNames(plan.Commands), []string{"documentation-contract", "offline-assurance"}; !slices.Equal(got, want) {
		t.Fatalf("recommended documentation plan = %v, want %v", got, want)
	}
}

func TestDocumentationMergeLaneFailsClosedOutsideMarkdownDocs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{
			name: "root Markdown", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "README.md", "# Root\n", 0o600)
			},
		},
		{
			name: "template", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "templates/guide.md", "# Template\n", 0o600)
			},
		},
		{
			name: "skill", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "skills/guide.md", "# Skill\n", 0o600)
			},
		},
		{
			name: "workflow", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, ".github/workflows/verify.yml", "name: verify\n", 0o600)
			},
		},
		{
			name: "deletion", mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "docs/deletable.md")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "policy", mutate: func(t *testing.T, root string) {
				configPath := filepath.Join(root, policy.ConfigFilename)
				contents, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				writeEngineFile(t, root, policy.ConfigFilename, string(contents)+"\n", 0o600)
			},
		},
		{
			name: "dependency", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "package.json", "{}\n", 0o600)
			},
		},
		{
			name: "ambiguous Markdown", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "docs/ambiguous.md", "# Ambiguous\n", 0o600)
			},
		},
		{
			name: "non Markdown", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "docs/data.txt", "not Markdown\n", 0o600)
			},
		},
		{
			name: "documentation symlink", mutate: func(t *testing.T, root string) {
				if err := os.Symlink("index.md", filepath.Join(root, "docs", "linked.md")); err != nil {
					t.Skipf("create documentation symlink: %v", err)
				}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := documentationRepository(t)
			initializeEngineGitRepository(t, root)
			testCase.mutate(t, root)
			policyEngine, err := Open(root, root, "")
			if err != nil {
				t.Fatal(err)
			}
			plan, err := policyEngine.PlanMergeGateExecution("main")
			if err != nil {
				t.Fatal(err)
			}
			if plan.Level != testpolicy.MergeLevelFull {
				t.Fatalf("merge level = %q, want full", plan.Level)
			}
		})
	}
}

func TestMergeGateForcesFullWhenPolicyConfigurationChanges(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	configPath := filepath.Join(root, policy.ConfigFilename)
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(contents, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if report.MergePolicy == nil || report.MergePolicy.Level != "full" {
		t.Fatalf("merge policy = %+v", report.MergePolicy)
	}
	if !slices.Equal(commandRunner.commands, []string{"content-check", "focused", "full", "content-build", "offline-supply"}) {
		t.Fatalf("commands = %v", commandRunner.commands)
	}
}

func TestMergeGateDoesNotStartLaterSuitesAfterSuiteFailure(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	configPath := filepath.Join(root, policy.ConfigFilename)
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, policy.ConfigFilename, string(contents)+"\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &failingEngineRunner{failure: "focused"}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Subject != "focused" {
		t.Fatalf("findings = %+v", report.Findings)
	}
	if !slices.Equal(commandRunner.commands, []string{"content-check", "focused"}) {
		t.Fatalf("merge gate ran after a failed suite: %v", commandRunner.commands)
	}
}

func TestGateAlwaysReportsFullPolicyLevel(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.Gate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.MergePolicy == nil || report.MergePolicy.Level != "full" {
		t.Fatalf("merge policy = %+v", report.MergePolicy)
	}
	if !slices.Equal(commandRunner.commands, []string{"content-check", "focused", "full", "content-build", "offline-supply"}) {
		t.Fatalf("commands = %v", commandRunner.commands)
	}
}

func TestCheckNamedRunsOnlyTheExactConfiguredCheck(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckNamed(context.Background(), []string{"content-check"})
	if err != nil || len(report.Findings) != 0 {
		t.Fatalf("report=%+v error=%v", report, err)
	}
	if !slices.Equal(commandRunner.commands, []string{"content-check"}) {
		t.Fatalf("commands = %v", commandRunner.commands)
	}
	if _, err := policyEngine.CheckNamed(context.Background(), []string{"absent"}); err == nil {
		t.Fatal("unknown exact check was accepted")
	}
}

func TestDependencyReviewUsesMergeBaseAndRendersChangeTable(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.DependencyReview(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tables) != 1 || report.Tables[0].Title != "DEPENDENCY REVIEW" || len(report.Tables[0].Rows) != 0 {
		t.Fatalf("tables = %+v", report.Tables)
	}
	if !slices.Equal(commandRunner.commands, []string{"offline-supply"}) {
		t.Fatalf("commands = %v", commandRunner.commands)
	}
}

func contentRepository(t *testing.T, excludes []string) string {
	t.Helper()
	root := t.TempDir()
	excludeJSON := ""
	for index, item := range excludes {
		if index > 0 {
			excludeJSON += ","
		}
		excludeJSON += `"` + item + `"`
	}
	config := `{
  "version": 3,
  "project": {"kind": "content"},
  "scope": {"exclude": [` + excludeJSON + `]},
  "quality": {},
  "modules": [{"name":"content","paths":["content/**"]}],
  "verification": {"mergeGate":{"recommendedModules":["content"]}},
  "checks": [
    {"name":"content-check","provides":["content-validation"],"argv":["true"],"modules":["content"],"runOn":["gate"]},
    {"name":"content-build","provides":["content-build"],"argv":["true"],"modules":["content"],"runOn":["build"]},
    {"name":"offline-supply","provides":["content-integrity"],"argv":["true"],"runOn":["supply-chain"]}
  ],
  "tests": {"suites":[
    {"name":"focused","kind":"content","scope":"module","modules":["content"],"argv":["go","test","./..."]},
    {"name":"full","kind":"content","scope":"repository","argv":["go","test","./..."]}
  ]},
  "supplyChain": {},
  "exceptions": []
}`
	writeEngineFile(t, root, ".code-polishy.json", config+"\n", 0o600)
	writeEngineFile(t, root, "content/data.json", "{}\n", 0o600)
	writePinnedGoToolchain(t, root)
	return root
}

func documentationRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	config := `{
  "version": 3,
  "project": {"kind": "tooling", "capabilities": ["cli"]},
  "scope": {},
  "quality": {},
  "modules": [
    {"name": "docs", "paths": ["docs/**/*.md"]},
    {"name": "tooling", "paths": ["templates/**", "skills/**"]},
    {"name": "source", "paths": ["internal/**"]},
    {"name": "shared", "paths": ["docs/ambiguous.md"]}
  ],
  "verification": {"mergeGate": {"recommendedModules": ["docs"]}},
  "checks": [
    {"name": "go-build", "provides": ["build"], "argv": ["true"], "modules": ["source"], "runOn": ["build"]},
    {"name": "offline-assurance", "provides": ["lock-sync"], "argv": ["true"], "runOn": ["supply-chain"]},
    {"name": "online-supply", "provides": ["release-age"], "argv": ["true"], "runOn": ["supply-chain-online"]}
  ],
  "tests": {"suites": [
    {"name": "documentation-contract", "kind": "contract", "scope": "module", "modules": ["docs"], "argv": ["./scripts/check-documentation.sh"]},
    {"name": "repository-full", "kind": "integration", "scope": "repository", "argv": ["./scripts/test-repository.sh"]}
  ]},
  "supplyChain": {"artifactSecurity": {"targets": [{"name": "archive", "mode": "archive", "archive": "artifacts/docs.tar"}]}},
  "exceptions": []
}`
	writeEngineFile(t, root, policy.ConfigFilename, config+"\n", 0o600)
	writeEngineFile(t, root, "docs/index.md", "# Documentation\n", 0o600)
	writeEngineFile(t, root, "docs/deletable.md", "# Deletable\n", 0o600)
	writeEngineFile(t, root, "internal/main.go", "package internal\n", 0o600)
	writeEngineFile(t, root, "artifacts/docs.tar", "archive", 0o600)
	writePinnedGoToolchain(t, root)
	return root
}

// A policy root carries the pinned Go toolchain Code Polishy runs, so a fixture
// whose configured commands name `go` carries one too. There is no ambient
// toolchain to fall back to.
func writePinnedGoToolchain(t *testing.T, policyRoot string) {
	t.Helper()
	writeEngineFile(t, policyRoot, "scripts/go_version.txt", "go1.24.0\n", 0o600)
	directory := filepath.Join(policyRoot, ".tools", "go", runtime.GOOS+"-"+runtime.GOARCH, "go", "bin")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go", "gofmt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func initializeEngineGitRepository(t *testing.T, root string) {
	t.Helper()
	commands := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "policy@example.test"},
		{"config", "user.name", "Policy Test"},
		{"add", "."},
		{"commit", "-m", "initial"},
	}
	for _, arguments := range commands {
		command := exec.Command("git", arguments...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
}

func writeEngineFile(t *testing.T, root, path, contents string, mode os.FileMode) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

type recordingEngineRunner struct {
	commands []string
}

type failingEngineRunner struct {
	commands []string
	failure  string
}

func (runner *failingEngineRunner) Run(_ context.Context, _ string, command policy.Command) error {
	runner.commands = append(runner.commands, command.Name)
	if command.Name == runner.failure {
		return errors.New("configured failure")
	}
	return nil
}

func (runner *recordingEngineRunner) Run(_ context.Context, _ string, command policy.Command) error {
	runner.commands = append(runner.commands, command.Name)
	return nil
}
