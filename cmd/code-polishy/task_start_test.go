package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
)

func TestTaskStartCapturesExactIntentAndComposesAuthoritativeContext(t *testing.T) {
	root, policyRoot, intent := newTaskStartCLIRepository(t)
	arguments := taskStartCLIArguments(root, policyRoot, intent)
	arguments = append(arguments, "--files", "value.go", "--feature", "ＰＵＲＣＨＡＳＥ completion", "--situation", "release")
	status, stdout, stderr := captureRunOutput(t, arguments)
	var packet engine.TaskStartPacket
	if err := json.Unmarshal([]byte(stdout), &packet); err != nil || status != 0 || stderr != "" || packet.Protocol != "task-start/v1" {
		t.Fatalf("task start: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	if !slices.Equal(packet.Capture.Features, []string{"checkout"}) || packet.Capture.RequirementID == "" || !slices.Equal(packet.RequestedSelection.Expanded, []string{"value.go"}) {
		t.Fatalf("packet selection or explicit activation: %+v", packet)
	}
	policyEngine, err := engine.Open(root, policyRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	assertTaskStartComponentFacts(t, policyEngine, packet)
	journal, err := os.ReadFile(filepath.Join(root, packet.Capture.JournalPath))
	if err != nil || !strings.Contains(string(journal), packet.Capture.ID) || !strings.Contains(string(journal), "Original request with exact whitespace.  \\n") {
		t.Fatalf("exact capture missing from journal: %s error=%v", journal, err)
	}
	for _, path := range []string{"executed-check", "procedure-must-not-run", ".code-polishy-reports/behavior-review/packet.json"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("task-start executed or prepared review work: %s: %v", path, err)
		}
	}
	if len(packet.NextActions) == 0 || packet.NextActions[len(packet.NextActions)-1].Name != "final-gate" {
		t.Fatalf("required next actions are missing: %+v", packet.NextActions)
	}
}

func assertTaskStartComponentFacts(t *testing.T, policyEngine *engine.Engine, packet engine.TaskStartPacket) {
	t.Helper()
	if !reflect.DeepEqual(packet.Verification, policyEngine.Repository.Config.Verification) {
		t.Fatalf("packet omitted or changed configured verification: %+v", packet.Verification)
	}
	context, err := policyEngine.DesignContext(engine.ContextRequest{Mode: "files", Files: []string{"value.go"}, Situations: []string{"release"}, Workflow: "task-start"})
	if err != nil || !reflect.DeepEqual(packet.RepositoryContext, context.RepositoryContext) || len(packet.RepositoryContext.Handoffs) != 1 || len(packet.RepositoryContext.DesignDocuments) != 1 {
		t.Fatalf("context differs from its component: packet=%+v context=%+v error=%v", packet.RepositoryContext, context.RepositoryContext, err)
	}
	inventory, err := policyEngine.Capabilities("")
	if err != nil || inventory.LockedRelease == nil || !reflect.DeepEqual(packet.LockedRelease, *inventory.LockedRelease) || packet.CatalogSHA256 != inventory.ReleaseCatalog.SHA256 {
		t.Fatalf("catalog differs from discovery: %+v error=%v", inventory, err)
	}
	for _, guard := range packet.ConfiguredGuards {
		if !reflect.DeepEqual(guard, capabilityCLIEntry(t, inventory, guard.ID)) {
			t.Fatalf("guard differs from capability discovery: %+v", guard)
		}
	}
}

func TestTaskStartInvalidInputsCreateNoJournalOrArtifacts(t *testing.T) {
	for name, tail := range map[string][]string{
		"missing selection":        {},
		"two selectors":            {"--files", "value.go", "--module", "application"},
		"multiple files":           {"--files", "value.go", "README.md"},
		"missing source":           {"--files", "absent.go"},
		"unknown feature":          {"--files", "value.go", "--feature", "purchase"},
		"selected missing handoff": {"--files", "value.go", "--situation", "authentication"},
		"unknown format":           {"--files", "value.go", "--format", "human"},
	} {
		t.Run(name, func(t *testing.T) {
			root, policyRoot, intent := newTaskStartCLIRepository(t)
			status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), tail...))
			if status != 2 || stdout != "" || stderr == "" {
				t.Fatalf("invalid task start: status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			if _, err := os.Lstat(filepath.Join(root, ".code-polishy-reports")); !os.IsNotExist(err) {
				t.Fatalf("invalid preflight created report state: %v", err)
			}
		})
	}
}

func TestTaskStartModuleSelectionDoesNotInferFeaturesFromIntent(t *testing.T) {
	root, policyRoot, intent := newTaskStartCLIRepository(t)
	if err := os.WriteFile(intent, []byte("Explain checkout and purchase completion.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--module", "application"))
	var packet engine.TaskStartPacket
	if err := json.Unmarshal([]byte(stdout), &packet); err != nil || status != 0 || stderr != "" {
		t.Fatalf("module packet: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	if len(packet.Capture.Features) != 0 || packet.Capture.RequirementID != "" || packet.RequestedSelection.Mode != "module" {
		t.Fatalf("module selection inferred a behavior feature: %+v", packet)
	}
	before, err := os.ReadFile(filepath.Join(root, packet.Capture.JournalPath))
	if err != nil {
		t.Fatal(err)
	}
	status, _, _ = captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--files", "value.go", "--situation", "authentication"))
	after, err := os.ReadFile(filepath.Join(root, packet.Capture.JournalPath))
	if status != 2 || err != nil || !slices.Equal(before, after) {
		t.Fatalf("invalid later task start changed the journal: status=%d error=%v", status, err)
	}
}

func newTaskStartCLIRepository(t *testing.T) (string, string, string) {
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
	policyRoot := installedRelease(t, strings.Repeat("e", 40))
	manifest, _, err := release.ReadManifest(policyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := release.WriteLock(root, release.LockFor(manifest)); err != nil {
		t.Fatal(err)
	}
	gitBehaviorReviewCLI(t, root, "add", policy.ConfigFilename, release.LockFilename, "docs")
	gitBehaviorReviewCLI(t, root, "commit", "-m", "Prepare task context")
	if _, err := engine.Open(root, policyRoot, ""); err != nil {
		t.Fatalf("invalid task-start fixture: %v", err)
	}
	intent := filepath.Join(t.TempDir(), "intent.txt")
	if err := os.WriteFile(intent, []byte("Original request with exact whitespace.  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, policyRoot, intent
}

func taskStartCLIArguments(root, policyRoot, intent string) []string {
	return []string{"--repo-root", root, "--policy-root", policyRoot, "task-start", "--intent-file", intent}
}

func TestTaskStartPacketSizeFailureOccursBeforeIntentPublication(t *testing.T) {
	root, policyRoot, intent := newTaskStartCLIRepository(t)
	configPath := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	documentation := config["documentation"].(map[string]any)
	handoffs := documentation["handoffs"].([]any)
	for index := range 3 {
		path := fmt.Sprintf("docs/large-%d.md", index)
		handoffs = append(handoffs, map[string]any{
			"name": fmt.Sprintf("large-%d", index), "description": "Large valid context.",
			"path": path, "sourcePaths": []string{"value.go"},
		})
		writeBehaviorReviewCLIFile(t, root, path, "#\n"+strings.Repeat("<", (1<<20)-3)+"\n")
	}
	documentation["handoffs"] = handoffs
	data, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorReviewCLIFile(t, root, policy.ConfigFilename, string(data))
	gitBehaviorReviewCLI(t, root, "add", policy.ConfigFilename, "docs")
	gitBehaviorReviewCLI(t, root, "commit", "-m", "Declare large valid context")
	status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--files", "value.go"))
	if status != 2 || stdout != "" || !strings.Contains(stderr, "task-start packet exceeds") {
		t.Fatalf("packet bound: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(root, ".code-polishy-reports")); !os.IsNotExist(err) {
		t.Fatalf("oversized packet left an intent capture or artifact root: %v", err)
	}
}

func TestTaskStartUnavailableCatalogCannotPublishIntent(t *testing.T) {
	root, policyRoot, intent := newTaskStartCLIRepository(t)
	if err := os.Remove(filepath.Join(policyRoot, release.CapabilityCatalogPath)); err != nil {
		t.Fatal(err)
	}
	status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--files", "docs"))
	if status != 2 || stdout != "" || !strings.Contains(stderr, "authenticated locked capability catalog") {
		t.Fatalf("unavailable catalog: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(root, ".code-polishy-reports")); !os.IsNotExist(err) {
		t.Fatalf("unavailable catalog left an intent capture: %v", err)
	}
}
