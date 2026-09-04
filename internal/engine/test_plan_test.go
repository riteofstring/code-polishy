package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestTestPlanDoesNotExecuteSuites(t *testing.T) {
	t.Parallel()
	report, commandRunner := testPlanWithoutExecutingSuites(t)
	assertTestPlanDidNotExecuteSuites(t, report, commandRunner)
	assertNoBaseTestPlanNotes(t, report)
	table := testPlanTable(t, report)
	assertTestPlanLevels(t, table)
	assertTestLevelHasSuites(t, table, "supplemental")
	assertSupplementalTestPlanRow(t, table)
}

func testPlanWithoutExecutingSuites(t *testing.T) (Report, *recordingEngineRunner) {
	t.Helper()
	root := t.TempDir()
	writeEngineFile(t, root, "domain/model.go", "package domain\n", 0o600)
	config := policy.Config{
		Version:      3,
		Modules:      []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}},
		ModuleByName: map[string]int{"domain": 0},
		Tests: policy.Testing{RequiredSupplementalKinds: []string{"mutation"}, Suites: []policy.TestSuite{
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
	return report, commandRunner
}

func assertTestPlanDidNotExecuteSuites(t *testing.T, report Report, commandRunner *recordingEngineRunner) {
	t.Helper()
	if len(commandRunner.commands) != 0 {
		t.Fatalf("planner executed suites: %v", commandRunner.commands)
	}
	if report.MergePolicy != nil {
		t.Fatalf("no-base planner reported merge policy: %+v", report.MergePolicy)
	}
}

func assertNoBaseTestPlanNotes(t *testing.T, report Report) {
	t.Helper()
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "ordinary advice: full") {
		t.Fatalf("plan notes = %v", report.Notes)
	}
	if strings.Contains(notes, "merge-gate --base") {
		t.Fatalf("no-base planner claimed a merge-gate command: %v", report.Notes)
	}
	assertSupplementalTestPlanNotes(t, report, notes)
}

func assertSupplementalTestPlanNotes(t *testing.T, report Report, notes string) {
	t.Helper()
	if !strings.Contains(notes, "available only for a caller request, checked-in event workflow, or stable release checklist") ||
		!strings.Contains(notes, "requiredSupplementalKinds do not select work") ||
		!strings.Contains(notes, "test --supplemental") || !strings.Contains(notes, "domain-mutation") {
		t.Fatalf("supplemental plan note missing: %v", report.Notes)
	}
}

func testPlanTable(t *testing.T, report Report) Table {
	t.Helper()
	if len(report.Tables) < 1 || !strings.Contains(report.Tables[0].Title, "TEST LEVELS") {
		t.Fatalf("test-level table = %+v", report.Tables)
	}
	return report.Tables[0]
}

func assertTestPlanLevels(t *testing.T, table Table) {
	t.Helper()
	seenLevels := map[string]bool{
		"focused": false, "recommended": false, "full": false, "supplemental": false,
	}
	for _, row := range table.Rows {
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
			t.Fatalf("test-level table omitted %s: %v", level, table.Rows)
		}
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

func assertSupplementalTestPlanRow(t *testing.T, table Table) {
	t.Helper()
	supplementalRow := slices.IndexFunc(table.Rows, func(row []string) bool {
		return len(row) == 4 && row[0] == "supplemental"
	})
	if supplementalRow < 0 || table.Rows[supplementalRow][2] != "explicit only" {
		t.Fatalf("supplemental table row = %v", table.Rows)
	}
	if width := tableWidth(table); width > 80 {
		t.Fatalf("test-level table width = %d, want <= 80", width)
	}
}
