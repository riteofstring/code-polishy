package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func TestMachineReportOutputIsOneDocumentWithCompleteManagedCustody(t *testing.T) {
	root, _ := newBehaviorReviewCLIRepository(t)
	policyRoot := behaviorReviewCLIPolicyRoot(t)
	status, stdout, _ := captureRunOutput(t, []string{
		"--repo-root", root, "--policy-root", policyRoot,
		"architecture", "--files", "value.go", "--format", "json", "--filter-rule", "quality.lint",
	})
	if status != 0 && status != 1 {
		t.Fatalf("status=%d stdout=%q", status, stdout)
	}
	view := engine.Report{}
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("stdout is not one JSON document: %v: %q", err, stdout)
	}
	if view.Protocol != engine.ReportProtocol || view.Command != "architecture" || view.ReportPath == "" || view.Display == nil || view.RequestedSelection == nil {
		t.Fatalf("view = %+v", view)
	}
	managedData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(view.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	managed := engine.Report{}
	if err := json.Unmarshal(managedData, &managed); err != nil {
		t.Fatal(err)
	}
	if managed.Display == nil || managed.Display.Total != view.Display.Total || len(managed.Findings) < len(view.Findings) {
		t.Fatalf("managed = %+v view = %+v", managed, view)
	}
}

func TestSARIFOutputFileLeavesStdoutMachineClean(t *testing.T) {
	root, _ := newBehaviorReviewCLIRepository(t)
	output := "analysis.sarif"
	status, stdout, stderr := captureRunOutput(t, []string{
		"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t),
		"architecture", "--files", "value.go", "--format", "sarif", "--output", output,
	})
	if status != 0 && status != 1 {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if stdout != "" || !strings.Contains(stderr, "REPORT OUTPUT: "+output) {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
	data, err := os.ReadFile(filepath.Join(root, output))
	if err != nil {
		t.Fatal(err)
	}
	var sarif struct {
		Version string `json:"version"`
		Runs    []any  `json:"runs"`
	}
	if err := json.Unmarshal(data, &sarif); err != nil || sarif.Version != "2.1.0" || len(sarif.Runs) != 1 {
		t.Fatalf("sarif=%+v error=%v", sarif, err)
	}
}
