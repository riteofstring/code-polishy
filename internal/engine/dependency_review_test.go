package engine

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/supplychain"
)

func TestDependencyReviewSeparatesRemovalsWithoutLosingChanges(t *testing.T) {
	t.Parallel()
	changes := []supplychain.DependencyChange{
		{Ecosystem: "pypi", Directness: "direct", Usage: "runtime", Change: "added", Package: "numpy", From: "-", To: "2.3.0", Scope: "pyproject.toml"},
		{Ecosystem: "pypi", Directness: "direct", Usage: "runtime", Change: "removed", Package: "pillow", From: "11.0.0", To: "-", Scope: "pyproject.toml"},
		{Ecosystem: "pypi", Directness: "transitive", Usage: "resolved", Change: "updated", Package: "torch", From: "2.7.0", To: "2.8.0", Scope: "uv.lock"},
	}
	tables := dependencyReviewTables(changes)
	if len(tables) != 2 || tables[0].Title != "DEPENDENCIES REMOVED" || len(tables[0].Rows) != 1 || len(tables[1].Rows) != 2 {
		t.Fatalf("dependency tables = %+v", tables)
	}
	if !slices.Equal(tables[0].Rows[0], []string{"pypi/direct", "runtime", "removed", "pillow", "11.0.0", "-", "pyproject.toml"}) ||
		!slices.Equal(tables[1].Rows[0], []string{"pypi/direct", "runtime", "added", "numpy", "-", "2.3.0", "pyproject.toml"}) ||
		!slices.Equal(tables[1].Rows[1], []string{"pypi/transitive", "resolved", "updated", "torch", "2.7.0", "2.8.0", "uv.lock"}) {
		t.Fatalf("dependency changes lost their identity or scope: %+v", tables)
	}
	tables = dependencyReviewTables(changes[1:2])
	if len(tables) != 1 || tables[0].Title != "DEPENDENCIES REMOVED" || len(tables[0].Rows) != 1 {
		t.Fatalf("removal-only review emitted unrelated rows: %+v", tables)
	}
}

func TestDependencyReviewRunsBuiltInCandidateAdmissionAfterHistoricalComparison(t *testing.T) {
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
	if len(commandRunner.commands) != 0 {
		t.Fatalf("candidate security execution = %v", commandRunner.commands)
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "installed dependency license evidence is deferred") {
		t.Fatalf("dependency review omitted the pre-install evidence boundary: %v", report.Notes)
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
	if len(commandRunner.commands) != 0 {
		t.Fatalf("repair candidate security execution = %v", commandRunner.commands)
	}
}

func TestDependencyReviewDefersRepositoryProvidersAndArtifactBuilds(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "README.md", "project\n", 0o600)
	initializeEngineGitRepository(t, root)
	commandRunner := &recordingEngineRunner{}
	policyEngine := &Engine{
		Repository: repository.Repository{Root: root, PolicyRoot: root, Config: policy.Config{
			Checks: []policy.Command{
				{Name: "frontend-lock-sync", Provides: []string{"lock-sync"}, Argv: []string{"npm", "ci", "--ignore-scripts", "--no-audit"}, RunOn: []string{"supply-chain"}},
				{Name: "python-lock", Provides: []string{"lock-sync"}, Argv: []string{"uv", "sync", "--frozen", "--group", "dev"}, RunOn: []string{"supply-chain"}},
				{Name: "custom-age", Provides: []string{"release-age"}, Argv: []string{"custom-wrapper"}, RunOn: []string{"supply-chain-online"}},
				{Name: "candidate-security", Provides: []string{"security"}, Argv: []string{"build-and-scan"}, RunOn: []string{"security"}},
			},
			SupplyChain: policy.SupplyChain{ArtifactSecurity: policy.ArtifactSecurity{Targets: []policy.ArtifactTarget{
				{Name: "application", Mode: "command", Producer: &policy.ArtifactProducer{Argv: []string{"build-artifact"}, Cwd: ".", Manifest: "manifest.json"}},
			}}},
		}},
		Runner: commandRunner, Output: io.Discard,
	}
	for _, changed := range []bool{false, true} {
		if changed {
			writeEngineFile(t, root, "README.md", "updated project\n", 0o600)
		}
		report, err := policyEngine.DependencyReview(t.Context(), "main")
		if err != nil {
			t.Fatal(err)
		}
		if len(commandRunner.commands) != 0 || dependencyReviewHasFinding(report, "artifactSecurity.operation") {
			t.Fatalf("pre-install review entered provider or artifact execution: commands=%v, report=%+v", commandRunner.commands, report)
		}
		notes := strings.Join(report.Notes, "\n")
		for _, name := range []string{"frontend-lock-sync", "python-lock", "custom-age", "candidate-security", "application"} {
			if !strings.Contains(notes, name) {
				t.Errorf("deferred check %q missing from report: %s", name, notes)
			}
		}
		if strings.Contains(notes, "completed online supply-chain profile") || !strings.Contains(notes, "without --offline") {
			t.Fatalf("review misrepresented deferred evidence or omitted recovery: %s", notes)
		}
	}
	report, err := policyEngine.SupplyChain(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(commandRunner.commands, []string{"frontend-lock-sync", "python-lock", "custom-age", "candidate-security"}) ||
		!dependencyReviewHasFinding(report, "artifactSecurity.operation") {
		t.Fatalf("full supply-chain profile lost deferred checks: commands=%v, report=%+v", commandRunner.commands, report)
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
