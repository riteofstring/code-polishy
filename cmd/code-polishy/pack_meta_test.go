package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackHelpAndRootWorkBeforeRepositoryInitialization(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", missing, "pack", "--help"})
	if status != 0 || stderr != "" || !strings.Contains(stdout, "code-polishy pack install --source PATH") || !strings.Contains(stdout, "code-polishy pack install --official NAME@VERSION") || !strings.Contains(stdout, "code-polishy pack list") || !strings.Contains(stdout, "code-polishy pack conformance --ledger PATH --reference PATH --candidate PATH") {
		t.Fatalf("pack help failed: %d %q %q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, []string{"--repo-root", missing, "pack", "root"})
	if status != 0 || stderr != "" || strings.TrimSpace(stdout) == "" {
		t.Fatalf("pack root failed: %d %q %q", status, stdout, stderr)
	}
}

func TestPackRejectsIncompleteAndUnknownActionsBeforeReadingSource(t *testing.T) {
	for _, arguments := range [][]string{{"pack"}, {"pack", "catalog"}, {"pack", "install"}, {"pack", "update"}, {"pack", "remove"}, {"pack", "verify"}, {"pack", "list", "extra"}, {"pack", "conformance"}, {"pack", "conformance", "--ledger", "ledger.json", "--reference", "reference"}, {"pack", "root", "extra"}, {"pack", "unknown"}} {
		status, stdout, stderr := captureRunOutput(t, arguments)
		if status != 2 || stdout != "" || !strings.Contains(stderr, "usage error:") {
			t.Fatalf("invalid pack action was not a usage failure: %v => %d %q %q", arguments, status, stdout, stderr)
		}
	}
}

func TestPackListReportsEmptyLocalStateWithoutARepository(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	missing := filepath.Join(t.TempDir(), "missing")
	status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", missing, "pack", "list", "--format", "json"})
	document := packListDocument{}
	if err := json.Unmarshal([]byte(stdout), &document); status != 0 || err != nil || stderr != "" || document.Protocol != "code-polishy-pack-status/v1" || len(document.Packs) != 0 {
		t.Fatalf("pack list = %d %q %q %+v: %v", status, stdout, stderr, document, err)
	}
}

func TestPackLifecycleOptionParsingIsExact(t *testing.T) {
	t.Parallel()
	if _, _, err := parsePackOptions("pack update", []string{"shell", "--to=1.0.0", "--to", "1.0.1"}, "--to"); err == nil || !strings.Contains(err.Error(), "only once") {
		t.Fatalf("duplicate option passed: %v", err)
	}
	if _, _, err := parsePackOptions("pack update", []string{"shell", "--latest"}, "--to"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown option passed: %v", err)
	}
}
