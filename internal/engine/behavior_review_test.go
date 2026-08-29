package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/riteofstring/code-polishy/internal/runner"
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

func TestMergeGateRetainsTrustedBaseBehaviorReviewRequirement(t *testing.T) {
	root := contentRepository(t, nil)
	enableRequiredBehaviorReview(t, root)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "remove-requirement")
	removeBehaviorReview(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", policy.ConfigFilename, "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "remove behavior review requirement")

	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFindingAtLevel(t, report, testpolicy.MergeLevelFull, "missing behavior review receipt")
	if len(commandRunner.commands) != 0 {
		t.Fatalf("trusted-base requirement ran commands: %v", commandRunner.commands)
	}
}

func TestMergeGateRequiresReceiptWhenCandidateAddsBehaviorReviewConfig(t *testing.T) {
	root := contentRepository(t, nil)
	configPath := filepath.Join(root, policy.ConfigFilename)
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "add-requirement")
	writeEngineFile(t, root, policy.ConfigFilename, string(config), 0o600)
	enableRequiredBehaviorReview(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", policy.ConfigFilename, "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "add behavior review requirement")

	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFindingAtLevel(t, report, testpolicy.MergeLevelFull, "missing behavior review receipt")
	if len(commandRunner.commands) != 0 {
		t.Fatalf("candidate requirement ran commands: %v", commandRunner.commands)
	}
}

func TestMergeGateDoesNotRequireReceiptWhenBothConfigurationsDisableBehaviorReview(t *testing.T) {
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "no-requirement")
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "candidate")

	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if report.MergePolicy == nil || report.MergePolicy.Level != testpolicy.MergeLevelRecommended ||
		slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool { return finding.Check == "policy.behaviorReview" }) {
		t.Fatalf("report = %+v", report)
	}
	if len(commandRunner.commands) == 0 {
		t.Fatal("disabled behavior review skipped planned commands")
	}
}

func TestMergeGateFailsWhenTrustedBaseBehaviorReviewConfigIsInvalid(t *testing.T) {
	root := contentRepository(t, nil)
	configPath := filepath.Join(root, policy.ConfigFilename)
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, policy.ConfigFilename, "{\n", 0o600)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "repair-config")
	writeEngineFile(t, root, policy.ConfigFilename, string(config), 0o600)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", policy.ConfigFilename, "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "repair configuration")

	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	if _, err := policyEngine.MergeGate(t.Context(), "main"); err == nil || !strings.Contains(err.Error(), "parse behavior-review configuration") {
		t.Fatalf("MergeGate() error = %v, want trusted-base configuration parse failure", err)
	}
	if len(commandRunner.commands) != 0 {
		t.Fatalf("invalid trusted-base configuration ran commands: %v", commandRunner.commands)
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

func TestMergeGateContinuesItsPlannedCommandsAfterAValidPreservedBehaviorReviewReceipt(t *testing.T) {
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

func TestMergeGateReplaysRequestedBehaviorReviewProofBeforePlannedCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &behaviorReviewReplayRunner{candidateRoot: root, baselineStatus: 1}
	policyEngine.Runner = commandRunner
	prepareRequestedBehaviorReviewReceipt(t, policyEngine, root, commandRunner, nil)
	commandRunner.events = nil

	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(commandRunner.events) < 3 || commandRunner.events[0] != "replay:focused" || commandRunner.events[1] != "replay:focused" {
		t.Fatalf("replay and planned command order = %v", commandRunner.events)
	}
	if !slices.ContainsFunc(commandRunner.events[2:], func(event string) bool { return strings.HasPrefix(event, "planned:") }) {
		t.Fatalf("replay did not continue to planned commands: %v", commandRunner.events)
	}
}

func TestMergeGateRejectsForgedRecordedProofBeforePlannedCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &behaviorReviewReplayRunner{candidateRoot: root, baselineStatus: 1}
	policyEngine.Runner = commandRunner
	prepareRequestedBehaviorReviewReceipt(t, policyEngine, root, commandRunner, forgeBehaviorReviewProofRecord)
	if _, err := policyEngine.validateBehaviorReviewMergeReceipt(t.Context(), "main"); err != nil {
		t.Fatalf("forged proof record was not statically valid: %v", err)
	}
	commandRunner.events = nil
	commandRunner.baselineStatus = 2

	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFinding(t, report, "replay baseline suite")
	if len(commandRunner.events) != 1 || commandRunner.events[0] != "replay:focused" {
		t.Fatalf("forged proof ran unexpected commands: %v", commandRunner.events)
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

func removeBehaviorReview(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	verification, ok := document["verification"].(map[string]any)
	if !ok {
		t.Fatalf("configuration has no verification object: %s", data)
	}
	delete(verification, "behaviorReview")
	updated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, policy.ConfigFilename, string(updated)+"\n", 0o600)
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

func prepareRequestedBehaviorReviewReceipt(t *testing.T, policyEngine *Engine, root string, commandRunner *behaviorReviewReplayRunner, forge func(*testing.T, string, string)) {
	t.Helper()
	evidence := "content/behavior.test.txt"
	writeEngineFile(t, root, evidence, "behavior evidence\n", 0o600)
	gitBehaviorReview(t, root, "add", evidence)
	gitBehaviorReview(t, root, "commit", "-m", "add behavior evidence")
	intentPath := filepath.Join(t.TempDir(), "intent.md")
	if err := os.WriteFile(intentPath, []byte("Change the content behavior.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := policyEngine.PrepareBehaviorReview(t.Context(), "main", intentPath)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := policyEngine.ProveRegression(t.Context(), "main", "focused", []string{evidence}, "content-change", 1)
	if err != nil {
		t.Fatal(err)
	}
	if forge != nil {
		forge(t, root, proof.ID)
	}
	review := behaviorreview.ReviewResult{
		Version: 1, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: prepared.Candidate, IntentSHA256: prepared.IntentSHA256,
		Behaviors: []behaviorreview.Behavior{{
			Before: "The baseline content is available.", After: "The candidate content is available.", Classification: "requested", ProofIDs: []string{proof.ID},
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
	if len(commandRunner.events) != 2 || commandRunner.events[0] != "replay:focused" || commandRunner.events[1] != "replay:focused" {
		t.Fatalf("proof recording events = %v", commandRunner.events)
	}
}

func forgeBehaviorReviewProofRecord(t *testing.T, root, id string) {
	t.Helper()
	proofDirectory := filepath.Join(root, ".code-polishy-reports", "behavior-review", "proofs")
	baselineLog := []byte("recorded baseline failure\n")
	candidateLog := []byte("recorded candidate success\n")
	for path, data := range map[string][]byte{
		filepath.Join(proofDirectory, id+".baseline.log"):  baselineLog,
		filepath.Join(proofDirectory, id+".candidate.log"): candidateLog,
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(proofDirectory, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var proof map[string]any
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	baseline, ok := proof["baseline"].(map[string]any)
	if !ok {
		t.Fatalf("baseline = %#v", proof["baseline"])
	}
	candidate, ok := proof["candidate_execution"].(map[string]any)
	if !ok {
		t.Fatalf("candidate execution = %#v", proof["candidate_execution"])
	}
	baseline["exit_status"] = 1
	baseline["log_sha256"] = behaviorReviewProofDigest(baselineLog)
	candidate["exit_status"] = 0
	candidate["log_sha256"] = behaviorReviewProofDigest(candidateLog)
	updated, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
}

func behaviorReviewProofDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

type behaviorReviewReplayRunner struct {
	candidateRoot  string
	baselineStatus int
	events         []string
}

func (commandRunner *behaviorReviewReplayRunner) Run(_ context.Context, _ string, command policy.Command) error {
	commandRunner.events = append(commandRunner.events, "planned:"+command.Name)
	return nil
}

func (commandRunner *behaviorReviewReplayRunner) RunWithOutput(_ context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	commandRunner.events = append(commandRunner.events, "replay:"+command.Name)
	data, err := os.ReadFile(filepath.Join(root, "content", "data.json"))
	if err != nil {
		return runner.Result{ExitStatus: -1}, runner.Output{}, err
	}
	if strings.Contains(string(data), "updated") {
		return runner.Result{ExitStatus: 0}, runner.Output{Stdout: []byte("candidate passed\n")}, nil
	}
	return runner.Result{ExitStatus: commandRunner.baselineStatus}, runner.Output{Stdout: []byte("baseline failed\n")}, errors.New("recorded failure")
}

func assertBehaviorReviewGateFinding(t *testing.T, report Report, message string) {
	t.Helper()
	assertBehaviorReviewGateFindingAtLevel(t, report, testpolicy.MergeLevelRecommended, message)
}

func assertBehaviorReviewGateFindingAtLevel(t *testing.T, report Report, level, message string) {
	t.Helper()
	if report.MergePolicy == nil || report.MergePolicy.Level != level || len(report.Findings) != 1 {
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
