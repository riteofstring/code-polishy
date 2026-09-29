package engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestSummarizeFlakyTestsCountsFailuresThatPassOnTheSameCode(t *testing.T) {
	t.Parallel()
	history := repository.TrendsAnalysis{Weeks: []repository.TrendsWeek{{Week: "2026-09-07"}, {Week: "2026-09-14"}}}
	failed := gaterun.Attempt{Status: gaterun.Failed, FailureCategory: gaterun.CommandExit}
	environment := gaterun.Attempt{Status: gaterun.Failed, FailureCategory: gaterun.Environment}
	passed := gaterun.Attempt{Status: gaterun.Passed}
	outcomes := []gaterun.StoredTestOutcome{
		flakyOutcome(9, 8, "same-run", "unit", gaterun.Passed, failed, passed),
		flakyOutcome(9, 9, "rerun", "e2e", gaterun.Failed, failed),
		flakyOutcome(9, 15, "rerun", "e2e", gaterun.Passed, passed),
		flakyOutcome(9, 15, "environment", "lint", gaterun.Failed, environment),
		flakyOutcome(9, 16, "environment", "lint", gaterun.Passed, passed),
		flakyOutcome(9, 16, "changed-before", "api", gaterun.Failed, failed),
		flakyOutcome(9, 17, "changed-after", "api", gaterun.Passed, passed),
		flakyOutcome(8, 30, "outside-window", "unit", gaterun.Passed, failed, passed),
	}

	trends := summarizeFlakyTests(outcomes, history)

	if trends.SuiteRuns != 7 || trends.Weeks[0].SuiteRuns != 2 || trends.Weeks[1].SuiteRuns != 5 {
		t.Fatalf("suite runs = %+v", trends)
	}
	if trends.Weeks[0].Flaky != 2 || trends.Weeks[1].Flaky != 0 {
		t.Fatalf("flaky weeks = %+v", trends.Weeks)
	}
	want := []FlakyTestSuite{{Name: "e2e", Flaky: 1}, {Name: "unit", Flaky: 1}}
	if !slices.Equal(trends.Suites, want) {
		t.Fatalf("suites = %+v", trends.Suites)
	}
}

func TestTrendsBuildsHumanAndSchemaValidatedMachineEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runTrendsEngineGit(t, root, time.Time{}, "init", "-q", "-b", "main")
	runTrendsEngineGit(t, root, time.Time{}, "config", "user.email", "tests@example.test")
	runTrendsEngineGit(t, root, time.Time{}, "config", "user.name", "Code Polishy Tests")
	writeEngineFile(t, root, "src/app.go", "package app\n\nfunc Value() int {\n\treturn 1\n}\n", 0o600)
	writeEngineFile(t, root, "src/app_test.go", "package app\n", 0o600)
	runTrendsEngineGit(t, root, time.Time{}, "add", "-A")
	runTrendsEngineGit(t, root, time.Now().UTC().Add(-time.Hour), "commit", "-q", "-m", "add application")
	policyEngine := &Engine{Repository: repository.Repository{
		Root: root,
		Config: policy.Config{
			Modules:    []policy.Module{{Name: "application", Paths: []string{"src/**"}}},
			Exceptions: []policy.Exception{expiredEngineException()},
		},
	}}

	report, err := policyEngine.Trends(TrendsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.Summary.Status != "passed" {
		t.Fatalf("unrelated policy status changed the trends outcome: %+v", report.Findings)
	}
	if !slices.ContainsFunc(report.Notes, func(note string) bool { return strings.Contains(note, "before Code Polishy was adopted") }) {
		t.Fatalf("notes = %v", report.Notes)
	}
	if report.RepositoryTrends == nil || report.RepositoryTrends.History.Changes != 1 || report.RepositoryTrends.FlakyTests.Worktrees != 1 {
		t.Fatalf("repository trends = %+v", report.RepositoryTrends)
	}
	if len(report.Tables) == 0 || report.Tables[0].Title != "REPOSITORY TRENDS" || len(report.Tables[0].Rows) != repository.DefaultTrendsWeeks {
		t.Fatalf("tables = %+v", report.Tables)
	}
	finalized, err := policyEngine.FinalizeReport("trends", report)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(finalized.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	decoded := Report{}
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.RepositoryTrends == nil || decoded.RepositoryTrends.History.Changes != 1 {
		t.Fatalf("decoded report = %+v, error = %v", decoded.RepositoryTrends, err)
	}
}

func expiredEngineException() policy.Exception {
	return policy.Exception{
		ID: "expired-length", Check: "quality.fileLength", Path: "src/app.go", Subject: "1200",
		Reason: "Split planned for an earlier milestone.", Owner: "application",
		Expires: policy.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
}

func flakyOutcome(month, day int, candidate, suite string, status gaterun.CommandStatus, attempts ...gaterun.Attempt) gaterun.StoredTestOutcome {
	return gaterun.StoredTestOutcome{
		StartedAt: time.Date(2026, time.Month(month), day, 12, 0, 0, 0, time.UTC), Candidate: candidate,
		Command: gaterun.CommandOutcome{Name: suite, Category: gaterun.OrdinaryTest, Status: status, Attempts: attempts},
	}
}

func runTrendsEngineGit(t *testing.T, root string, at time.Time, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	command.Env = os.Environ()
	if !at.IsZero() {
		stamp := at.Format(time.RFC3339)
		command.Env = append(command.Env, "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
