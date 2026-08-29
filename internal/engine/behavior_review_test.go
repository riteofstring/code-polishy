package engine

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/policy"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestMergeGateBypassesRequiredBehaviorReviewForDocumentationCandidates(t *testing.T) {
	root := documentationRepository(t)
	enableRequiredBehaviorReview(t, root)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "README.md", "# Updated\n", 0o600)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if report.MergePolicy == nil || report.MergePolicy.Level != testpolicy.MergeLevelDocumentation ||
		slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool { return finding.Check == "policy.behaviorReview" }) {
		t.Fatalf("report = %+v", report)
	}
	if len(commandRunner.commands) != 0 {
		t.Fatalf("documentation merge gate commands = %v", commandRunner.commands)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".code-polishy-reports", "behavior-review")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("documentation merge gate created behavior-review artifacts: %v", statErr)
	}
}

func TestMergeGateReportsMissingRequiredBehaviorReviewReceiptBeforeCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFinding(t, report, "missing behavior review receipt")
	if len(commandRunner.commands) != 0 {
		t.Fatalf("missing receipt ran commands: %v", commandRunner.commands)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".code-polishy-reports", "behavior-review")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("missing receipt validation created behavior-review artifacts: %v", statErr)
	}
}

func TestMergeGateReportsStaleBehaviorReviewReceiptBeforeCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":2}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "change candidate after review")
	policyEngine, err = Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFinding(t, report, "stale behavior review receipt")
	if len(commandRunner.commands) != 0 {
		t.Fatalf("stale receipt ran commands: %v", commandRunner.commands)
	}
}

func TestMergeGateContinuesItsPlannedCommandsAfterAValidBehaviorReviewReceipt(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.MergePolicy == nil || report.MergePolicy.Level != testpolicy.MergeLevelRecommended {
		t.Fatalf("report = %+v", report)
	}
	for _, command := range []string{"content-check", "focused", "content-build", "offline-supply"} {
		if !slices.Contains(commandRunner.commands, command) {
			t.Fatalf("valid receipt skipped planned %q: %v", command, commandRunner.commands)
		}
	}
}

func requiredBehaviorReviewCandidate(t *testing.T) string {
	t.Helper()
	root := contentRepository(t, nil)
	enableRequiredBehaviorReview(t, root)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "behavior-review")
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "candidate")
	return root
}

func enableRequiredBehaviorReview(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, ".code-polishy.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	verification, _ := document["verification"].(map[string]any)
	if verification == nil {
		verification = map[string]any{}
		document["verification"] = verification
	}
	verification["behaviorReview"] = map[string]any{"required": true}
	updated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, ".code-polishy.json", string(updated)+"\n", 0o600)
}

func installBehaviorReviewTestGuidance(t *testing.T, root string) {
	t.Helper()
	policyRoot := enginePolicyRoot(t)
	for target, source := range map[string]string{
		"AGENTS.md": filepath.Join(policyRoot, "templates", "AGENTS.md"),
		"CLAUDE.md": filepath.Join(policyRoot, "templates", "CLAUDE.md"),
	} {
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		writeEngineFile(t, root, target, string(data), 0o600)
	}
}

func prepareValidBehaviorReviewReceipt(t *testing.T, policyEngine *Engine, root string) {
	t.Helper()
	intentPath := filepath.Join(t.TempDir(), "intent.md")
	if err := os.WriteFile(intentPath, []byte("Preserve the requested behavior.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := policyEngine.PrepareBehaviorReview(t.Context(), "main", intentPath)
	if err != nil {
		t.Fatal(err)
	}
	review := behaviorreview.ReviewResult{
		Version: 1, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: prepared.Candidate, IntentSHA256: prepared.IntentSHA256,
		Behaviors: []behaviorreview.Behavior{{
			Before: "The baseline content is available.", After: "The candidate content is available.", Classification: "preserved", ProofIDs: []string{},
		}}, Findings: []string{},
	}
	data, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(root, ".code-polishy-reports", "behavior-review", "result.json")
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := policyEngine.FinalizeBehaviorReview(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
}

func assertBehaviorReviewGateFinding(t *testing.T, report Report, message string) {
	t.Helper()
	if report.MergePolicy == nil || report.MergePolicy.Level != testpolicy.MergeLevelRecommended || len(report.Findings) != 1 {
		t.Fatalf("merge policy = %+v findings = %+v", report.MergePolicy, report.Findings)
	}
	finding := report.Findings[0]
	if finding.Check != "policy.behaviorReview" || finding.Path != ".code-polishy-reports/behavior-review/receipt.json" ||
		finding.Subject != "merge-receipt" || !strings.Contains(finding.Message, message) {
		t.Fatalf("finding = %+v", finding)
	}
}

func enginePolicyRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}

func gitBehaviorReview(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
