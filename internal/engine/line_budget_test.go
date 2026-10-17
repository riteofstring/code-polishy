package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestCheckEnforcesLineBudgetAndReportsChangesFromBase(t *testing.T) {
	root := contentRepository(t, nil)
	setLineBudgetFixture(t, root, `{"production":1}`)
	writeEngineFile(t, root, "content/run.sh", "#!/bin/sh\n\necho ready\n", 0o755)
	initializeEngineGitRepository(t, root)
	setLineBudgetFixture(t, root, `{"production":1,"test":5}`)
	commitEngineCandidate(t, root, "add test budget")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	policyEngine.Runner = &recordingEngineRunner{}
	selection, err := policyEngine.SelectBase("main")
	if err != nil {
		t.Fatal(err)
	}

	report := policyEngine.Check(t.Context(), selection, "check")

	budget := lineBudgetFindings(report, "quality.lineBudget")
	if len(budget) != 1 || budget[0].Subject != "production" || budget[0].Severity == policy.FindingWarning ||
		!strings.Contains(budget[0].Message, "2 non-blank lines; budget is 1") {
		t.Fatalf("line budget findings = %+v", budget)
	}
	changes := lineBudgetFindings(report, "quality.lineBudgetChange")
	if len(changes) != 1 || changes[0].Subject != "test" || changes[0].Severity != policy.FindingWarning ||
		!strings.Contains(changes[0].Message, "from unset at base to 5") {
		t.Fatalf("line budget change findings = %+v", changes)
	}
}

func TestLineBudgetChangesReportRaisedAndRemovedScopes(t *testing.T) {
	t.Parallel()
	findings := lineBudgetChanges(&policy.LineBudget{Production: 100, Total: 300}, &policy.LineBudget{Production: 120})
	subjects := []string{}
	for _, finding := range findings {
		subjects = append(subjects, finding.Subject+": "+finding.Message)
	}
	want := []string{
		"production: production line budget changed from 100 at base to 120; change it only at the caller's explicit request",
		"total: total line budget changed from 300 at base to unset; change it only at the caller's explicit request",
	}
	if !slices.Equal(subjects, want) {
		t.Fatalf("changes = %q, want %q", subjects, want)
	}
	if unchanged := lineBudgetChanges(&policy.LineBudget{Test: 5}, &policy.LineBudget{Test: 5}); len(unchanged) != 0 {
		t.Fatalf("unchanged budget reported %+v", unchanged)
	}
}

var qualityFixturePattern = regexp.MustCompile(`"quality": \{.*\},`)

func setLineBudgetFixture(t *testing.T, root, budget string) {
	t.Helper()
	path := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	configured := qualityFixturePattern.ReplaceAllString(string(data), `"quality": {"lineBudget":`+budget+`},`)
	if configured == string(data) {
		t.Fatal("quality configuration fixture was not updated")
	}
	writeEngineFile(t, root, policy.ConfigFilename, configured, 0o600)
}

func lineBudgetFindings(report Report, check string) []policy.Finding {
	findings := []policy.Finding{}
	for _, finding := range report.Findings {
		if finding.Check == check {
			findings = append(findings, finding)
		}
	}
	return findings
}
