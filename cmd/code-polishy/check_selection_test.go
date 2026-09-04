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

func TestCheckSelectedAgentGuidanceDoesNotStartPythonAnalysis(t *testing.T) {
	root, _ := newBehaviorReviewCLIBaseRepository(t)
	config, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(config), `"scope": {}`, `"scope": {
  "pythonDynamicReferences": [{"project":"pyproject.toml","module":"app","symbol":"handler"}],
  "pythonExternalAttributes": [{"project":"pyproject.toml","module":"app","callable":"configure","receiver":"settings","attribute":"output_path","line":4,"consumerType":"external.runtime.Settings"}]
}`, 1)
	writeBehaviorReviewCLIFile(t, root, policy.ConfigFilename, configured)
	writeBehaviorReviewCLIFile(t, root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\n")
	writeBehaviorReviewCLIFile(t, root, "app.py", "def handler():\n    return 1\n\ndef unused_function():\n    return 2\n")
	status, stdout, stderr := captureRunOutput(t, []string{
		"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t),
		"check", "--files", "AGENTS.md", "--format", "json",
	})
	report := engine.Report{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status > 1 {
		t.Fatalf("check failed: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	if report.RequestedSelection == nil || !slices.Equal(report.RequestedSelection.Expanded, []string{"AGENTS.md"}) || slices.ContainsFunc(report.AnalysisContext, func(context engine.AnalysisContext) bool {
		return context.Analyzer == "python-project"
	}) {
		t.Fatalf("selection = %+v, context = %+v", report.RequestedSelection, report.AnalysisContext)
	}
	for _, finding := range report.Findings {
		if strings.HasPrefix(finding.Check, "quality.deadCode") || strings.HasPrefix(finding.Check, "architecture.python") ||
			finding.Check == "policy.pythonDynamicReference" || finding.Check == "policy.pythonExternalAttribute" {
			t.Errorf("Python analysis ran for selected agent guidance: %+v", finding)
		}
	}
}
