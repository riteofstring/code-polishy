package gaterun

import (
	"errors"
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
	report, err := run.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{"quality failed"}})
	if err != nil {
		t.Fatal(err)
	}
	assertFailedReport(t, report)
	loaded, err := LoadReport(run.repositoryRoot, identity)
	if err != nil {
		t.Fatal(err)
	}
	assertLoadedReport(t, loaded, report)
	path := filepath.Join(run.repositoryRoot, filepath.FromSlash(run.ReportPath()))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	assertRestrictiveFile(t, info.Mode().Perm())
	logPath := filepath.Join(run.repositoryRoot, filepath.FromSlash(first.Attempts[0].LogPath))
	log, _, err := readCommandLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	assertBoundedLog(t, log)
}

func assertBoundedOutcome(t *testing.T, outcome CommandOutcome, reportPath string) {
	t.Helper()
	if len(outcome.Attempts) != 1 || !outcome.Attempts[0].StdoutTruncated || !outcome.Attempts[0].StderrTruncated || outcome.ReceiptPath == "" ||
		!strings.HasPrefix(reportPath, ".code-polishy-reports/merge-gate/") {
		t.Fatalf("outcome = %+v, report path = %q", outcome, reportPath)
	}
}

func assertFailedReport(t *testing.T, report Report) {
	t.Helper()
	if report.SHA256 == "" || len(report.Commands) != 2 || len(report.Commands[0].Attempts) != 1 || report.Commands[0].Attempts[0].DurationMilliseconds < 0 {
		t.Fatalf("report = %+v", report)
	}
}

func assertLoadedReport(t *testing.T, loaded, report Report) {
	t.Helper()
	if loaded.SHA256 != report.SHA256 || loaded.Commands[0].ReceiptSHA256 != report.Commands[0].ReceiptSHA256 {
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
	if string(log.Stdout) != "1234" || string(log.Stderr) != "abcd" || !log.StdoutTruncated || !log.StderrTruncated {
		t.Fatalf("log = %+v", log)
	}
}

func TestMergeRunResumesOnlyPassedOrdinaryTests(t *testing.T) {
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit"), testCommand(Check, "quality")})
	root := t.TempDir()
	failed := startRun(t, root, identity)
	recordAttempt(t, failed, 0, Passed, 0, "unit pass", "", 16)
	recordAttempt(t, failed, 1, Failed, 1, "", "quality failed", 16)
	if _, err := failed.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}}); err != nil {
		t.Fatal(err)
	}
	reusable, err := LoadReusableReceipt(root, identity, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReusableReceipt(root, identity, 1); !errors.Is(err, ErrIneligible) {
		t.Fatalf("check receipt error = %v, want ineligible", err)
	}
	resumed := startRun(t, root, identity)
	entry, err := resumed.RecordReuse(0, reusable)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Reused || len(entry.Attempts) != 0 || entry.Status != Passed {
		t.Fatalf("reused entry = %+v", entry)
	}
	recordAttempt(t, resumed, 1, Passed, 0, "quality pass", "", 16)
	passed, err := resumed.Finalize(FinalizeOptions{Status: RunPassed, Findings: []Finding{}, Notes: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if !passed.Commands[0].Reused || passed.Commands[1].Reused {
		t.Fatalf("passed report = %+v", passed.Commands)
	}
}

func TestOnlyOrdinaryTestsCanProvideReusableReceipts(t *testing.T) {
	for _, category := range []CommandCategory{Check, Build, SupplyChain, ArtifactSecurity, BehaviorProof} {
		t.Run(string(category), func(t *testing.T) {
			identity := testIdentity(t, []CommandSpec{testCommand(category, "phase")})
			root := t.TempDir()
			run := startRun(t, root, identity)
			recordAttempt(t, run, 0, Passed, 0, "pass", "", 16)
			if _, err := run.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}}); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadReusableReceipt(root, identity, 0); !errors.Is(err, ErrIneligible) {
				t.Fatalf("LoadReusableReceipt() error = %v, want ineligible", err)
			}
		})
	}
}

func TestLoadRejectsTamperedReportReceiptAndLog(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string, report Report)
	}{
		{
			name: "report", mutate: func(t *testing.T, root string, report Report) {
				writeRuntimeFile(t, filepath.Join(root, ".code-polishy-reports", "merge-gate", report.IdentitySHA256, reportFilename), []byte(`{"version":1}`))
			},
		},
		{
			name: "receipt", mutate: func(t *testing.T, root string, report Report) {
				writeRuntimeFile(t, filepath.Join(root, filepath.FromSlash(report.Commands[0].ReceiptPath)), []byte(`{}`))
			},
		},
		{
			name: "log", mutate: func(t *testing.T, root string, report Report) {
				writeRuntimeFile(t, filepath.Join(root, filepath.FromSlash(report.Commands[0].Attempts[0].LogPath)), []byte(`{}`))
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
	receiptPath := filepath.Join(root, filepath.FromSlash(report.Commands[0].ReceiptPath))
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

func TestReceiptRejectsCandidateAndLogChangesBeforeReuse(t *testing.T) {
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
	writeRuntimeFile(t, filepath.Join(root, filepath.FromSlash(report.Commands[0].Attempts[0].LogPath)), []byte(`{}`))
	resumed := startRun(t, root, identity)
	if _, err := resumed.RecordReuse(0, reusable); !errors.Is(err, ErrInvalidArtifact) && !errors.Is(err, ErrStaleArtifact) {
		t.Fatalf("RecordReuse() error = %v, want invalid or stale artifact", err)
	}
}

func testIdentity(t *testing.T, commands []CommandSpec) Identity {
	t.Helper()
	identity, err := NewIdentity(testIdentityInput(commands))
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
	input := AttemptInput{Status: status, ExitStatus: exitStatus, Duration: time.Second, ResourceWait: time.Millisecond}
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
	report, err := run.Finalize(FinalizeOptions{Status: RunFailed, Findings: []Finding{}, Notes: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return root, identity, report
}

func writeRuntimeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
