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
	initializeReportIgnoreGitRepository(t, repoRoot)
	writeFile(t, filepath.Join(repoRoot, ".gitignore"), []byte("/.code-polishy-reports/\n!/.code-polishy-reports/\n"), 0o600)
	if _, err := Install(repoRoot, policyRoot); err != nil {
		t.Fatal(err)
	}
	assertReportsIgnoredByGit(t, repoRoot)
	commands := [][]string{
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
	ignorePath := filepath.Join(repoRoot, ".gitignore")
	contents, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, ignorePath, append(contents, []byte("!/.code-polishy-reports/\n")...), 0o600)
	if status := Check(repoRoot, policyRoot); status.Current {
		t.Fatalf("later negation was accepted: %+v", status)
	}
	if _, err := Sync(repoRoot, policyRoot); err != nil {
		t.Fatal(err)
	}
	assertReportsIgnoredByGit(t, repoRoot)
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
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if strings.Contains(line, ".code-polishy-reports/") {
				t.Fatalf("generated reports changed git status: %s", output)
			}
		}
	}
}

func assertReportsIgnoredByGit(t *testing.T, repoRoot string) {
	t.Helper()
	command := exec.Command("git", "-C", repoRoot, "check-ignore", "--no-index", "--quiet", "--", ".code-polishy-reports/")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("report artifacts are visible to Git: %v\n%s", err, output)
	}
}

func initializeReportIgnoreGitRepository(t *testing.T, repoRoot string) {
	t.Helper()
	command := exec.Command("git", "init", "--quiet", repoRoot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("initialize Git repository: %v\n%s", err, output)
	}
}
