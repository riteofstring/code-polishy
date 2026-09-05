package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	if status != 0 || !strings.Contains(stdout, "NO DESIGN MAPPING: value.go") || !strings.Contains(stdout, "Missing mappings alone do not block routine work") {
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
	root, policyRoot, _ := newTaskStartCLIRepository(t)
	status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", root, "--policy-root", policyRoot, "design-context", "--files", "value.go"})
	if status != 0 || !strings.Contains(stdout, "DESIGN DOCUMENT: docs/design/current.md\n  SELECTED BY: module application") || strings.Contains(stdout, "NO DESIGN MAPPING: value.go") {
		t.Fatalf("matched context: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestTaskStartCarriesActionableDesignGapsAndMatchesStandaloneContext(t *testing.T) {
	root, policyRoot, intent := newTaskStartCLIRepository(t)
	path := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	delete(config["documentation"].(map[string]any), "design")
	data, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorReviewCLIFile(t, root, policy.ConfigFilename, string(data))
	gitBehaviorReviewCLI(t, root, "add", policy.ConfigFilename)
	gitBehaviorReviewCLI(t, root, "commit", "-m", "Adopt without mapped rationale")
	status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--files", "value.go"))
	var packet engine.TaskStartPacket
	if err := json.Unmarshal([]byte(stdout), &packet); err != nil || status != 0 || packet.RepositoryContext == nil {
		t.Fatalf("task start: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	policyEngine, err := engine.Open(root, policyRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	standalone, err := policyEngine.DesignContext(engine.ContextRequest{Mode: "files", Files: []string{"value.go"}, Workflow: "task-start"})
	if err != nil || !reflect.DeepEqual(packet.RepositoryContext, standalone.RepositoryContext) || !slices.Equal(packet.RepositoryContext.DesignResolution.UnmappedPaths, []string{"value.go"}) {
		t.Fatalf("task and standalone context differ: %+v, %+v, %v", packet.RepositoryContext, standalone.RepositoryContext, err)
	}
	for _, action := range packet.NextActions {
		if action.Name == "review-design-coverage" {
			if !strings.Contains(action.Description, "adoption or consequential design changes") || len(action.Argv) != 0 {
				t.Fatalf("coverage action executes work or mandates boilerplate: %+v", action)
			}
			return
		}
	}
	t.Fatal("task-start omitted the design coverage action")
}
