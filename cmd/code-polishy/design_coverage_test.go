package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestDesignContextCLIReportsMissingGuidanceWithoutBlocking(t *testing.T) {
	root := newHandoffCLIRepository(t)
	arguments := []string{"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t), "design-context", "--files", "value.go"}
	status, stdout, stderr := captureRunOutput(t, arguments)
	if status != 0 || !strings.Contains(stdout, "NO DESIGN MAPPING: value.go") || strings.Contains(stdout, "Review existing rationale") {
		t.Fatalf("coverage: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, append(arguments, "--format", "json"))
	var report engine.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 || report.RepositoryContext == nil {
		t.Fatalf("JSON coverage: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	if !slices.Equal(report.RepositoryContext.DesignResolution.UnmappedPaths, []string{"value.go"}) || len(report.Findings) != 0 {
		t.Fatalf("missing mapping became a failure or disappeared: %+v", report)
	}
}

func TestDesignContextCLIExplainsModuleMatches(t *testing.T) {
	root, policyRoot := newDesignContextCLIRepository(t)
	status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", root, "--policy-root", policyRoot, "design-context", "--files", "value.go"})
	if status != 0 || !strings.Contains(stdout, "DESIGN DOCUMENT: docs/design/current.md\n  SELECTED BY: module application") || strings.Contains(stdout, "NO DESIGN MAPPING: value.go") {
		t.Fatalf("matched context: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func newDesignContextCLIRepository(t *testing.T) (string, string) {
	t.Helper()
	root := newCapabilityCLIRepository(t)
	configPath := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	config["documentation"] = map[string]any{
		"design": []any{map[string]any{"path": "docs/design/current.md", "module": "application"}},
		"handoffs": []any{
			map[string]any{"name": "release", "description": "Release procedure.", "path": "docs/release.md", "sourcePaths": []string{"value.go"}, "situations": []string{"release"}},
			map[string]any{"name": "authentication", "description": "Authentication procedure.", "path": "docs/absent.md", "situations": []string{"authentication"}},
		},
	}
	data, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorReviewCLIFile(t, root, policy.ConfigFilename, string(data))
	writeBehaviorReviewCLIFile(t, root, "docs/design/current.md", "# Current design\n\nPreserve the application boundary.\n")
	writeBehaviorReviewCLIFile(t, root, "docs/release.md", "# Release\n\n    touch procedure-must-not-run\n")
	gitBehaviorReviewCLI(t, root, "add", policy.ConfigFilename, "docs")
	gitBehaviorReviewCLI(t, root, "commit", "-m", "Prepare task context")
	return root, behaviorReviewCLIPolicyRoot(t)
}
