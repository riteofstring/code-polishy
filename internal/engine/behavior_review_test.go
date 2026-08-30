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

	agentpolicy "github.com/riteofstring/code-polishy/internal/agents"
	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestMergeGateBypassesBehaviorReviewForDocumentationCandidates(t *testing.T) {
	root := documentationRepository(t)
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

func TestCheckpointGateReportsMissingBehaviorReviewBeforeCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertCheckpointBehaviorReviewFinding(t, report, "missing behavior review receipt")
	if len(commandRunner.commands) != 0 {
		t.Fatalf("missing receipt ran commands: %v", commandRunner.commands)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("missing receipt recorded a checkpoint: %v", statErr)
	}
}

func TestDoctorReportsMissingReportArtifactIgnoreAtTheOwnedPath(t *testing.T) {
	root := contentRepository(t, nil)
	policyRoot := enginePolicyRoot(t)
	installBehaviorReviewTestGuidance(t, root)
	if err := os.Remove(filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	policyEngine, err := Open(root, policyRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	report, err := policyEngine.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.reportArtifacts" && finding.Path == ".gitignore" &&
			finding.Subject == "workspace-ignore" && strings.Contains(finding.Message, "agents sync")
	}) {
		t.Fatalf("findings = %+v", report.Findings)
	}
}

func TestCheckpointGateRunsChangedScopeAndRecordsAcceptedCandidate(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.CheckpointPolicy == nil || report.CheckpointPolicy.Scope != behaviorreview.CheckpointScopeChanged ||
		report.CheckpointPolicy.ReceiptPath != behaviorreview.CheckpointReceiptPath || report.GateRunPolicy == nil || report.GateRunPolicy.Status != "passed" {
		t.Fatalf("report = %+v", report)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(report.GateRunPolicy.ReportPath))); err != nil {
		t.Fatalf("checkpoint gate report is unavailable: %v", err)
	}
	if !slices.Equal(commandRunner.commands, []string{"content-check", "focused"}) {
		t.Fatalf("checkpoint commands = %v", commandRunner.commands)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath)))
	if err != nil {
		t.Fatal(err)
	}
	var receipt behaviorreview.CheckpointReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Candidate != report.CheckpointPolicy.Candidate || receipt.Scope != behaviorreview.CheckpointScopeChanged || receipt.BehaviorReviewID == "" {
		t.Fatalf("checkpoint receipt = %+v", receipt)
	}
	accepted, err := behaviorreview.ReadCheckpoint(t.Context(), policyEngine.Repository)
	if err != nil || accepted != receipt {
		t.Fatalf("ReadCheckpoint() receipt = %+v, error = %v, want %+v", accepted, err, receipt)
	}
}

func TestCheckpointGateReplaysRegressionProofsBeforeChangedChecks(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &behaviorReviewReplayRunner{candidateRoot: root, baselineStatus: 1}
	policyEngine.Runner = commandRunner
	prepareRequestedBehaviorReviewReceipt(t, policyEngine, root, commandRunner, nil)
	commandRunner.events = nil
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil || len(report.Findings) != 0 {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	if len(commandRunner.events) < 4 || commandRunner.events[0] != "replay:focused" || commandRunner.events[1] != "replay:focused" ||
		commandRunner.events[2] != "planned:content-check" || commandRunner.events[3] != "planned:focused" {
		t.Fatalf("checkpoint replay and verification order = %v", commandRunner.events)
	}
	assertBehaviorProofGateOutcome(t, root, report)
}

func TestCheckpointGateDoesNotRecordAfterChangedTestFailure(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	commandRunner := &failingEngineRunner{failure: "focused"}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.CheckpointPolicy == nil || report.CheckpointPolicy.ReceiptPath != "" ||
		report.GateRunPolicy == nil || report.GateRunPolicy.Status != "failed" {
		t.Fatalf("report = %+v", report)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed checkpoint recorded a receipt: %v", statErr)
	}
}

func TestCheckpointGateDoesNotRecordWhenGateArtifactFinalizationFails(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	policyEngine.Runner = &checkpointArtifactFailureRunner{root: root}
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err == nil {
		t.Fatal("checkpoint gate unexpectedly succeeded")
	}
	if report.CheckpointPolicy == nil || report.CheckpointPolicy.ReceiptPath != "" || report.GateRunPolicy == nil ||
		report.GateRunPolicy.Status != string(gaterun.RunOperational) {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	if report.GateRunPolicy.ReportPath != "" {
		t.Fatalf("failed gate retained a current report path: %+v", report.GateRunPolicy)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))); statErr != nil {
		t.Fatalf("bound receipt is unavailable: %v", statErr)
	}
	if _, readErr := behaviorreview.ReadCheckpoint(t.Context(), policyEngine.Repository); !errors.Is(readErr, behaviorreview.ErrStaleCheckpoint) {
		t.Fatalf("ReadCheckpoint() error = %v, want stale checkpoint", readErr)
	}
}

func TestCheckpointGateFinalizesOperationalWhenReceiptPublicationFails(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	path := filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	policyEngine.Runner = &recordingEngineRunner{}
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err == nil {
		t.Fatal("checkpoint gate unexpectedly succeeded")
	}
	if report.CheckpointPolicy == nil || report.CheckpointPolicy.ReceiptPath != "" || report.GateRunPolicy == nil ||
		report.GateRunPolicy.Status != string(gaterun.RunOperational) || report.GateRunPolicy.ReportPath == "" {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	if _, readErr := behaviorreview.ReadCheckpoint(t.Context(), policyEngine.Repository); !errors.Is(readErr, behaviorreview.ErrStaleCheckpoint) {
		t.Fatalf("ReadCheckpoint() error = %v, want stale checkpoint", readErr)
	}
}

func TestCheckpointGateRecordsDocumentationWithoutBehaviorReview(t *testing.T) {
	root := documentationRepository(t)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "README.md", "# Updated\n", 0o600)
	commitEngineCandidate(t, root, "documentation candidate")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.CheckpointPolicy == nil || report.CheckpointPolicy.Scope != behaviorreview.CheckpointScopeDocumentation ||
		report.CheckpointPolicy.ReceiptPath != behaviorreview.CheckpointReceiptPath || len(commandRunner.commands) != 0 {
		t.Fatalf("report = %+v commands = %v", report, commandRunner.commands)
	}
}

func TestCheckpointGateTreatsUnchangedCandidateAsNoOp(t *testing.T) {
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.CheckpointPolicy == nil || report.CheckpointPolicy.Scope != "unchanged" ||
		report.CheckpointPolicy.ReceiptPath != "" || len(commandRunner.commands) != 0 {
		t.Fatalf("report = %+v commands = %v", report, commandRunner.commands)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unchanged checkpoint wrote a receipt: %v", statErr)
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

func TestMergeGateResumeReusesOnlyPassedOrdinaryTestsFromIdenticalFailedRun(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	failedRunner := &failingEngineRunner{failure: "offline-supply"}
	policyEngine.Runner = failedRunner
	failed, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if failed.GateRunPolicy == nil || failed.GateRunPolicy.Status != "failed" || len(failed.Findings) == 0 {
		t.Fatalf("failed report = %+v", failed)
	}

	resumedRunner := &recordingEngineRunner{}
	policyEngine.Runner = resumedRunner
	resumed, err := policyEngine.MergeGateWithOptions(t.Context(), "main", MergeGateOptions{Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.Findings) != 0 || resumed.GateRunPolicy == nil || resumed.GateRunPolicy.Status != "passed" ||
		!slices.Equal(resumed.GateRunPolicy.ReusedPhases, []string{"focused"}) {
		t.Fatalf("resumed report = %+v", resumed)
	}
	if slices.Contains(resumedRunner.commands, "focused") {
		t.Fatalf("resumed ordinary test executed again: %v", resumedRunner.commands)
	}
	for _, command := range []string{"content-check", "content-build", "offline-supply"} {
		if !slices.Contains(resumedRunner.commands, command) {
			t.Fatalf("resume skipped non-reusable %q: %v", command, resumedRunner.commands)
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
	assertBehaviorProofGateOutcome(t, root, report)
}

func assertBehaviorProofGateOutcome(t *testing.T, root string, report Report) {
	t.Helper()
	if report.GateRunPolicy == nil || report.GateRunPolicy.ReportPath == "" {
		t.Fatalf("gate run policy = %+v", report.GateRunPolicy)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(report.GateRunPolicy.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	var artifact gaterun.Report
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(artifact.Commands, func(outcome gaterun.CommandOutcome) bool {
		return outcome.Category == gaterun.BehaviorProof && outcome.Status == gaterun.Passed && !outcome.Reused
	}) {
		t.Fatalf("behavior proof outcome is absent: %+v", artifact.Commands)
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
	if _, err := behaviorreview.ValidateGateReceipt(t.Context(), policyEngine.Repository, behaviorreview.ValidateGateReceiptOptions{Base: "main"}); err != nil {
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
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "behavior-review")
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "candidate")
	return root
}

func installBehaviorReviewTestGuidance(t *testing.T, root string) {
	t.Helper()
	if _, err := agentpolicy.Install(root, enginePolicyRoot(t)); err != nil {
		t.Fatal(err)
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

type checkpointArtifactFailureRunner struct {
	root      string
	sabotaged bool
}

func (runner *checkpointArtifactFailureRunner) Run(_ context.Context, _ string, _ policy.Command) error {
	if runner.sabotaged {
		return nil
	}
	paths, err := filepath.Glob(filepath.Join(runner.root, ".code-polishy-reports", string(gaterun.CheckpointGate), "*"))
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return errors.New("checkpoint gate artifact directory is unavailable")
	}
	if err := os.Mkdir(filepath.Join(paths[0], "latest.json"), 0o700); err != nil {
		return err
	}
	runner.sabotaged = true
	return nil
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
		finding.Subject != "gate-receipt" || !strings.Contains(finding.Message, message) {
		t.Fatalf("finding = %+v", finding)
	}
}

func assertCheckpointBehaviorReviewFinding(t *testing.T, report Report, message string) {
	t.Helper()
	if report.CheckpointPolicy == nil || report.CheckpointPolicy.Scope != behaviorreview.CheckpointScopeChanged || len(report.Findings) != 1 {
		t.Fatalf("checkpoint policy = %+v findings = %+v", report.CheckpointPolicy, report.Findings)
	}
	finding := report.Findings[0]
	if finding.Check != "policy.behaviorReview" || finding.Path != ".code-polishy-reports/behavior-review/receipt.json" ||
		finding.Subject != "gate-receipt" || !strings.Contains(finding.Message, message) {
		t.Fatalf("finding = %+v", finding)
	}
}

func enginePolicyRoot(t *testing.T) string {
	t.Helper()
	goExecutable, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("resolve governed Go executable: %v", err)
	}
	current := filepath.Dir(goExecutable)
	for {
		version, versionErr := os.Stat(filepath.Join(current, "VERSION"))
		schema, schemaErr := os.Stat(filepath.Join(current, "schema", "code-polishy.schema.json"))
		if versionErr == nil && schemaErr == nil && version.Mode().IsRegular() && schema.Mode().IsRegular() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatalf("governed Go executable is outside a Code Polishy policy root: %s", goExecutable)
		}
		current = parent
	}
}

func gitBehaviorReview(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
