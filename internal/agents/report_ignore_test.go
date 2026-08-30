package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallKeepsGeneratedReportsOutOfGitStatus(t *testing.T) {
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	if _, err := Install(repoRoot, policyRoot); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "agents@example.test"},
		{"config", "user.name", "Agents Test"},
		{"add", "--all"},
		{"commit", "--quiet", "-m", "adopt policy"},
	}
	for _, arguments := range commands {
		command := exec.Command("git", arguments...)
		command.Dir = repoRoot
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	reportDirectory := filepath.Join(repoRoot, ".code-polishy-reports", "merge-gate")
	if err := os.MkdirAll(reportDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reportDirectory, "report.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "status", "--short", "--untracked-files=all")
	command.Dir = repoRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != "" {
		t.Fatalf("generated reports changed git status: %s", output)
	}
}
