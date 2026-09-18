package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPreservesAConflictingWrapperAndEveryOtherTarget(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	path := filepath.Join(repoRoot, posixWrapperTargetFilename)
	contents := []byte("#!/usr/bin/env bash\nprintf 'project wrapper\\n'\n")
	writeFile(t, path, contents, 0o700)

	_, err := Install(repoRoot, policyRoot)
	if err == nil || !strings.Contains(err.Error(), "code-polishyw conflicts with the canonical wrapper") {
		t.Fatalf("conflicting wrapper was accepted: %v", err)
	}
	assertFile(t, path, contents, 0o700)
	assertMissing(t, filepath.Join(repoRoot, agentsTargetFilename))
	assertMissing(t, filepath.Join(repoRoot, claudeTargetFilename))
	assertMissing(t, filepath.Join(repoRoot, powerShellWrapperTargetFilename))
	assertMissing(t, filepath.Join(repoRoot, ignoreTargetFilename))
}

func TestSyncReplacesStaleManagedWrappersAndRestoresModes(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	if _, err := Install(repoRoot, policyRoot); err != nil {
		t.Fatal(err)
	}
	posixPath := filepath.Join(repoRoot, posixWrapperTargetFilename)
	powerShellPath := filepath.Join(repoRoot, powerShellWrapperTargetFilename)
	posixContents, err := os.ReadFile(posixPath)
	if err != nil {
		t.Fatal(err)
	}
	powerShellContents, err := os.ReadFile(powerShellPath)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, posixPath, append(posixContents, []byte("printf 'stale\\n'\n")...), 0o644)
	writeFile(t, powerShellPath, append(powerShellContents, []byte("Write-Output 'stale'\n")...), 0o755)

	status := Check(repoRoot, policyRoot)
	if status.Current || !strings.Contains(status.Message, "code-polishyw is stale") || !strings.Contains(status.Message, "code-polishyw.ps1 is stale") {
		t.Fatalf("stale wrappers status = %+v", status)
	}
	if _, err := Sync(repoRoot, policyRoot); err != nil {
		t.Fatal(err)
	}
	assertCanonicalWrappers(t, repoRoot, policyRoot)
}

func TestCheckReportsMissingAndConflictingWrappers(t *testing.T) {
	t.Parallel()
	policyRoot := policyFixture(t, canonicalAgentsText)
	repoRoot := t.TempDir()
	writeFile(t, filepath.Join(repoRoot, agentsTargetFilename), []byte(canonicalAgentsText), 0o600)
	writeFile(t, filepath.Join(repoRoot, claudeTargetFilename), []byte(expectedClaudeImport), 0o600)
	writeFile(t, filepath.Join(repoRoot, ignoreTargetFilename), []byte(expectedArtifactIgnores), 0o600)
	writeFile(t, filepath.Join(repoRoot, posixWrapperTargetFilename), []byte("project-owned\n"), 0o600)

	status := Check(repoRoot, policyRoot)
	if status.Current || !strings.Contains(status.Message, "code-polishyw conflicts") || !strings.Contains(status.Message, "code-polishyw.ps1 is missing") {
		t.Fatalf("wrapper status = %+v", status)
	}
	if len(status.Issues) != 2 {
		t.Fatalf("wrapper issues = %+v", status.Issues)
	}
	for _, issue := range status.Issues {
		if issue.Check != "policy.bootstrapWrapper" || issue.Subject != "managed-wrapper" {
			t.Fatalf("wrapper issue = %+v", issue)
		}
	}
}
