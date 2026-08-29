package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseBehaviorReviewOptionsAcceptsStrictPrepareAndFinalizeRequests(t *testing.T) {
	t.Parallel()
	prepare, err := parseBehaviorReviewOptions([]string{"prepare", "--intent-file=intent.md", "--base", "origin/main"})
	if err != nil || prepare.action != "prepare" || prepare.base != "origin/main" || prepare.intentFile != "intent.md" {
		t.Fatalf("prepare=%+v err=%v", prepare, err)
	}
	finalize, err := parseBehaviorReviewOptions([]string{"finalize", "--base=origin/main"})
	if err != nil || finalize.action != "finalize" || finalize.base != "origin/main" || finalize.intentFile != "" {
		t.Fatalf("finalize=%+v err=%v", finalize, err)
	}
}

func TestParseBehaviorReviewOptionsRejectsIncompleteDuplicateAndUnknownRequests(t *testing.T) {
	t.Parallel()
	for _, arguments := range [][]string{
		nil,
		{"unknown"},
		{"prepare", "--base", "origin/main"},
		{"prepare", "--intent-file", "intent.md"},
		{"prepare", "--base", "origin/main", "--base", "HEAD", "--intent-file", "intent.md"},
		{"prepare", "--base", "origin/main", "--intent-file", "intent.md", "extra"},
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
	status, stdout, stderr := runBehaviorReviewCLI(t, []string{"--policy-root", policyRoot, "help"})
	if status != 0 || stderr != "" || !strings.Contains(stdout, "behavior-review prepare --base REF --intent-file PATH") ||
		!strings.Contains(stdout, "regression-proof --base REF --suite NAME --evidence PATH... --id ID") {
		t.Fatalf("help status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}

	repositoryRoot, intent := newBehaviorReviewCLIRepository(t)
	status, stdout, stderr = runBehaviorReviewCLI(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", policyRoot,
		"behavior-review", "prepare", "--base", "main",
	})
	if status != 2 || stdout != "" || !strings.Contains(stderr, "--intent-file") {
		t.Fatalf("missing prepare option status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = runBehaviorReviewCLI(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", policyRoot,
		"regression-proof", "--base", "main", "--suite", "regression", "--evidence", "evidence_test.go", "--id", "proof", "--id", "duplicate",
	})
	if status != 2 || stdout != "" || !strings.Contains(stderr, "only once") {
		t.Fatalf("duplicate proof option status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = runBehaviorReviewCLI(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", policyRoot,
		"behavior-review", "prepare", "--base", "missing-base", "--intent-file", intent,
	})
	if status != 2 || stdout != "" || !strings.Contains(stderr, "operational error:") {
		t.Fatalf("prepare operational failure status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if _, err := os.Stat(intent); err != nil {
		t.Fatalf("intent was changed by rejected CLI calls: %v", err)
	}
}

func TestBehaviorReviewCLIExecutesPrepareProofAndFinalizeWorkflow(t *testing.T) {
	repositoryRoot, intent := newBehaviorReviewCLIRepository(t)
	policyRoot := behaviorReviewCLIPolicyRoot(t)
	common := []string{"--repo-root", repositoryRoot, "--policy-root", policyRoot}

	status, stdout, stderr := runBehaviorReviewCLI(t, append(append([]string{}, common...), "behavior-review", "prepare", "--base", "main", "--intent-file", intent))
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
		"version":       1,
		"review_id":     packet.ReviewID,
		"base":          packet.Base,
		"candidate":     packet.Candidate,
		"intent_sha256": packet.IntentSHA256,
		"behaviors": []any{map[string]any{
			"before": "Value returns old.", "after": "Value returns new.", "classification": "requested", "proof_ids": []string{"value-change"},
		}},
		"findings": []string{},
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
	for _, path := range []string{
		".code-polishy-reports/behavior-review/packet.json",
		".code-polishy-reports/behavior-review/proofs/value-change.json",
		".code-polishy-reports/behavior-review/receipt.json",
	} {
		if info, statErr := os.Stat(filepath.Join(repositoryRoot, filepath.FromSlash(path))); statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("artifact %s: info=%v err=%v", path, info, statErr)
		}
	}
}

type behaviorReviewCLIPacket struct {
	ReviewID     string `json:"review_id"`
	Base         string `json:"base"`
	Candidate    string `json:"candidate"`
	IntentSHA256 string `json:"intent_sha256"`
}

type behaviorReviewCLIProof struct {
	Baseline           behaviorReviewCLIExecution `json:"baseline"`
	CandidateExecution behaviorReviewCLIExecution `json:"candidate_execution"`
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
	if packet.ReviewID == "" || packet.Base == "" || packet.Candidate == "" || packet.IntentSHA256 == "" {
		t.Fatalf("packet = %+v", packet)
	}
	return packet
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
	root := t.TempDir()
	writeBehaviorReviewCLIFile(t, root, ".code-polishy.json", `{
  "version": 3,
  "project": {"kind": "application", "capabilities": []},
  "scope": {},
  "quality": {},
  "portability": {},
  "modules": [{"name": "application", "paths": ["**"]}],
  "checks": [],
  "tests": {"suites": [{
    "name": "regression", "kind": "unit", "scope": "repository",
    "argv": ["go", "test", "./..."], "runOn": ["full"]
  }]},
  "supplyChain": {},
  "exceptions": []
}`+"\n")
	writeBehaviorReviewCLIFile(t, root, "go.mod", "module example.test/behavior-review-cli\n\ngo 1.26.0\n")
	writeBehaviorReviewCLIFile(t, root, "value.go", "package behaviorreviewcli\n\nfunc Value() string { return \"old\" }\n")
	gitBehaviorReviewCLI(t, root, "init", "-b", "main")
	gitBehaviorReviewCLI(t, root, "config", "user.email", "tests@example.invalid")
	gitBehaviorReviewCLI(t, root, "config", "user.name", "Code Polishy Tests")
	gitBehaviorReviewCLI(t, root, "add", ".")
	gitBehaviorReviewCLI(t, root, "commit", "-m", "base")
	gitBehaviorReviewCLI(t, root, "switch", "-c", "feature")
	writeBehaviorReviewCLIFile(t, root, "value.go", "package behaviorreviewcli\n\nfunc Value() string { return \"new\" }\n")
	writeBehaviorReviewCLIFile(t, root, "evidence_test.go", "package behaviorreviewcli\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\tif Value() != \"new\" {\n\t\tt.Fatalf(\"Value() = %q\", Value())\n\t}\n}\n")
	gitBehaviorReviewCLI(t, root, "add", "value.go", "evidence_test.go")
	gitBehaviorReviewCLI(t, root, "commit", "-m", "candidate")
	intent := writeBehaviorReviewCLIFile(t, t.TempDir(), "intent.md", "Change Value to return new.\n")
	return root, intent
}

func behaviorReviewCLIPolicyRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
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
