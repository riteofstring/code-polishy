package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type docsCatalogFixture struct {
	Version int                `json:"version"`
	Topics  []docsTopicFixture `json:"topics"`
}

type docsTopicFixture struct {
	ID      string   `json:"id"`
	Path    string   `json:"path"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Aliases []string `json:"aliases"`
	Public  bool     `json:"public"`
}

func TestDocsEngineListsFindsAndReadsWithoutRepositoryInitialization(t *testing.T) {
	policyRoot := docsPolicyFixture(t)
	missingRepository := filepath.Join(t.TempDir(), "missing-repository")
	status, stdout, stderr := captureRunOutput(t, []string{
		"--repo-root", missingRepository, "--policy-root", policyRoot, "docs", "list",
	})
	if status != 0 || stderr != "" || stdout != "agent-workflows\tAgent Workflows\nbehavior-review\tBehavior Review\n" {
		t.Fatalf("list: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, []string{
		"--repo-root", missingRepository, "--policy-root", policyRoot, "docs", "find", "red", "proof",
	})
	if status != 0 || stderr != "" || !strings.HasPrefix(stdout, "behavior-review\tBehavior Review\t") || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("find: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, []string{
		"--repo-root", missingRepository, "--policy-root", policyRoot, "docs", "read", "agents",
	})
	if status != 0 || stderr != "" || stdout != "# Agent Workflows\n\nRun the same policy.\n" {
		t.Fatalf("read: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(missingRepository); !os.IsNotExist(err) {
		t.Fatalf("docs command mutated repository path: %v", err)
	}
}

func TestDocsCLIReportsNoResultsAsSuccess(t *testing.T) {
	status, stdout, stderr := captureRunOutput(t, []string{
		"--policy-root", docsPolicyFixture(t), "docs", "find", "no-such-term",
	})
	if status != 0 || stderr != "" || stdout != "No documentation topics matched \"no-such-term\".\n" {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestDocsCLIRejectsInvalidUsageBeforeReadingDocumentation(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	tests := [][]string{
		{"--policy-root", missing, "docs"},
		{"--policy-root", missing, "docs", "unknown"},
		{"--policy-root", missing, "docs", "list", "extra"},
		{"--policy-root", missing, "docs", "find"},
		{"--policy-root", missing, "docs", "find", "--unknown"},
		{"--policy-root", missing, "docs", "read"},
		{"--policy-root", missing, "docs", "read", "one", "two"},
		{"--policy-root", missing, "docs", "read", "--unknown"},
	}
	for _, arguments := range tests {
		status, stdout, stderr := captureRunOutput(t, arguments)
		if status != 2 || stdout != "" || !strings.Contains(stderr, "usage error:") || !strings.Contains(stderr, "code-polishy docs list") {
			t.Fatalf("%v: status=%d stdout=%q stderr=%q", arguments, status, stdout, stderr)
		}
	}
}

func TestDocsCLIReportsUnknownAndUnavailableTopicsPrecisely(t *testing.T) {
	policyRoot := docsPolicyFixture(t)
	status, stdout, stderr := captureRunOutput(t, []string{
		"--policy-root", policyRoot, "docs", "read", "behaviour-review",
	})
	if status != 2 || stdout != "" || !strings.Contains(stderr, `did you mean "behavior-review"?`) {
		t.Fatalf("unknown: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, []string{
		"--policy-root", filepath.Join(t.TempDir(), "missing"), "docs", "list",
	})
	if status != 2 || stdout != "" || !strings.Contains(stderr, "operational error: documentation unavailable") {
		t.Fatalf("unavailable: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestEveryDocsHelpFormSkipsRepositoryAndDocumentationInitialization(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	arguments := [][]string{
		{"--repo-root", missing, "--policy-root", missing, "docs", "--help"},
		{"--repo-root", missing, "--policy-root", missing, "docs", "list", "--help"},
		{"--repo-root", missing, "--policy-root", missing, "docs", "find", "--help"},
		{"--repo-root", missing, "--policy-root", missing, "docs", "read", "--help"},
		{"--repo-root", missing, "--policy-root", missing, "help", "docs"},
	}
	var expected string
	for _, values := range arguments {
		status, stdout, stderr := captureRunOutput(t, values)
		if status != 0 || stderr != "" || !strings.Contains(stdout, "code-polishy docs read TOPIC") {
			t.Fatalf("%v: status=%d stdout=%q stderr=%q", values, status, stdout, stderr)
		}
		if expected == "" {
			expected = stdout
		} else if stdout != expected {
			t.Fatalf("help forms diverged: %q and %q", expected, stdout)
		}
	}
}

func TestCheckedInDocsCLIReadsTheSourceCatalog(t *testing.T) {
	root := docsSourceRoot(t)
	status, stdout, stderr := captureRunOutput(t, []string{"--policy-root", root, "docs", "list"})
	if status != 0 || stderr != "" || !strings.Contains(stdout, "agent-workflows\tAgent Workflows\n") ||
		!strings.Contains(stdout, "verification\tVerification and Testing Policy\n") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, []string{"--policy-root", root, "docs", "read", "agent-workflows"})
	if status != 0 || stderr != "" || !strings.HasPrefix(stdout, "# Agent Workflows\n") || !strings.HasSuffix(stdout, "\n") {
		t.Fatalf("status=%d stdout-prefix=%q stderr=%q", status, firstBytes(stdout, 80), stderr)
	}
}

func docsPolicyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	topics := []docsTopicFixture{
		{
			ID: "behavior-review", Path: "docs/behavior.md", Title: "Behavior Review",
			Summary: "Review behavior changes.", Aliases: []string{"regressions"}, Public: true,
		},
		{
			ID: "agent-workflows", Path: "docs/agents.md", Title: "Agent Workflows",
			Summary: "Run agent workflows.", Aliases: []string{"agents"}, Public: true,
		},
	}
	data, err := json.Marshal(docsCatalogFixture{Version: 1, Topics: topics})
	if err != nil {
		t.Fatal(err)
	}
	writeDocsCLIFile(t, root, "docs/catalog.json", append(data, '\n'))
	writeDocsCLIFile(t, root, "docs/behavior.md", []byte("# Behavior Review\n\nA red proof protects behavior.\n"))
	writeDocsCLIFile(t, root, "docs/agents.md", []byte("# Agent Workflows\n\nRun the same policy."))
	return root
}

func writeDocsCLIFile(t *testing.T, root, relative string, data []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func docsSourceRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve docs CLI source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
}

func firstBytes(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}
