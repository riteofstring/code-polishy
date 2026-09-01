package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPackHelpAndRootWorkBeforeRepositoryInitialization(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", missing, "pack", "--help"})
	if status != 0 || stderr != "" || !strings.Contains(stdout, "code-polishy pack install --source PATH") {
		t.Fatalf("pack help failed: %d %q %q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, []string{"--repo-root", missing, "pack", "root"})
	if status != 0 || stderr != "" || strings.TrimSpace(stdout) == "" {
		t.Fatalf("pack root failed: %d %q %q", status, stdout, stderr)
	}
}

func TestPackRejectsIncompleteAndUnknownActionsBeforeReadingSource(t *testing.T) {
	for _, arguments := range [][]string{{"pack"}, {"pack", "install"}, {"pack", "verify"}, {"pack", "root", "extra"}, {"pack", "unknown"}} {
		status, stdout, stderr := captureRunOutput(t, arguments)
		if status != 2 || stdout != "" || !strings.Contains(stderr, "usage error:") {
			t.Fatalf("invalid pack action was not a usage failure: %v => %d %q %q", arguments, status, stdout, stderr)
		}
	}
}
