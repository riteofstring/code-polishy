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
	if err := json.Unmarshal([]byte(stdout), &packet); err != nil || status != 0 || stderr != "" || packet.Protocol != "task-start/v2" {
		t.Fatalf("task start: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	assertSelectedTaskStartPacket(t, packet)
	policyEngine, err := engine.Open(root, policyRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	assertTaskStartComponentFacts(t, policyEngine, packet)
	journal, err := os.ReadFile(filepath.Join(root, packet.Intent.Capture.JournalPath))
	if err != nil || !strings.Contains(string(journal), packet.Intent.Capture.ID) || !strings.Contains(string(journal), "Original request with exact whitespace.  \\n") {
		t.Fatalf("exact capture missing from journal: %s error=%v", journal, err)
	}
	assertTaskStartDidNotExecute(t, root)
	if len(packet.NextActions) == 0 || packet.NextActions[len(packet.NextActions)-1].Name != "final-gate" {
		t.Fatalf("required next actions are missing: %+v", packet.NextActions)
	}
}

func assertSelectedTaskStartPacket(t *testing.T, packet engine.TaskStartPacket) {
	t.Helper()
	if !packet.Intent.Captured || !packet.Intent.WillBeUsed || packet.Intent.Capture == nil || packet.Intent.Capture.RequirementID == "" ||
		!slices.Equal(packet.Intent.Capture.Features, []string{"checkout"}) || !slices.Equal(packet.Intent.SelectedFeatures, []string{"checkout"}) ||
		packet.TaskBase != packet.Intent.Capture.Commit || !slices.Equal(packet.RequestedSelection.Expanded, []string{"value.go"}) {
		t.Fatalf("packet selection or explicit activation: %+v", packet)
	}
}

func assertTaskStartDidNotExecute(t *testing.T, root string) {
	t.Helper()
	for _, path := range []string{"executed-check", "procedure-must-not-run", ".code-polishy-reports/behavior-review/packet.json"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("task-start executed or prepared review work: %s: %v", path, err)
		}
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

func TestTaskStartOptionalSelectionCreatesNoIntentJournalOrReviewActions(t *testing.T) {
	root, policyRoot, intent := newTaskStartCLIRepository(t)
	if err := os.WriteFile(intent, []byte("Explain checkout and purchase completion.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, ""), "--module", "application"))
	var packet engine.TaskStartPacket
	if err := json.Unmarshal([]byte(stdout), &packet); err != nil || status != 0 || stderr != "" {
		t.Fatalf("module packet: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	if packet.Intent.Captured || packet.Intent.WillBeUsed || packet.Intent.Capture != nil || len(packet.Intent.SelectedFeatures) != 0 || packet.RequestedSelection.Mode != "module" {
		t.Fatalf("optional module selection captured intent: %+v", packet)
	}
	if slices.ContainsFunc(packet.NextActions, func(action engine.TaskStartAction) bool {
		return action.Name == "review-status" || action.Name == "complete-reviews"
	}) {
		t.Fatalf("optional packet scheduled review work: %+v", packet.NextActions)
	}
	if _, err := os.Lstat(filepath.Join(root, ".code-polishy-reports", "behavior-review")); !os.IsNotExist(err) {
		t.Fatalf("optional task start created behavior review artifacts: %v", err)
	}
	status, _, _ = captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, ""), "--files", "value.go", "--situation", "authentication"))
	if status != 2 {
		t.Fatalf("invalid later task start status=%d", status)
	}
}

func TestTaskStartRejectsUnusedIntentAndRequiresSelectedIntent(t *testing.T) {
	root, policyRoot, intent := newTaskStartCLIRepository(t)
	status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--module", "application"))
	if status != 2 || stdout != "" || !strings.Contains(stderr, "will not use intent") {
		t.Fatalf("unused intent: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(root, ".code-polishy-reports", "behavior-review")); !os.IsNotExist(err) {
		t.Fatalf("unused intent created artifacts: %v", err)
	}
	status, stdout, stderr = captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, ""), "--module", "application", "--feature", "checkout"))
	if status != 2 || stdout != "" || !strings.Contains(stderr, "requires intent because behavior review is selected") {
		t.Fatalf("missing selected intent: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func TestTaskStartCapturesSelectedIntentFromStandardInput(t *testing.T) {
	root, policyRoot, _ := newTaskStartCLIRepository(t)
	arguments := append(taskStartCLIArguments(root, policyRoot, "-"), "--files", "value.go", "--feature", "checkout")
	status, stdout, stderr := captureRunOutputWithStdin(t, arguments, "Exact request from standard input.\n")
	var packet engine.TaskStartPacket
	if err := json.Unmarshal([]byte(stdout), &packet); err != nil || status != 0 || stderr != "" || packet.Intent.Capture == nil {
		t.Fatalf("stdin task start: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	journal, err := os.ReadFile(filepath.Join(root, packet.Intent.Capture.JournalPath))
	if err != nil || !strings.Contains(string(journal), "Exact request from standard input.\\n") {
		t.Fatalf("stdin intent missing from journal: %v %s", err, journal)
	}
}

func TestTaskStartRequiresIntentForConfiguredReviewScope(t *testing.T) {
	for _, requiredAt := range []string{"merge", "checkpoint"} {
		t.Run(requiredAt, func(t *testing.T) {
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
			config["verification"].(map[string]any)["behaviorReview"].(map[string]any)["defaultRequiredAt"] = requiredAt
			data, err = json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			writeBehaviorReviewCLIFile(t, root, policy.ConfigFilename, string(data))
			gitBehaviorReviewCLI(t, root, "add", policy.ConfigFilename)
			gitBehaviorReviewCLI(t, root, "commit", "-m", "Require behavior review")
			status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, ""), "--files", "value.go"))
			if status != 2 || stdout != "" || !strings.Contains(stderr, "requires intent because behavior review is selected") {
				t.Fatalf("required scope without intent: status=%d stdout=%q stderr=%q", status, stdout, stderr)
			}
			status, stdout, stderr = captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--files", "value.go"))
			var packet engine.TaskStartPacket
			if err := json.Unmarshal([]byte(stdout), &packet); err != nil || status != 0 || stderr != "" || !packet.Intent.WillBeUsed || packet.Intent.Capture == nil ||
				len(packet.Intent.Capture.Features) != 0 || !slices.Equal(packet.Intent.SelectedFeatures, []string{"checkout"}) {
				t.Fatalf("required scope capture: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
			}
		})
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
	arguments := []string{"--repo-root", root, "--policy-root", policyRoot, "task-start"}
	if intent != "" {
		arguments = append(arguments, "--intent-file", intent)
	}
	return arguments
}

func captureRunOutputWithStdin(t *testing.T, arguments []string, input string) (int, string, string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = previous
		_ = reader.Close()
	}()
	return captureRunOutput(t, arguments)
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
	status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--files", "value.go", "--feature", "checkout"))
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
	status, stdout, stderr := captureRunOutput(t, append(taskStartCLIArguments(root, policyRoot, intent), "--files", "docs", "--feature", "checkout"))
	if status != 2 || stdout != "" || !strings.Contains(stderr, "authenticated locked capability catalog") {
		t.Fatalf("unavailable catalog: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(root, ".code-polishy-reports")); !os.IsNotExist(err) {
		t.Fatalf("unavailable catalog left an intent capture: %v", err)
	}
}
