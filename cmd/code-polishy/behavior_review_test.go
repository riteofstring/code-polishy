package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseBehaviorReviewOptionsAcceptsStrictFeatureAndReviewRequests(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		arguments []string
		want      behaviorReviewOptions
	}{
		{
			name:      "capture intent",
			arguments: []string{"capture-intent", "--feature", "checkout", "--intent-file=intent.md", "--feature=search"},
			want:      behaviorReviewOptions{action: "capture-intent", intentFile: "intent.md", features: []string{"checkout", "search"}},
		},
		{
			name:      "require",
			arguments: []string{"require", "--feature=checkout", "--base", "origin/main", "--feature", "search"},
			want:      behaviorReviewOptions{action: "require", base: "origin/main", features: []string{"checkout", "search"}},
		},
		{
			name:      "status",
			arguments: []string{"status", "--base=origin/main"},
			want:      behaviorReviewOptions{action: "status", base: "origin/main"},
		},
		{
			name:      "prepare",
			arguments: []string{"prepare", "--base", "origin/main"},
			want:      behaviorReviewOptions{action: "prepare", base: "origin/main"},
		},
		{
			name:      "finalize",
			arguments: []string{"finalize", "--base=origin/main"},
			want:      behaviorReviewOptions{action: "finalize", base: "origin/main"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseBehaviorReviewOptions(test.arguments)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("options=%+v want=%+v", got, test.want)
			}
		})
	}
}

func TestParseBehaviorReviewOptionsRejectsIncompleteDuplicateAndUnknownRequests(t *testing.T) {
	t.Parallel()
	for _, arguments := range [][]string{
		nil,
		{"unknown"},
		{"capture-intent"},
		{"capture-intent", "--intent-file", "intent.md", "--intent-file", "other.md"},
		{"capture-intent", "--intent-file", "intent.md", "--feature"},
		{"capture-intent", "--base", "origin/main"},
		{"require"},
		{"require", "--base", "origin/main"},
		{"require", "--feature", "checkout"},
		{"require", "--base", "origin/main", "--feature", "checkout", "--base", "HEAD"},
		{"require", "--base", "origin/main", "--feature", "checkout", "--intent-file", "intent.md"},
		{"status"},
		{"status", "--feature", "checkout"},
		{"status", "--base", "origin/main", "--base", "HEAD"},
		{"prepare"},
		{"prepare", "--intent-file", "intent.md"},
		{"prepare", "--base", "origin/main", "--base", "HEAD"},
		{"prepare", "--base", "origin/main", "extra"},
		{"finalize"},
		{"finalize", "--base", "origin/main", "--intent-file", "intent.md"},
		{"finalize", "--base="},
	} {
		if _, err := parseBehaviorReviewOptions(arguments); err == nil {
			t.Fatalf("accepted invalid behavior-review request %v", arguments)
		}
	}
}

func TestParseRegressionProofOptionsAcceptsRepeatedEvidenceAndRedExit(t *testing.T) {
	t.Parallel()
	options, err := parseRegressionProofOptions([]string{
		"--id", "login-fix", "--evidence=internal/login/login_test.go", "--base", "abc123", "--suite", "login-unit",
		"--evidence", "web/login.spec.ts", "--red-exit", "3",
	})
	if err != nil || options.base != "abc123" || options.suite != "login-unit" || options.id != "login-fix" || options.redExit != 3 ||
		!slices.Equal(options.evidence, []string{"internal/login/login_test.go", "web/login.spec.ts"}) {
		t.Fatalf("options=%+v err=%v", options, err)
	}
}

func TestParseRegressionProofOptionsDefaultsRedExitAndRejectsInvalidRequests(t *testing.T) {
	t.Parallel()
	options, err := parseRegressionProofOptions([]string{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix"})
	if err != nil || options.redExit != 1 {
		t.Fatalf("options=%+v err=%v", options, err)
	}
	for _, arguments := range [][]string{
		nil,
		{"--base", "abc123", "--suite", "unit", "--id", "fix"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--suite", "other"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--evidence", "test.go", "--id", "fix"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--red-exit", "0"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--red-exit", "one"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--red-exit", "1", "--red-exit", "1"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--unknown"},
	} {
		if _, err := parseRegressionProofOptions(arguments); err == nil {
			t.Fatalf("accepted invalid regression-proof request %v", arguments)
		}
	}
}

func TestBehaviorReviewSuccessMessagesStayConciseAndAlwaysNameTheArtifact(t *testing.T) {
	t.Parallel()
	for name, message := range map[string]string{
		"capture":  behaviorReviewIntentCapturedMessage(".code-polishy-reports/behavior-review/intent-journal.json", "intent-123"),
		"require":  behaviorReviewRequirementAddedMessage(".code-polishy-reports/behavior-review/intent-journal.json", "requirement-123", []string{"checkout", "search"}),
		"prepare":  behaviorReviewPreparedMessage(".code-polishy-reports/behavior-review/packet.json", "review-123"),
		"finalize": behaviorReviewFinalizedMessage(".code-polishy-reports/behavior-review/receipt.json", "review-123"),
		"proof":    regressionProofMessage(".code-polishy-reports/behavior-review/proofs/login.json", "login"),
	} {
		if !strings.Contains(message, ".code-polishy-reports/behavior-review/") || strings.Contains(message, "\n") {
			t.Fatalf("%s message = %q", name, message)
		}
	}
}

func TestBehaviorReviewCLIHelpAndFailuresUsePublicExitContract(t *testing.T) {
	policyRoot := behaviorReviewCLIPolicyRoot(t)
	assertBehaviorReviewCLIHelpContract(t, policyRoot)
	assertBehaviorReviewCLIInputFailureContracts(t, policyRoot)
	assertBehaviorReviewCLIOperationalFailureContract(t, policyRoot)
}

func assertBehaviorReviewCLIHelpContract(t *testing.T, policyRoot string) {
	t.Helper()
	status, stdout, stderr := runBehaviorReviewCLI(t, []string{"--policy-root", policyRoot, "help"})
	assertBehaviorReviewCLISuccess(t, status, stdout, stderr)
	for _, syntax := range []string{
		"behavior-review capture-intent --intent-file PATH [--feature NAME...]",
		"behavior-review require --base REF --feature NAME...",
		"behavior-review status --base REF",
		"behavior-review prepare --base REF",
		"regression-proof --base REF --suite NAME --evidence PATH... --id ID",
	} {
		assertBehaviorReviewCLIOutputContains(t, stdout, syntax)
	}
}

func assertBehaviorReviewCLIInputFailureContracts(t *testing.T, policyRoot string) {
	t.Helper()
	repositoryRoot, _ := newBehaviorReviewCLIBaseRepository(t)
	commitBehaviorReviewCLICandidate(t, repositoryRoot)
	status, stdout, stderr := runBehaviorReviewCLI(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", policyRoot,
		"behavior-review", "prepare", "--base", "main",
	})
	assertBehaviorReviewCLIRejected(t, status, stdout, stderr, "capture the original request")
	status, stdout, stderr = runBehaviorReviewCLI(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", policyRoot,
		"behavior-review", "require", "--base", "main",
	})
	assertBehaviorReviewCLIRejected(t, status, stdout, stderr, "requires at least one --feature NAME", "Usage:\n  code-polishy behavior-review")
	status, stdout, stderr = runBehaviorReviewCLI(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", policyRoot,
		"regression-proof", "--base", "main", "--suite", "regression", "--evidence", "evidence_test.go", "--id", "proof", "--id", "duplicate",
	})
	assertBehaviorReviewCLIRejected(t, status, stdout, stderr, "only once")
}

func assertBehaviorReviewCLIOperationalFailureContract(t *testing.T, policyRoot string) {
	t.Helper()
	repositoryRoot, intent := newBehaviorReviewCLIBaseRepository(t)
	status, stdout, stderr := runBehaviorReviewCLI(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", policyRoot,
		"behavior-review", "capture-intent", "--intent-file", intent,
	})
	assertBehaviorReviewCLISuccess(t, status, stdout, stderr)
	commitBehaviorReviewCLICandidate(t, repositoryRoot)
	status, stdout, stderr = runBehaviorReviewCLI(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", policyRoot,
		"behavior-review", "prepare", "--base", "missing-base",
	})
	assertBehaviorReviewCLIRejected(t, status, stdout, stderr, "operational error:")
	if _, err := os.Stat(intent); err != nil {
		t.Fatalf("intent was changed by rejected CLI calls: %v", err)
	}
}

func assertBehaviorReviewCLISuccess(t *testing.T, status int, stdout, stderr string) {
	t.Helper()
	if status != 0 {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func assertBehaviorReviewCLIRejected(t *testing.T, status int, stdout, stderr string, messages ...string) {
	t.Helper()
	if status != 2 {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
	for _, message := range messages {
		assertBehaviorReviewCLIOutputContains(t, stderr, message)
	}
}

func assertBehaviorReviewCLIOutputContains(t *testing.T, output, text string) {
	t.Helper()
	if !strings.Contains(output, text) {
		t.Fatalf("output omitted %q: %q", text, output)
	}
}

func TestBehaviorReviewCLICaptureIntentAcceptsRepeatedConfiguredFeatures(t *testing.T) {
	repositoryRoot, intent := newFeatureBehaviorReviewCLIBaseRepository(t)
	common := []string{"--repo-root", repositoryRoot, "--policy-root", behaviorReviewCLIPolicyRoot(t)}
	status, stdout, stderr := runBehaviorReviewCLI(t, append(append([]string{}, common...),
		"behavior-review", "capture-intent", "--feature", "search", "--intent-file", intent, "--feature=checkout",
	))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "Behavior review intent captured: .code-polishy-reports/behavior-review/intent-journal.json") {
		t.Fatalf("capture status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	journal := readBehaviorReviewCLIIntentJournal(t, repositoryRoot)
	if len(journal.Requirements) != 1 || !slices.Equal(journal.Requirements[0].Features, []string{"checkout", "search"}) {
		t.Fatalf("journal requirements = %+v", journal.Requirements)
	}
}

func TestBehaviorReviewCLIRequireAndStatusReportReadOnlyDecision(t *testing.T) {
	repositoryRoot, intent := newFeatureBehaviorReviewCLIBaseRepository(t)
	common := []string{"--repo-root", repositoryRoot, "--policy-root", behaviorReviewCLIPolicyRoot(t)}
	captureBehaviorReviewCLIIntentAndCommit(t, common, repositoryRoot, intent)
	assertBehaviorReviewCLIBaseAwarePlan(t, common, "NOT RUN (optional)")

	status, stdout, stderr := runBehaviorReviewCLI(t, append(append([]string{}, common...),
		"behavior-review", "require", "--base", "main", "--feature", "search", "--feature=checkout",
	))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "Behavior review requirement added: .code-polishy-reports/behavior-review/intent-journal.json") ||
		!strings.Contains(stdout, "features checkout, search") {
		t.Fatalf("require status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	assertBehaviorReviewCLIBaseAwarePlan(t, common, "REQUIRED (checkout, search)")
	journalPath := filepath.Join(repositoryRoot, ".code-polishy-reports", "behavior-review", "intent-journal.json")
	before, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}

	status, stdout, stderr = runBehaviorReviewCLI(t, append(append([]string{}, common...), "behavior-review", "status", "--base", "main"))
	if status != 0 || stderr != "" || strings.Count(stdout, "BEHAVIOR REVIEW:") != 1 {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	for _, want := range []string{
		"BEHAVIOR REVIEW: REQUIRED (checkout, search)",
		"AFFECTED FEATURES: checkout, search",
		"CONFIGURED FEATURES: checkout, search",
		"TASK-REQUESTED FEATURES: checkout, search",
		"REQUIRED FEATURES: checkout, search",
		"COMPLETED FEATURES: none",
		"MISSING FEATURES: checkout, search",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("status omitted %q: %q", want, stdout)
		}
	}
	after, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("behavior-review status changed the intent journal")
	}
	for _, path := range []string{"packet.json", "prepare.json", "receipt.json"} {
		if _, statErr := os.Stat(filepath.Join(repositoryRoot, ".code-polishy-reports", "behavior-review", path)); !os.IsNotExist(statErr) {
			t.Fatalf("behavior-review status wrote %s: %v", path, statErr)
		}
	}
}

func TestBehaviorReviewCLIExecutesPrepareProofFinalizeAndCheckpointWorkflow(t *testing.T) {
	repositoryRoot, intent := newBehaviorReviewCLIBaseRepository(t)
	policyRoot := behaviorReviewCLIPolicyRoot(t)
	common := []string{"--repo-root", repositoryRoot, "--policy-root", policyRoot}

	captureBehaviorReviewCLIIntentAndCommit(t, common, repositoryRoot, intent)

	status, stdout, stderr := runBehaviorReviewCLI(t, append(append([]string{}, common...), "behavior-review", "prepare", "--base", "main"))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "Behavior review prepared: .code-polishy-reports/behavior-review/packet.json") {
		t.Fatalf("prepare status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}

	status, stdout, stderr = runBehaviorReviewCLI(t, append(append([]string{}, common...), "regression-proof", "--base", "main", "--suite", "regression", "--evidence", "evidence_test.go", "--id", "value-change"))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "Regression proof recorded: .code-polishy-reports/behavior-review/proofs/value-change.json") {
		t.Fatalf("proof status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}

	packet := readBehaviorReviewCLIPacket(t, repositoryRoot)
	proof := readBehaviorReviewCLIProof(t, repositoryRoot)
	if proof.Baseline.ExitStatus != 1 || proof.CandidateExecution.ExitStatus != 0 {
		t.Fatalf("proof statuses = baseline %d candidate %d", proof.Baseline.ExitStatus, proof.CandidateExecution.ExitStatus)
	}
	result := map[string]any{
		"version":          4,
		"review_id":        packet.ReviewID,
		"base":             packet.Base,
		"candidate":        packet.Candidate,
		"intent_sha256":    packet.IntentSHA256,
		"selection_sha256": packet.SelectionSHA256,
		"decision_sha256":  packet.DecisionSHA256,
		"behaviors": []any{map[string]any{
			"before": "Value returns old.", "after": "Value returns new.", "classification": "requested", "proof_ids": []string{"value-change"},
			"scope": map[string]any{"features": []string{}, "full_candidate": true},
		}},
		"findings":             []string{},
		"final_state_findings": []any{},
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(repositoryRoot, ".code-polishy-reports", "behavior-review", "result.json")
	if err := os.WriteFile(resultPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	status, stdout, stderr = runBehaviorReviewCLI(t, append(append([]string{}, common...), "behavior-review", "finalize", "--base", "main"))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "Behavior review finalized: .code-polishy-reports/behavior-review/receipt.json") {
		t.Fatalf("finalize status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}

	assertBehaviorReviewCLIMerge(t, common)
	assertBehaviorReviewCLICheckpoint(t, common)
	assertBehaviorReviewCLIArtifacts(t, repositoryRoot, []string{
		".code-polishy-reports/behavior-review/intent-journal.json",
		".code-polishy-reports/behavior-review/packet.json",
		".code-polishy-reports/behavior-review/proofs/value-change.json",
		".code-polishy-reports/behavior-review/receipt.json",
		".code-polishy-reports/checkpoint-gate/receipt.json",
	})
}

func captureBehaviorReviewCLIIntentAndCommit(t *testing.T, common []string, repositoryRoot, intent string) {
	t.Helper()
	status, stdout, stderr := runBehaviorReviewCLI(t, append(append([]string{}, common...), "behavior-review", "capture-intent", "--intent-file", intent))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "Behavior review intent captured: .code-polishy-reports/behavior-review/intent-journal.json") {
		t.Fatalf("capture status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	commitBehaviorReviewCLICandidate(t, repositoryRoot)
}

func assertBehaviorReviewCLIArtifacts(t *testing.T, repositoryRoot string, paths []string) {
	t.Helper()
	for _, path := range paths {
		if info, statErr := os.Stat(filepath.Join(repositoryRoot, filepath.FromSlash(path))); statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("artifact %s: info=%v err=%v", path, info, statErr)
		}
	}
}

func assertBehaviorReviewCLICheckpoint(t *testing.T, common []string) {
	t.Helper()
	status, stdout, stderr := runBehaviorReviewCLI(t, append(append([]string{}, common...), "checkpoint-gate", "--base", "main"))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "CHECKPOINT GATE: CHANGED against main") || !strings.Contains(stdout, "CHECKPOINT ACCEPTED:") ||
		strings.Count(stdout, "BEHAVIOR REVIEW:") != 1 || !strings.Contains(stdout, "BEHAVIOR REVIEW: NOT RUN (optional)") {
		t.Fatalf("checkpoint status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

func assertBehaviorReviewCLIBaseAwarePlan(t *testing.T, common []string, behaviorReview string) {
	t.Helper()
	for _, command := range []string{"test-plan", "test-levels"} {
		status, stdout, stderr := runBehaviorReviewCLI(t, append(append([]string{}, common...), command, "--base", "main"))
		if status != 0 || stderr != "" || strings.Count(stdout, "BEHAVIOR REVIEW:") != 1 ||
			!strings.Contains(stdout, "BEHAVIOR REVIEW: "+behaviorReview) {
			t.Fatalf("%s status=%d stdout=%q stderr=%q", command, status, stdout, stderr)
		}
	}
}

func assertBehaviorReviewCLIMerge(t *testing.T, common []string) {
	t.Helper()
	status, stdout, stderr := runBehaviorReviewCLI(t, append(append([]string{}, common...), "merge-gate", "--base", "main"))
	if status != 0 || stderr != "" || !strings.Contains(stdout, "MERGE GATE:") || strings.Count(stdout, "BEHAVIOR REVIEW:") != 1 ||
		!strings.Contains(stdout, "BEHAVIOR REVIEW: PASSED (all changes)") {
		t.Fatalf("merge status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}

type behaviorReviewCLIPacket struct {
	ReviewID        string `json:"review_id"`
	Base            string `json:"base"`
	Candidate       string `json:"candidate"`
	IntentSHA256    string `json:"intent_sha256"`
	SelectionSHA256 string `json:"selection_sha256"`
	DecisionSHA256  string `json:"decision_sha256"`
}

type behaviorReviewCLIProof struct {
	Baseline           behaviorReviewCLIExecution `json:"baseline"`
	CandidateExecution behaviorReviewCLIExecution `json:"candidate_execution"`
}

type behaviorReviewCLIIntentJournal struct {
	Requirements []behaviorReviewCLITaskRequirement `json:"requirements"`
}

type behaviorReviewCLITaskRequirement struct {
	Features []string `json:"features"`
}

type behaviorReviewCLIExecution struct {
	ExitStatus int `json:"exit_status"`
}

func readBehaviorReviewCLIPacket(t *testing.T, root string) behaviorReviewCLIPacket {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".code-polishy-reports", "behavior-review", "packet.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packet behaviorReviewCLIPacket
	if err := json.Unmarshal(data, &packet); err != nil {
		t.Fatal(err)
	}
	if packet.ReviewID == "" || packet.Base == "" || packet.Candidate == "" || packet.IntentSHA256 == "" ||
		packet.SelectionSHA256 == "" || packet.DecisionSHA256 == "" {
		t.Fatalf("packet = %+v", packet)
	}
	return packet
}

func readBehaviorReviewCLIIntentJournal(t *testing.T, root string) behaviorReviewCLIIntentJournal {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".code-polishy-reports", "behavior-review", "intent-journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	var journal behaviorReviewCLIIntentJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		t.Fatal(err)
	}
	return journal
}

func readBehaviorReviewCLIProof(t *testing.T, root string) behaviorReviewCLIProof {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".code-polishy-reports", "behavior-review", "proofs", "value-change.json"))
	if err != nil {
		t.Fatal(err)
	}
	var proof behaviorReviewCLIProof
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	return proof
}

func runBehaviorReviewCLI(t *testing.T, arguments []string) (int, string, string) {
	t.Helper()
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		t.Fatal(err)
	}
	previousStdout, previousStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	defer func() {
		os.Stdout, os.Stderr = previousStdout, previousStderr
	}()
	status := run(arguments)
	if err := stdoutWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}
	if err := stdoutReader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrReader.Close(); err != nil {
		t.Fatal(err)
	}
	return status, string(stdout), string(stderr)
}

func newBehaviorReviewCLIRepository(t *testing.T) (string, string) {
	t.Helper()
	root, intent := newBehaviorReviewCLIBaseRepository(t)
	commitBehaviorReviewCLICandidate(t, root)
	return root, intent
}

func newBehaviorReviewCLIBaseRepository(t *testing.T) (string, string) {
	t.Helper()
	return newBehaviorReviewCLIBaseRepositoryWithReviewPolicy(t, `{"defaultRequiredAt":"merge","features":[]}`)
}

func newFeatureBehaviorReviewCLIBaseRepository(t *testing.T) (string, string) {
	t.Helper()
	return newBehaviorReviewCLIBaseRepositoryWithReviewPolicy(t, `{
  "features": [
    {"name":"checkout","modules":["application"],"suites":["regression"]},
    {"name":"search","paths":["value.go"],"suites":["regression"]}
  ]
}`)
}

func newBehaviorReviewCLIBaseRepositoryWithReviewPolicy(t *testing.T, behaviorReview string) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeBehaviorReviewCLIFile(t, root, ".code-polishy.json", `{
  "version": 3,
  "project": {"kind": "application", "capabilities": []},
  "scope": {},
  "quality": {},
  "verification": {"behaviorReview": `+behaviorReview+`},
  "portability": {},
  "modules": [{"name": "application", "paths": ["**"]}],
  "checks": [
    {"name": "build", "provides": ["build"], "modules": ["application"], "argv": ["true"], "runOn": ["build"]},
    {"name": "monitor", "provides": ["security-monitoring"], "argv": ["true"], "runOn": ["security"]}
  ],
  "tests": {"suites": [{
    "name": "regression", "kind": "unit", "scope": "module", "modules": ["application"],
    "cost": "quick", "argv": ["go", "test", "./..."], "runOn": ["focused", "recommended", "full"]
  }, {
    "name": "full", "kind": "integration", "scope": "repository",
    "argv": ["go", "test", "./..."], "runOn": ["full"]
  }]},
  "supplyChain": {},
  "exceptions": []
}`+"\n")
	writeBehaviorReviewCLIFile(t, root, "go.mod", "module example.test/behavior-review-cli\n\ngo 1.26.0\n")
	writeBehaviorReviewCLIFile(t, root, "value.go", "package behaviorreviewcli\n\nfunc Value() string { return \"old\" }\n")
	writeBehaviorReviewCLICanonicalGuidance(t, root)
	gitBehaviorReviewCLI(t, root, "init", "-b", "main")
	gitBehaviorReviewCLI(t, root, "config", "user.email", "tests@example.invalid")
	gitBehaviorReviewCLI(t, root, "config", "user.name", "Code Polishy Tests")
	gitBehaviorReviewCLI(t, root, "add", ".")
	gitBehaviorReviewCLI(t, root, "commit", "-m", "base")
	gitBehaviorReviewCLI(t, root, "switch", "-c", "feature")
	intent := writeBehaviorReviewCLIFile(t, t.TempDir(), "intent.md", "Change Value to return new.\n")
	return root, intent
}

func writeBehaviorReviewCLICanonicalGuidance(t *testing.T, root string) {
	t.Helper()
	policyRoot := behaviorReviewCLIPolicyRoot(t)
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		contents, err := os.ReadFile(filepath.Join(policyRoot, "templates", name))
		if err != nil {
			t.Fatal(err)
		}
		writeBehaviorReviewCLIFile(t, root, name, string(contents))
	}
	writeBehaviorReviewCLIFile(t, root, ".gitignore", "/.code-polishy-reports/\n/.code-polishy-artifacts/\n")
}

func commitBehaviorReviewCLICandidate(t *testing.T, root string) {
	t.Helper()
	writeBehaviorReviewCLIFile(t, root, "value.go", "package behaviorreviewcli\n\nfunc Value() string { return \"new\" }\n")
	writeBehaviorReviewCLIFile(t, root, "evidence_test.go", "package behaviorreviewcli\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\tif Value() != \"new\" {\n\t\tt.Fatalf(\"Value() = %q\", Value())\n\t}\n}\n")
	gitBehaviorReviewCLI(t, root, "add", "value.go", "evidence_test.go")
	gitBehaviorReviewCLI(t, root, "commit", "-m", "candidate")
}

func behaviorReviewCLIPolicyRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err == nil {
		if root, found := findPolicyRoot(workingDirectory); found {
			return root
		}
	}
	goExecutable, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("resolve governed Go executable: %v", err)
	}
	policyRoot, found := findPolicyRoot(filepath.Dir(goExecutable))
	if !found {
		t.Fatalf("governed Go executable is outside a Code Polishy policy root: %s", goExecutable)
	}
	return policyRoot
}

func writeBehaviorReviewCLIFile(t *testing.T, root, name, contents string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func gitBehaviorReviewCLI(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
