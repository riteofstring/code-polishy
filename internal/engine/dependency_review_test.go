package engine

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDependencyReviewRunsWholeCandidateAdmissionAfterHistoricalComparison(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reference := "v1.2.3"
	manifest := "[project]\ndependencies = [\"tool @ git+https://example.test/team/tool.git@" + reference + "\"]\n"
	writeEngineFile(t, root, "pyproject.toml", manifest, 0o600)
	initializeEngineGitRepository(t, root)
	commandRunner := &recordingEngineRunner{}
	policyEngine := &Engine{
		Repository: repository.Repository{Root: root, Config: policy.Config{
			Checks: []policy.Command{{Name: "candidate-security", Argv: []string{"repository-security"}, RunOn: []string{"security"}}},
		}},
		Runner: commandRunner, Output: io.Discard,
	}
	report, err := policyEngine.DependencyReview(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tables) != 1 || len(report.Tables[0].Rows) != 0 || !dependencyReviewHasFinding(report, "supplyChain.pythonManifest") {
		t.Fatalf("unchanged candidate bypassed admission: %+v", report)
	}
	if !slices.Equal(commandRunner.commands, []string{"candidate-security"}) {
		t.Fatalf("candidate security execution = %v", commandRunner.commands)
	}
	commit := strings.Repeat("a", 40)
	writeEngineFile(t, root, "pyproject.toml", strings.ReplaceAll(manifest, reference, commit), 0o600)
	commandRunner.commands = nil
	report, err = policyEngine.DependencyReview(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tables) != 1 || len(report.Tables[0].Rows) != 1 || report.Tables[0].Rows[0][2] != "updated" {
		t.Fatalf("historical tag repair did not reach review: %+v", report)
	}
	if dependencyReviewHasFinding(report, "supplyChain.pythonManifest") || !dependencyReviewHasFinding(report, "supplyChain.lockfile") {
		t.Fatalf("candidate admission lost the missing-lock failure: %+v", report.Findings)
	}
	if !slices.Equal(commandRunner.commands, []string{"candidate-security"}) {
		t.Fatalf("repair candidate security execution = %v", commandRunner.commands)
	}
}

func dependencyReviewHasFinding(report Report, rule string) bool {
	for _, finding := range report.Findings {
		if finding.Check == rule {
			return true
		}
	}
	return false
}
