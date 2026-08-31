package gaterun

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunWritesBoundedArtifactsAndStrictReport(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit"), testCommand(Check, "quality")})
	run := startRun(t, t.TempDir(), identity)
	first := recordAttempt(t, run, 0, Passed, 0, "123456", "abcdef", 4)
	assertBoundedOutcome(t, first, run.ReportPath())
	recordAttempt(t, run, 1, Failed, 1, "failure", "details", 16)
	report, err := run.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{"quality failed"}, BehaviorReview: identity.BehaviorReview})
	if err != nil {
		t.Fatal(err)
	}
	assertFailedReport(t, report)
	loaded, err := LoadReport(run.repositoryRoot, identity)
	if err != nil {
		t.Fatal(err)
	}
	assertLoadedReport(t, loaded, report)
	info, err := os.Stat(physicalArtifactPath(run.repositoryRoot, run.ReportPath()))
	if err != nil {
		t.Fatal(err)
	}
	assertRestrictiveFile(t, info.Mode().Perm())
	log, _, err := readAttemptLog(run, identity, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertBoundedLog(t, log)
}

func assertBoundedOutcome(t *testing.T, outcome CommandOutcome, reportPath string) {
	t.Helper()
	if len(outcome.Attempts) != 1 || !outcome.Attempts[0].StdoutTruncated || !outcome.Attempts[0].StderrTruncated || outcome.ReceiptPath == "" ||
		!strings.Contains(reportPath, "/executions/run-") {
		t.Fatalf("outcome = %+v, report path = %q", outcome, reportPath)
	}
}

func assertFailedReport(t *testing.T, report Report) {
	t.Helper()
	if report.SHA256 == "" || !validExecutionID(report.ExecutionID) || len(report.Commands) != 2 || len(report.Commands[0].Attempts) != 1 ||
		report.Commands[0].Attempts[0].DurationMilliseconds < 0 {
		t.Fatalf("report = %+v", report)
	}
}

func assertLoadedReport(t *testing.T, loaded, report Report) {
	t.Helper()
	if loaded.SHA256 != report.SHA256 || loaded.Commands[0].ReceiptSHA256 != report.Commands[0].ReceiptSHA256 || loaded.ExecutionID != report.ExecutionID {
		t.Fatalf("loaded report = %+v, want %+v", loaded, report)
	}
}

func assertRestrictiveFile(t *testing.T, mode os.FileMode) {
	t.Helper()
	if mode != 0o600 {
		t.Fatalf("report permissions = %o, want 600", mode)
	}
}

func assertBoundedLog(t *testing.T, log commandLogDocument) {
	t.Helper()
	if string(log.Stdout) != "12" || string(log.StdoutTail) != "56" || string(log.Stderr) != "ab" || string(log.StderrTail) != "ef" ||
		!log.StdoutTruncated || !log.StderrTruncated || log.StdoutBytes != 6 || log.StderrBytes != 6 {
		t.Fatalf("log = %+v", log)
	}
}

func TestMergeRunKeepsFailedEvidenceImmutableUntilLaterTestReuse(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(Check, "quality"), testCommand(OrdinaryTest, "unit")})
	root := t.TempDir()
	failed := startRun(t, root, identity)
	recordAttempt(t, failed, 0, Failed, 1, "", "quality failed", 16)
	recordAttempt(t, failed, 1, Passed, 0, "unit pass", "", 16)
	prior, err := failed.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview})
	if err != nil {
		t.Fatal(err)
	}
	resumed := startRun(t, root, identity)
	if resumed.ReportPath() == failed.ReportPath() {
		t.Fatalf("resumed path %q overwrites failed path", resumed.ReportPath())
	}
	recordAttempt(t, resumed, 0, Passed, 0, "quality pass", "", 16)
	reusable, err := LoadReusableReceipt(root, identity, 1)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := resumed.RecordReuse(1, reusable)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Reused || len(entry.Attempts) != 0 || entry.Status != Passed {
		t.Fatalf("reused entry = %+v", entry)
	}
	if _, err := LoadReport(root, identity); err != nil {
		t.Fatalf("prior failed report no longer validates: %v", err)
	}
	passed, err := resumed.Finalize(FinalizeOptions{Status: RunPassed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview})
	if err != nil {
		t.Fatal(err)
	}
	if !passed.Commands[1].Reused || passed.Commands[0].Reused || passed.ExecutionID == prior.ExecutionID {
		t.Fatalf("passed report = %+v", passed)
	}
}

func TestMergeRunReloadsAndResumesAfterAnotherLateFailure(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit"), testCommand(Check, "quality")})
	root := t.TempDir()
	initial := startRun(t, root, identity)
	recordAttempt(t, initial, 0, Passed, 0, "unit pass", "", 16)
	recordAttempt(t, initial, 1, Failed, 1, "", "initial late failure", 16)
	initialReport, err := initial.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview})
	if err != nil {
		t.Fatal(err)
	}

	firstReusable, err := LoadReusableReceipt(root, identity, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstResume := startRun(t, root, identity)
	firstOutcome, err := firstResume.RecordReuse(0, firstReusable)
	if err != nil {
		t.Fatal(err)
	}
	if firstOutcome.ReceiptPath == firstReusable.Path || !strings.Contains(firstOutcome.ReceiptPath, firstResume.executionID) {
		t.Fatalf("first resumed receipt path = %q, source = %q", firstOutcome.ReceiptPath, firstReusable.Path)
	}
	recordAttempt(t, firstResume, 1, Failed, 1, "", "resumed late failure", 16)
	firstReport, err := firstResume.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReport(root, identity)
	if err != nil {
		t.Fatalf("reloading resumed failed run: %v", err)
	}
	if loaded.ExecutionID != firstReport.ExecutionID || loaded.Commands[0].ReceiptPath != firstOutcome.ReceiptPath {
		t.Fatalf("loaded resumed report = %+v", loaded)
	}

	secondReusable, err := LoadReusableReceipt(root, identity, 0)
	if err != nil {
		t.Fatalf("reusing from resumed failed run: %v", err)
	}
	if secondReusable.Path != firstOutcome.ReceiptPath || secondReusable.Receipt.SourceExecutionID != initialReport.ExecutionID ||
		secondReusable.Receipt.SourceReceiptSHA256 != initialReport.Commands[0].ReceiptSHA256 {
		t.Fatalf("second reusable receipt = %+v", secondReusable)
	}
	secondResume := startRun(t, root, identity)
	secondOutcome, err := secondResume.RecordReuse(0, secondReusable)
	if err != nil {
		t.Fatal(err)
	}
	if secondOutcome.ReceiptPath == secondReusable.Path || !strings.Contains(secondOutcome.ReceiptPath, secondResume.executionID) {
		t.Fatalf("second resumed receipt path = %q, source = %q", secondOutcome.ReceiptPath, secondReusable.Path)
	}
	recordAttempt(t, secondResume, 1, Failed, 1, "", "second resumed late failure", 16)
	if _, err := secondResume.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReport(root, identity); err != nil {
		t.Fatalf("reloading second resumed failed run: %v", err)
	}
}

func TestOnlyOrdinaryTestsCanProvideReusableReceipts(t *testing.T) {
	for _, category := range []CommandCategory{Check, Build, SupplyChain, ArtifactSecurity, BehaviorProof} {
		t.Run(string(category), func(t *testing.T) {
			identity := testIdentity(t, []CommandSpec{testCommand(category, "phase")})
			root := t.TempDir()
			run := startRun(t, root, identity)
			recordAttempt(t, run, 0, Passed, 0, "pass", "", 16)
			if _, err := run.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview}); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadReusableReceipt(root, identity, 0); !errors.Is(err, ErrIneligible) {
				t.Fatalf("LoadReusableReceipt() error = %v, want ineligible", err)
			}
		})
	}
}

func TestDiagnosticAttemptDoesNotChangePlannedOutcomeOrReceipt(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, t.TempDir(), identity)
	recordAttempt(t, run, 0, Failed, 1, "", "first failure", 16)
	diagnostic := recordDiagnosticAttempt(t, run, 0, Passed, 0, "retry pass", "", 16)
	if diagnostic.Status != Failed || diagnostic.ReceiptPath != "" || diagnostic.ReceiptSHA256 != "" || len(diagnostic.Attempts) != 2 ||
		!diagnostic.Attempts[1].Diagnostic || diagnostic.Attempts[1].Status != Passed {
		t.Fatalf("diagnostic outcome = %+v", diagnostic)
	}
	if _, err := run.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview}); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReusableReceipt(run.repositoryRoot, identity, 0); !errors.Is(err, ErrMissingArtifact) {
		t.Fatalf("diagnostic receipt error = %v, want missing artifact", err)
	}
}

func TestDiagnosticAttemptRequiresFailedPlannedOutcome(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, t.TempDir(), identity)
	log, err := run.OpenCommandLog(0, LogOptions{StreamLimit: 16})
	if err != nil {
		t.Fatal(err)
	}
	result, err := log.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = run.RecordAttempt(0, AttemptInput{Status: Passed, Duration: time.Second, Diagnostic: true}, result)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("RecordAttempt() error = %v, want invalid input", err)
	}
}

func TestBoundedLogResultKeepsTerminalTail(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(Check, "quality")})
	run := startRun(t, t.TempDir(), identity)
	log, err := run.OpenCommandLog(0, LogOptions{StreamLimit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Stderr().Write([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	result, err := log.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !result.StderrTruncated || string(result.StderrTail) != "ef" || !strings.Contains(string(result.Stderr), "ef") {
		t.Fatalf("bounded result = %+v", result)
	}
}

func TestFinalizeBindsTypedTestEvidenceAndDiagnosticState(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, t.TempDir(), identity)
	planned := recordAttempt(t, run, 0, Failed, 1, "", "failure", 16)
	diagnostic := recordDiagnosticAttempt(t, run, 0, Failed, 1, "", "retry failure", 16)
	initialEvidence := testEvidenceForAttempt(planned.Attempts[0], false)
	diagnosticEvidence := testEvidenceForAttempt(diagnostic.Attempts[1], true)
	diagnosticState := TestDiagnostic{Suite: "unit", State: "baseline-unavailable", CandidateRetry: &diagnosticEvidence}
	report, err := run.Finalize(FinalizeOptions{
		Status: RunFailed, Findings: []Finding{}, Notes: []string{}, TestEvidence: []TestEvidence{initialEvidence, diagnosticEvidence},
		TestDiagnostics: []TestDiagnostic{diagnosticState}, BehaviorReview: identity.BehaviorReview,
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReport(run.repositoryRoot, identity)
	if err != nil {
		t.Fatal(err)
	}
	loaded.TestEvidence[0].SuiteModules[0] = "changed"
	if report.TestEvidence[0].SuiteModules[0] == "changed" || loaded.TestDiagnostics[0].CandidateRetry.LogPath == "" {
		t.Fatalf("report evidence cloning failed: %+v", loaded)
	}
}

func TestFinalizeRecordsBehaviorReviewReplayFailure(t *testing.T) {
	for _, withReceipt := range []bool{false, true} {
		t.Run(fmt.Sprintf("receipt=%t", withReceipt), func(t *testing.T) {
			identity := testIdentityWithBehaviorReview(t, []CommandSpec{testCommand(Check, "quality")}, passedBehaviorReview())
			run := startRun(t, t.TempDir(), identity)
			recordAttempt(t, run, 0, Failed, 1, "", "replay failed", 16)
			outcome := cloneBehaviorReview(identity.BehaviorReview)
			outcome.State = BehaviorReviewFailed
			if !withReceipt {
				outcome.ReviewID, outcome.ReceiptPath = "", ""
			}
			report, err := run.Finalize(FinalizeOptions{
				Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: outcome,
			})
			if err != nil {
				t.Fatal(err)
			}
			if report.BehaviorReview.State != BehaviorReviewFailed || report.BehaviorReview.SelectionDigest != identity.BehaviorReview.SelectionDigest ||
				report.BehaviorReview.ReviewID != outcome.ReviewID || report.BehaviorReview.ReceiptPath != outcome.ReceiptPath {
				t.Fatalf("report behavior review = %+v", report.BehaviorReview)
			}
			loaded, err := LoadReport(run.repositoryRoot, identity)
			if err != nil {
				t.Fatal(err)
			}
			loaded.BehaviorReview.SelectedFeatures[0].Reasons[0] = "changed"
			if report.BehaviorReview.SelectedFeatures[0].Reasons[0] == "changed" {
				t.Fatalf("report behavior review cloning failed: %+v", loaded.BehaviorReview)
			}
		})
	}
}

func TestFinalizeRejectsBehaviorReviewOutcomeTransitions(t *testing.T) {
	cases := []struct {
		name    string
		planned BehaviorReview
		outcome BehaviorReview
	}{
		{
			name: "not run becomes required",
			planned: func() BehaviorReview {
				value := requiredBehaviorReview("task-request")
				value.State = BehaviorReviewNotRun
				return value
			}(),
			outcome: requiredBehaviorReview("task-request"),
		},
		{
			name:    "required becomes passed",
			planned: requiredBehaviorReview("task-request"),
			outcome: passedBehaviorReview(),
		},
		{
			name: "failed becomes passed",
			planned: func() BehaviorReview {
				value := passedBehaviorReview()
				value.State = BehaviorReviewFailed
				return value
			}(),
			outcome: passedBehaviorReview(),
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			identity := testIdentityWithBehaviorReview(t, []CommandSpec{testCommand(Check, "quality")}, test.planned)
			run := startRun(t, t.TempDir(), identity)
			recordAttempt(t, run, 0, Failed, 1, "", "gate failed", 16)
			if _, err := run.Finalize(FinalizeOptions{
				Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: test.outcome,
			}); !errors.Is(err, ErrStaleArtifact) {
				t.Fatalf("Finalize() error = %v, want stale artifact", err)
			}
		})
	}
}

func testEvidenceForAttempt(attempt Attempt, diagnostic bool) TestEvidence {
	return TestEvidence{
		Name: "unit", Kind: "unit", Scope: "module", Cost: "quick", Target: "working-tree", SuiteModules: []string{"gaterun"},
		SuitePaths: []string{}, ChangedModules: []string{"gaterun"}, ImpactedModules: []string{"gaterun"},
		ChangedModuleOverlap: []string{"gaterun"}, ImpactedModuleOverlap: []string{"gaterun"}, ChangedPathOverlap: []string{},
		Status: attempt.Status, FailureCategory: attempt.FailureCategory, Attempt: attempt.Number, LogPath: attempt.LogPath, Diagnostic: diagnostic,
	}
}

func TestLoadRejectsTamperedReportReceiptAndLog(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string, report Report)
	}{
		{
			name: "report", mutate: func(t *testing.T, root string, report Report) {
				writeRuntimeFile(t, reportPhysicalPath(root, report), []byte(`{"version":1}`))
			},
		},
		{
			name: "receipt", mutate: func(t *testing.T, root string, report Report) {
				writeRuntimeFile(t, physicalArtifactPath(root, report.Commands[0].ReceiptPath), []byte(`{}`))
			},
		},
		{
			name: "log", mutate: func(t *testing.T, root string, report Report) {
				writeRuntimeFile(t, physicalArtifactPath(root, report.Commands[0].Attempts[0].LogPath), []byte(`{}`))
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root, identity, report := failedRunWithReceipt(t)
			test.mutate(t, root, report)
			if _, err := LoadReusableReceipt(root, identity, 0); !errors.Is(err, ErrInvalidArtifact) && !errors.Is(err, ErrStaleArtifact) {
				t.Fatalf("LoadReusableReceipt() error = %v, want invalid or stale artifact", err)
			}
		})
	}
}

func TestLogWriteRejectsDirectorySymlinkSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	root := t.TempDir()
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	run := startRun(t, root, identity)
	log, err := run.OpenCommandLog(0, LogOptions{StreamLimit: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Stderr().Write([]byte("failure")); err != nil {
		t.Fatal(err)
	}
	logsPath := filepath.Join(root, filepath.FromSlash(filepath.Dir(run.ReportPath())), logsDirectory)
	holdingPath := logsPath + ".holding"
	outside := t.TempDir()
	if err := os.Rename(logsPath, holdingPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, logsPath); err != nil {
		t.Fatal(err)
	}
	_, closeErr := log.Close()
	if err := os.Remove(logsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(holdingPath, logsPath); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(closeErr, ErrInvalidArtifact) {
		t.Fatalf("Close() error = %v, want invalid artifact", closeErr)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory swap wrote outside artifacts: %+v", entries)
	}
}

func TestFinalizeRejectsLatestPointerSymlinkReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	root := t.TempDir()
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	prior := startRun(t, root, identity)
	recordAttempt(t, prior, 0, Passed, 0, "pass", "", 16)
	if _, err := prior.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview}); err != nil {
		t.Fatal(err)
	}
	current := startRun(t, root, identity)
	recordAttempt(t, current, 0, Passed, 0, "pass", "", 16)
	latestPath := filepath.Join(root, reportsDirectory, string(identity.Gate), identityDigest(t, identity), latestFilename)
	if err := os.Remove(latestPath); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "latest.json")
	writeRuntimeFile(t, outside, []byte("outside"))
	if err := os.Symlink(outside, latestPath); err != nil {
		t.Fatal(err)
	}
	if _, err := current.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview}); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("Finalize() error = %v, want invalid artifact", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside" {
		t.Fatalf("latest pointer replacement wrote outside artifact: %q", data)
	}
}

func TestLoadRejectsSymlinkEscapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	root := t.TempDir()
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit")})
	if err := os.Symlink(t.TempDir(), filepath.Join(root, reportsDirectory)); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(StartOptions{RepositoryRoot: root, Identity: identity}); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("Start() error = %v, want invalid artifact", err)
	}
	root, identity, report := failedRunWithReceipt(t)
	receiptPath := physicalArtifactPath(root, report.Commands[0].ReceiptPath)
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "receipt.json")
	writeRuntimeFile(t, outside, []byte(`{}`))
	if err := os.Symlink(outside, receiptPath); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReusableReceipt(root, identity, 0); !errors.Is(err, ErrInvalidArtifact) {
		t.Fatalf("LoadReusableReceipt() error = %v, want invalid artifact", err)
	}
}

func TestReceiptRejectsCandidateSelectionAndLogChangesBeforeReuse(t *testing.T) {
	root, identity, report := failedRunWithReceipt(t)
	reusable, err := LoadReusableReceipt(root, identity, 0)
	if err != nil {
		t.Fatal(err)
	}
	changedInput := testIdentityInput(identity.Commands)
	changedInput.Candidate = strings.Repeat("c", 40)
	changed, err := NewIdentity(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReusableReceipt(root, changed, 0); !errors.Is(err, ErrMissingArtifact) {
		t.Fatalf("changed candidate receipt error = %v, want missing artifact", err)
	}
	selectionChangedInput := testIdentityInput(identity.Commands)
	selectionChangedInput.BehaviorReview = requiredBehaviorReview("task-request")
	selectionChanged, err := NewIdentity(selectionChangedInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReusableReceipt(root, selectionChanged, 0); !errors.Is(err, ErrMissingArtifact) {
		t.Fatalf("changed behavior review selection receipt error = %v, want missing artifact", err)
	}
	writeRuntimeFile(t, physicalArtifactPath(root, report.Commands[0].Attempts[0].LogPath), []byte(`{}`))
	resumed := startRun(t, root, identity)
	if _, err := resumed.RecordReuse(0, reusable); !errors.Is(err, ErrInvalidArtifact) && !errors.Is(err, ErrStaleArtifact) {
		t.Fatalf("RecordReuse() error = %v, want invalid or stale artifact", err)
	}
}

func testIdentity(t *testing.T, commands []CommandSpec) Identity {
	t.Helper()
	return testIdentityWithBehaviorReview(t, commands, notRunBehaviorReview())
}

func testIdentityWithBehaviorReview(t *testing.T, commands []CommandSpec, review BehaviorReview) Identity {
	t.Helper()
	input := testIdentityInput(commands)
	input.BehaviorReview = cloneBehaviorReview(review)
	identity, err := NewIdentity(input)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func startRun(t *testing.T, root string, identity Identity) *Run {
	t.Helper()
	run, err := Start(StartOptions{RepositoryRoot: root, Identity: identity, StartedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func recordAttempt(t *testing.T, run *Run, index int, status CommandStatus, exitStatus int, stdout, stderr string, limit int) CommandOutcome {
	t.Helper()
	return recordAttemptWithKind(t, run, index, status, exitStatus, stdout, stderr, limit, false)
}

func recordDiagnosticAttempt(t *testing.T, run *Run, index int, status CommandStatus, exitStatus int, stdout, stderr string, limit int) CommandOutcome {
	t.Helper()
	return recordAttemptWithKind(t, run, index, status, exitStatus, stdout, stderr, limit, true)
}

func recordAttemptWithKind(t *testing.T, run *Run, index int, status CommandStatus, exitStatus int, stdout, stderr string, limit int, diagnostic bool) CommandOutcome {
	t.Helper()
	log, err := run.OpenCommandLog(index, LogOptions{StreamLimit: limit})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Stdout().Write([]byte(stdout)); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Stderr().Write([]byte(stderr)); err != nil {
		t.Fatal(err)
	}
	result, err := log.Close()
	if err != nil {
		t.Fatal(err)
	}
	input := AttemptInput{Status: status, ExitStatus: exitStatus, Duration: time.Second, ResourceWait: time.Millisecond, Diagnostic: diagnostic}
	if status == Failed {
		input.FailureCategory = CommandExit
	}
	outcome, err := run.RecordAttempt(index, input, result)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func failedRunWithReceipt(t *testing.T) (string, Identity, Report) {
	t.Helper()
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit"), testCommand(Check, "quality")})
	root := t.TempDir()
	run := startRun(t, root, identity)
	recordAttempt(t, run, 0, Passed, 0, "pass", "", 16)
	recordAttempt(t, run, 1, Failed, 1, "", "failed", 16)
	report, err := run.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview})
	if err != nil {
		t.Fatal(err)
	}
	return root, identity, report
}

func readAttemptLog(run *Run, identity Identity, index, attempt int) (commandLogDocument, []byte, error) {
	reference, err := identity.Command(index)
	if err != nil {
		return commandLogDocument{}, nil, err
	}
	file, _, err := run.logPath(reference.SHA256, attempt, false)
	if err != nil {
		return commandLogDocument{}, nil, err
	}
	return readCommandLog(file)
}

func reportPhysicalPath(root string, report Report) string {
	return filepath.Join(root, reportsDirectory, string(report.Identity.Gate), report.IdentitySHA256, executionsDirectory, report.ExecutionID, reportFilename)
}

func identityDigest(t *testing.T, identity Identity) string {
	t.Helper()
	digest, err := identity.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func physicalArtifactPath(root, display string) string {
	return filepath.Join(root, filepath.FromSlash(display))
}

func writeRuntimeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
