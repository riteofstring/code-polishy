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
	if status != 0 || stderr != "" || !strings.Contains(stdout, "code-polishy pack install --source PATH") || !strings.Contains(stdout, "code-polishy pack install --official NAME@VERSION") || !strings.Contains(stdout, "code-polishy pack validate --kind manifest|request|response") || !strings.Contains(stdout, "code-polishy pack list") || !strings.Contains(stdout, "code-polishy pack migration plan") || !strings.Contains(stdout, "code-polishy pack conformance --ledger PATH --reference PATH --candidate PATH") {
		t.Fatalf("pack help failed: %d %q %q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, []string{"--repo-root", missing, "pack", "root"})
	if status != 0 || stderr != "" || strings.TrimSpace(stdout) == "" {
		t.Fatalf("pack root failed: %d %q %q", status, stdout, stderr)
	}
}

func TestPackRejectsIncompleteAndUnknownActionsBeforeReadingSource(t *testing.T) {
	for _, arguments := range [][]string{{"pack"}, {"pack", "catalog"}, {"pack", "install"}, {"pack", "update"}, {"pack", "remove"}, {"pack", "verify"}, {"pack", "validate"}, {"pack", "validate", "--kind", "response", "--input", "response.json"}, {"pack", "list", "extra"}, {"pack", "migration"}, {"pack", "migration", "plan"}, {"pack", "migration", "apply"}, {"pack", "migration", "rollback"}, {"pack", "conformance"}, {"pack", "conformance", "--ledger", "ledger.json", "--reference", "reference"}, {"pack", "root", "extra"}, {"pack", "unknown"}} {
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

func TestPackValidateEmitsStableMachineReadableResults(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "tools", "fixtures", "language-pack", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	request := filepath.Join(root, "request-v4.json")
	status, stdout, stderr := captureRunOutput(t, []string{"pack", "validate", "--kind", "request", "--input", request})
	report := struct {
		Protocol string `json:"protocol"`
		Status   string `json:"status"`
		Errors   []struct {
			Path string `json:"path"`
		} `json:"errors"`
	}{}
	if decodeErr := json.Unmarshal([]byte(stdout), &report); status != 0 || decodeErr != nil || stderr != "" || report.Protocol != "code-polishy-pack-validation/v1" || report.Status != "passed" {
		t.Fatalf("valid contract = %d %q %q %+v: %v", status, stdout, stderr, report, decodeErr)
	}
	invalid := filepath.Join(root, "invalid", "response-comment-kind-v4.json")
	status, stdout, stderr = captureRunOutput(t, []string{"pack", "validate", "--kind", "response", "--input", invalid, "--request", request})
	if decodeErr := json.Unmarshal([]byte(stdout), &report); status != 1 || decodeErr != nil || stderr != "" || report.Status != "failed" || len(report.Errors) == 0 || report.Errors[0].Path != "facts.comments[0].kind" {
		t.Fatalf("invalid contract = %d %q %q %+v: %v", status, stdout, stderr, report, decodeErr)
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

func TestPackMigrationOptionParsingRequiresAnExplicitCompleteSet(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	options, err := parsePackMigrationPlanOptions([]string{"--catalog", "catalog.json", "--sha256", digest, "--select", "shell@1.0.0", "--select=python@1.0.0"})
	if err != nil || options.catalog != "catalog.json" || options.sha256 != digest || len(options.selections) != 2 {
		t.Fatalf("options = %+v: %v", options, err)
	}
	for _, arguments := range [][]string{
		{"--catalog", "catalog.json", "--sha256", digest},
		{"--catalog", "catalog.json", "--sha256", digest, "--select", "shell@1.0.0", "extra"},
		{"--catalog", "catalog.json", "--catalog", "other.json", "--sha256", digest, "--select", "shell@1.0.0"},
	} {
		if _, err := parsePackMigrationPlanOptions(arguments); err == nil {
			t.Fatalf("invalid migration options passed: %v", arguments)
		}
	}
}
