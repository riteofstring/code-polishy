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
  "pythonDynamicReferences": [{"kind":"target","project":"pyproject.toml","target":{"module":"app","symbol":"handler"},"consumer":{"kind":"callsite","importer":"app.py","module":"app","callable":"load","site":{"line":3,"column":12},"callee":"pkgutil.resolve_name","shape":"module-object-call/v1","argument":"'app:handler'","sourceSha256":"`+strings.Repeat("a", 64)+`"}}],
  "pythonExternalAttributes": [{"project":"pyproject.toml","module":"app","callable":"configure","receiver":{"kind":"parameter","name":"settings","binding":{"line":3,"column":15},"type":"external.runtime.Settings"},"attribute":"output_path","write":{"line":4,"column":5}}],
  "pythonExternalPluginImports": [{"project":"pyproject.toml","distribution":"plug-dist","namespace":"third_party.plugins","inputGrammar":"python-module-object/v1","consumer":{"kind":"callsite","importer":"app.py","module":"app","callable":"load","site":{"line":5,"column":14},"callee":"pkgutil.resolve_name","shape":"module-object-call/v1","argument":"name","sourceSha256":"`+strings.Repeat("a", 64)+`"},"check":{"kind":"isinstance","protocol":"app.Contract","site":{"line":6,"column":12}}}]
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
			finding.Check == "policy.pythonDynamicReference" || finding.Check == "policy.pythonExternalAttribute" || finding.Check == "policy.pythonExternalPluginImport" {
			t.Errorf("Python analysis ran for selected agent guidance: %+v", finding)
		}
	}
	if report.Execution == nil || report.Execution.EvaluationDurationMilliseconds < 0 || !slices.ContainsFunc(report.Execution.Phases, func(phase engine.ExecutionPhase) bool {
		return phase.Name == "architecture"
	}) || len(report.Execution.Commands) != 0 {
		t.Fatalf("execution telemetry = %+v", report.Execution)
	}
}
