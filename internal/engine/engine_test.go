package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestDoctorAndCheckRejectStaleDesignDocumentation(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	configPath := filepath.Join(root, policy.ConfigFilename)
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(
		string(config),
		`"quality": {},`,
		`"quality": {}, "documentation":{"design":[{"path":"docs/design/content.md","module":"content"}]},`,
		1,
	)
	writeEngineFile(t, root, policy.ConfigFilename, updated, 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	doctor, err := policyEngine.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	check := policyEngine.Check(t.Context(), repository.Selection{Files: []string{"content/data.json"}, All: true}, "check")
	for name, report := range map[string]Report{"doctor": doctor, "check": check} {
		if !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
			return finding.Check == repository.DesignDocumentationCheck && finding.Subject == "docs/design/content.md"
		}) {
			t.Errorf("%s findings = %+v", name, report.Findings)
		}
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
	for _, path := range []string{"scripts/tool.py", "scripts/style.css", "scripts/page.html", "scripts/tool.ps1"} {
		t.Run(path, func(t *testing.T) {
			root := contentRepository(t, []string{"scripts/**"})
			writeEngineFile(t, root, path, "value\n", 0o600)
			policyEngine, err := Open(root, root, "")
			if err != nil {
				t.Fatal(err)
			}
			report, err := policyEngine.Doctor(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
				return finding.Check == "policy.scope" && finding.Path == path
			}) {
				t.Fatalf("findings = %+v", report.Findings)
			}
		})
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

func TestCombinedReportsCanonicalizeAdvisories(t *testing.T) {
	t.Parallel()
	earlier := policy.Advisory{Check: "portability.machinePath", Path: "content/a.go", Subject: "1"}
	later := policy.Advisory{Check: "portability.machinePath", Path: "content/z.go", Subject: "2"}
	report := (&Engine{}).combine(
		Report{Advisories: []policy.Advisory{later, earlier}},
		Report{Advisories: []policy.Advisory{later}},
	)
	if !slices.Equal(report.Advisories, []policy.Advisory{earlier, later}) {
		t.Fatalf("advisories = %+v", report.Advisories)
	}
}

func TestNewTestQualityReminderOrdersAndDeduplicatesPaths(t *testing.T) {
	t.Parallel()
	reminder := NewTestQualityReminder([]string{"internal/engine/engine_test.go", "", "cmd/code-polishy/main_test.go", "internal/engine/engine_test.go"})
	if reminder == nil {
		t.Fatal("reminder = nil")
	}
	want := []string{"cmd/code-polishy/main_test.go", "internal/engine/engine_test.go"}
	if !slices.Equal(reminder.ChangedTestPaths, want) {
		t.Fatalf("paths = %v, want %v", reminder.ChangedTestPaths, want)
	}
}

func TestNewTestQualityReminderOmitsEmptyEvidence(t *testing.T) {
	t.Parallel()
	if reminder := NewTestQualityReminder([]string{""}); reminder != nil {
		t.Fatalf("reminder = %+v", reminder)
	}
}

func TestCombinedReportsDeduplicateTestQualityReminder(t *testing.T) {
	t.Parallel()
	report := (&Engine{}).combine(
		Report{TestQualityReminder: &TestQualityReminder{ChangedTestPaths: []string{"internal/engine/engine_test.go", "cmd/code-polishy/main_test.go"}}},
		Report{TestQualityReminder: &TestQualityReminder{ChangedTestPaths: []string{"cmd/code-polishy/main_test.go", "internal/testing/quality_test.go"}}},
	)
	if report.TestQualityReminder == nil {
		t.Fatal("reminder = nil")
	}
	want := []string{"cmd/code-polishy/main_test.go", "internal/engine/engine_test.go", "internal/testing/quality_test.go"}
	if !slices.Equal(report.TestQualityReminder.ChangedTestPaths, want) {
		t.Fatalf("paths = %v, want %v", report.TestQualityReminder.ChangedTestPaths, want)
	}
}

func TestChangeAwareCheckAddsTestQualityReminderFromItsCandidate(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	path := "content/reminder_test.go"
	writeEngineFile(t, root, path, "package content\n\nimport \"testing\"\n\nfunc TestReminder(t *testing.T) {\n\tif t.Name() == \"\" {\n\t\tt.Fatal(\"missing test name\")\n\t}\n}\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	policyEngine.Runner = &recordingEngineRunner{}
	selection := repository.Selection{Candidate: repository.CandidateDelta{AddedOrModified: []string{path}}, Files: []string{path}}
	changeAware := policyEngine.CheckChangeAware(t.Context(), selection, "check")
	ordinary := policyEngine.Check(t.Context(), selection, "check")
	assertTestQualityReminderPaths(t, changeAware, []string{path})
	assertTestQualityReminderPaths(t, ordinary, nil)
}

func TestDirectChangedTestAddsReminderAndExplicitScopesStayQuiet(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		request func(repository.Selection) testpolicy.Request
		want    bool
	}{
		"changed scope": {
			request: func(selection repository.Selection) testpolicy.Request { return testpolicy.Request{Changed: selection} },
			want:    true,
		},
		"recommended": {
			request: func(selection repository.Selection) testpolicy.Request {
				return testpolicy.Request{Recommended: true, Changed: selection}
			},
		},
		"full": {
			request: func(selection repository.Selection) testpolicy.Request {
				return testpolicy.Request{Full: true, Changed: selection}
			},
		},
		"supplemental": {
			request: func(selection repository.Selection) testpolicy.Request {
				return testpolicy.Request{Supplemental: true, Changed: selection}
			},
		},
		"module": {
			request: func(selection repository.Selection) testpolicy.Request {
				return testpolicy.Request{Modules: []string{"content"}, Changed: selection}
			},
		},
		"suite": {
			request: func(selection repository.Selection) testpolicy.Request {
				return testpolicy.Request{Suites: []string{"focused"}, Changed: selection}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := contentRepository(t, nil)
			path := "content/reminder_test.go"
			writeEngineFile(t, root, path, "package content\n", 0o600)
			policyEngine, err := Open(root, root, "")
			if err != nil {
				t.Fatal(err)
			}
			policyEngine.Runner = &recordingEngineRunner{}
			selection := repository.Selection{Candidate: repository.CandidateDelta{AddedOrModified: []string{path}}, Files: []string{path}}
			report, err := policyEngine.Test(t.Context(), testCase.request(selection))
			if err != nil {
				t.Fatal(err)
			}
			if testCase.want {
				assertTestQualityReminderPaths(t, report, []string{path})
				return
			}
			assertTestQualityReminderPaths(t, report, nil)
		})
	}
}

func TestTestPlanAddsReminderForCurrentAndTrustedBaseCandidates(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	path := "content/reminder_test.go"
	writeEngineFile(t, root, path, "package content\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{"", "main"} {
		report, err := policyEngine.TestPlan(base)
		if err != nil {
			t.Fatal(err)
		}
		assertTestQualityReminderPaths(t, report, []string{path})
	}
}

func TestMergeGateAddsReminderFromTrustedBaseCandidateAtEveryLevel(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		level  string
		root   func(*testing.T) string
		change func(*testing.T, string) string
	}{
		{
			name:  "documentation",
			level: testpolicy.MergeLevelDocumentation,
			root:  documentationRepository,
			change: func(t *testing.T, root string) string {
				path := "tests/reminder.md"
				writeEngineFile(t, root, path, "# Reminder\n", 0o600)
				return path
			},
		},
		{
			name:  "recommended",
			level: testpolicy.MergeLevelRecommended,
			root:  func(t *testing.T) string { return contentRepository(t, nil) },
			change: func(t *testing.T, root string) string {
				path := "content/reminder_test.go"
				writeEngineFile(t, root, path, "package content\n", 0o600)
				return path
			},
		},
		{
			name:  "full",
			level: testpolicy.MergeLevelFull,
			root:  func(t *testing.T) string { return contentRepository(t, nil) },
			change: func(t *testing.T, root string) string {
				path := "content/reminder_test.go"
				writeEngineFile(t, root, path, "package content\n", 0o600)
				configPath := filepath.Join(root, policy.ConfigFilename)
				contents, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				writeEngineFile(t, root, policy.ConfigFilename, string(contents)+"\n", 0o600)
				return path
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := testCase.root(t)
			initializeEngineGitRepository(t, root)
			path := testCase.change(t, root)
			policyEngine, err := Open(root, root, "")
			if err != nil {
				t.Fatal(err)
			}
			policyEngine.Runner = &recordingEngineRunner{}
			report, err := policyEngine.MergeGate(t.Context(), "main")
			if err != nil {
				t.Fatal(err)
			}
			if report.MergePolicy == nil || report.MergePolicy.Level != testCase.level {
				t.Fatalf("merge policy = %+v, want level %q", report.MergePolicy, testCase.level)
			}
			assertTestQualityReminderPaths(t, report, []string{path})
		})
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
	if !slices.Contains(commandRunner.commands, "local") || slices.Contains(commandRunner.commands, "freshness") || slices.Contains(commandRunner.commands, "security") {
		t.Fatalf("offline commands = %v", commandRunner.commands)
	}
	commandRunner.commands = nil
	if _, err := policyEngine.SupplyChain(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(commandRunner.commands, "local") || !slices.Contains(commandRunner.commands, "freshness") || !slices.Contains(commandRunner.commands, "security") {
		t.Fatalf("online commands = %v", commandRunner.commands)
	}
}

func TestPlanMergeGateExecutionIncludesArtifactSecurityBoundary(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	base := gitEngineOutput(t, root, "rev-parse", "HEAD")
	config, err := os.ReadFile(filepath.Join(root, ".code-polishy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(config, &document); err != nil {
		t.Fatal(err)
	}
	supply, ok := document["supplyChain"].(map[string]any)
	if !ok {
		t.Fatal("content fixture has no supply-chain configuration")
	}
	supply["artifactSecurity"] = map[string]any{"targets": []any{map[string]any{
		"name": "archive", "mode": "archive", "archive": "content/image.tar",
	}}}
	updated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, ".code-polishy.json", string(updated), 0o600)
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
	artifactCommands := 0
	for _, command := range plan.Commands {
		if !slices.Equal(command.Command.ExclusiveResources, []string{"artifact-security"}) {
			continue
		}
		artifactCommands++
		if command.Kind != "check" || command.Scope != "repository" {
			t.Fatalf("artifact-security command escaped its planned check boundary: %+v", command)
		}
	}
	if artifactCommands == 0 {
		t.Fatal("full merge plan omitted the configured artifact-security boundary")
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
	if !slices.Contains(commandRunner.commands, "local") {
		t.Fatalf("independent provider was skipped after a static finding: %v", commandRunner.commands)
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
	if !strings.Contains(strings.Join(report.Notes, "\n"), "ordinary advice: full") {
		t.Fatalf("plan notes = %v", report.Notes)
	}
	if strings.Contains(strings.Join(report.Notes, "\n"), "merge-gate --base") {
		t.Fatalf("no-base planner claimed a merge-gate command: %v", report.Notes)
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "supplemental") || !strings.Contains(notes, "test --supplemental") || !strings.Contains(notes, "domain-mutation") {
		t.Fatalf("supplemental plan note missing: %v", report.Notes)
	}
	if len(report.Tables) != 1 || !strings.Contains(report.Tables[0].Title, "TEST LEVELS") {
		t.Fatalf("test-level table = %+v", report.Tables)
	}
	seenLevels := map[string]bool{
		"focused": false, "recommended": false, "full": false, "supplemental": false,
	}
	for _, row := range report.Tables[0].Rows {
		if len(row) == 0 {
			continue
		}
		for level := range seenLevels {
			if strings.HasPrefix(row[0], level) {
				seenLevels[level] = true
			}
		}
	}
	for level, seen := range seenLevels {
		if !seen {
			t.Fatalf("test-level table omitted %s: %v", level, report.Tables[0].Rows)
		}
	}
	assertTestLevelHasSuites(t, report.Tables[0], "supplemental")
	if width := tableWidth(report.Tables[0]); width > 80 {
		t.Fatalf("test-level table width = %d, want <= 80", width)
	}
}

func assertTestLevelHasSuites(t *testing.T, table Table, level string) {
	t.Helper()
	index := slices.IndexFunc(table.Rows, func(row []string) bool {
		return len(row) > 0 && strings.HasPrefix(row[0], level)
	})
	if index < 0 || len(table.Rows[index]) < 2 || table.Rows[index][1] == "none" {
		t.Fatalf("configured %s suites were reported as absent: %v", level, table.Rows)
	}
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
		name           string
		change         func(t *testing.T, root string)
		wantLevel      string
		reasonContains string
	}{
		{
			name: "recommended", change: func(t *testing.T, root string) {
				t.Helper()
				writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
			},
			wantLevel:      "recommended",
			reasonContains: "configured for recommended merge verification",
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
			wantLevel:      "full",
			reasonContains: "whole repository",
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
			if report.MergePolicy == nil || report.MergePolicy.Level != testCase.wantLevel || report.MergePolicy.Base != "main" ||
				!strings.Contains(strings.Join(report.MergePolicy.Reasons, "\n"), testCase.reasonContains) {
				t.Fatalf("merge policy = %+v", report.MergePolicy)
			}
			if !strings.Contains(strings.Join(report.Notes, "\n"), "merge-gate --base main") {
				t.Fatalf("plan notes = %v", report.Notes)
			}
			if len(report.Tables) != 1 || !strings.Contains(report.Tables[0].Title, "TEST LEVELS") {
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
	if !slices.Contains(commandRunner.commands, "full") || slices.Contains(commandRunner.commands, "mutation") {
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
	for _, name := range []string{"content-check", "focused", "content-build", "offline-supply"} {
		if !slices.Contains(commandRunner.commands, name) {
			t.Fatalf("recommended merge gate omitted %q: %v", name, commandRunner.commands)
		}
	}
	if slices.Contains(commandRunner.commands, "full") {
		t.Fatalf("commands = %v", commandRunner.commands)
	}
}

func TestDocumentationMergeLanePlansZeroApplicationCommands(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{
			name: "root README", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "README.md", "# Root\n", 0o600)
			},
		},
		{
			name: "nested README", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "guides/README.markdown", "# Nested\n", 0o600)
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
			name: "rename", mutate: func(t *testing.T, root string) {
				if err := os.Rename(filepath.Join(root, "docs/deletable.md"), filepath.Join(root, "docs/renamed.md")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "twenty documents", mutate: func(t *testing.T, root string) {
				for index := 0; index < 20; index++ {
					writeEngineFile(t, root, fmt.Sprintf("docs/guide-%02d.md", index), "# Guide\n", 0o600)
				}
			},
		},
		{
			name: "nonregular Markdown", mutate: func(t *testing.T, root string) {
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
			if plan.Level != testpolicy.MergeLevelDocumentation || len(plan.Tests.Suites) != 0 || len(plan.Commands) != 0 {
				t.Fatalf("documentation plan = %+v", plan)
			}
		})
	}
}

func TestDocumentationMergeLaneEscalatesNonDocumentationInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{
			name: "mixed source", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "README.md", "# Root\n", 0o600)
				writeEngineFile(t, root, "internal/main.go", "package internal\n\nfunc Changed() {}\n", 0o600)
			},
		},
		{
			name: "agent guidance", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "AGENTS.md", "# Guidance\n", 0o600)
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
			name: "product input", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "docs/product-input.md", "# Product input\n", 0o600)
			},
		},
		{
			name: "non Markdown", mutate: func(t *testing.T, root string) {
				writeEngineFile(t, root, "docs/data.txt", "not Markdown\n", 0o600)
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

func TestDocumentationMergeGateRunsNoApplicationCommands(t *testing.T) {
	t.Parallel()
	root := documentationRepository(t)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "README.md", "# Root\n", 0o600)
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
	if report.MergePolicy == nil || report.MergePolicy.Level != testpolicy.MergeLevelDocumentation {
		t.Fatalf("merge policy = %+v", report.MergePolicy)
	}
	if len(commandRunner.commands) != 0 {
		t.Fatalf("documentation merge gate ran application commands: %v", commandRunner.commands)
	}
}

func TestDocumentationTestPlanDisclosesZeroApplicationSuites(t *testing.T) {
	t.Parallel()
	root := documentationRepository(t)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "README.md", "# Root\n", 0o600)
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
	if report.MergePolicy == nil || report.MergePolicy.Level != testpolicy.MergeLevelDocumentation {
		t.Fatalf("merge policy = %+v", report.MergePolicy)
	}
	if len(report.Tables) != 1 || len(report.Tables[0].Rows) == 0 || !slices.Equal(report.Tables[0].Rows[0], []string{
		"documentation *", "0 application suites", "automatic", "merge-gate --base REF",
	}) {
		t.Fatalf("test levels = %+v", report.Tables)
	}
	if !slices.Contains(report.Notes, "documentation merge execution: code-polishy merge-gate --base main") {
		t.Fatalf("notes = %v", report.Notes)
	}
	if len(commandRunner.commands) != 0 {
		t.Fatalf("test planner ran application commands: %v", commandRunner.commands)
	}
}

func TestDocumentationChangedTestRunsZeroApplicationSuites(t *testing.T) {
	t.Parallel()
	root := documentationRepository(t)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "README.md", "# Root\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := policyEngine.SelectBase("main")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.Test(context.Background(), testpolicy.Request{Changed: selection})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || !slices.Contains(report.Notes, "documentation-only candidate selected zero application test suites") {
		t.Fatalf("report = %+v", report)
	}
	if len(commandRunner.commands) != 0 {
		t.Fatalf("changed documentation test ran application commands: %v", commandRunner.commands)
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
	for _, name := range []string{"content-check", "focused", "full", "content-build", "offline-supply"} {
		if !slices.Contains(commandRunner.commands, name) {
			t.Fatalf("full merge gate omitted %q: %v", name, commandRunner.commands)
		}
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
	if !slices.Contains(commandRunner.commands, "content-check") || !slices.Contains(commandRunner.commands, "focused") ||
		slices.Contains(commandRunner.commands, "full") || slices.Contains(commandRunner.commands, "content-build") || slices.Contains(commandRunner.commands, "offline-supply") {
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
	for _, name := range []string{"content-check", "focused", "full", "content-build", "offline-supply"} {
		if !slices.Contains(commandRunner.commands, name) {
			t.Fatalf("full gate omitted %q: %v", name, commandRunner.commands)
		}
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
	assertTestQualityReminderPaths(t, report, nil)
	if len(commandRunner.commands) != 1 || !slices.Contains(commandRunner.commands, "content-check") {
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
	if len(commandRunner.commands) != 1 || !slices.Contains(commandRunner.commands, "offline-supply") {
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
  "documentation": {"productInputs": ["docs/product-input.md"]},
  "modules": [
    {"name": "docs", "paths": ["docs/**/*.md"]},
    {"name": "tooling", "paths": ["templates/**", "skills/**"]},
    {"name": "source", "paths": ["internal/**"]},
    {"name": "shared", "paths": ["docs/ambiguous.md"]}
  ],
  "checks": [
    {"name": "go-build", "provides": ["build"], "argv": ["true"], "modules": ["source"], "runOn": ["build"]},
    {"name": "offline-assurance", "provides": ["lock-sync"], "argv": ["true"], "runOn": ["supply-chain"]},
    {"name": "online-supply", "provides": ["release-age"], "argv": ["true"], "runOn": ["supply-chain-online"]}
  ],
  "tests": {"suites": [
    {"name": "docs-product-contract", "kind": "contract", "scope": "module", "modules": ["docs"], "argv": ["go", "test", "./docs/..."]},
    {"name": "repository-full", "kind": "integration", "scope": "repository", "argv": ["./scripts/test-repository.sh"]}
  ]},
  "supplyChain": {"artifactSecurity": {"targets": [{"name": "archive", "mode": "archive", "archive": "artifacts/docs.tar"}]}},
  "exceptions": []
}`
	writeEngineFile(t, root, policy.ConfigFilename, config+"\n", 0o600)
	writeEngineFile(t, root, "docs/index.md", "# Documentation\n", 0o600)
	writeEngineFile(t, root, "docs/deletable.md", "# Deletable\n", 0o600)
	writeEngineFile(t, root, "docs/product-input.md", "# Product input\n", 0o600)
	writeEngineFile(t, root, "internal/main.go", "package internal\n", 0o600)
	writeEngineFile(t, root, "artifacts/docs.tar", "archive", 0o600)
	writePinnedGoToolchain(t, root)
	return root
}

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

func assertTestQualityReminderPaths(t *testing.T, report Report, want []string) {
	t.Helper()
	if len(want) == 0 {
		if report.TestQualityReminder != nil {
			t.Fatalf("test quality reminder = %+v", report.TestQualityReminder)
		}
		return
	}
	if report.TestQualityReminder == nil || !slices.Equal(report.TestQualityReminder.ChangedTestPaths, want) {
		t.Fatalf("test quality reminder = %+v, want paths %v", report.TestQualityReminder, want)
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
