package gaterun

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestStoredTestOutcomesSeparateOtherFormatsFromInvalidRecords(t *testing.T) {
	root := t.TempDir()
	identity := testIdentity(t, []CommandSpec{testCommand(OrdinaryTest, "unit"), testCommand(Check, "quality")})
	run := startRun(t, root, identity)
	recordAttempt(t, run, 0, Failed, 1, "", "first failure", 16)
	recordAttempt(t, run, 0, Passed, 0, "retry pass", "", 16)
	recordAttempt(t, run, 1, Passed, 0, "", "", 16)
	if _, err := run.Finalize(FinalizeOptions{Status: RunPassed, Findings: []policy.Finding{}, Notes: []string{}, BehaviorReview: identity.BehaviorReview}); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(root, reportsDirectory, string(CheckpointGate), strings.Repeat("c", 64), executionsDirectory, "run-"+strings.Repeat("d", 32))
	if err := os.MkdirAll(older, 0o700); err != nil {
		t.Fatal(err)
	}
	writeRuntimeFile(t, filepath.Join(older, reportFilename), []byte(`{"version":1}`))
	damaged := filepath.Join(root, reportsDirectory, string(MergeGate), strings.Repeat("e", 64), executionsDirectory, "run-"+strings.Repeat("f", 32))
	if err := os.MkdirAll(damaged, 0o700); err != nil {
		t.Fatal(err)
	}
	writeRuntimeFile(t, filepath.Join(damaged, reportFilename), fmt.Appendf(nil, `{"version":%d}`, Version))

	history, err := StoredTestOutcomes(root)
	if err != nil {
		t.Fatal(err)
	}
	if history.OtherFormat != 1 || history.Invalid != 1 || len(history.Outcomes) != 1 {
		t.Fatalf("history = %+v", history)
	}
	outcome := history.Outcomes[0]
	if outcome.Command.Name != "unit" || outcome.Candidate != identity.Candidate || len(outcome.Command.Attempts) != 2 ||
		!outcome.StartedAt.Equal(time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestStoredTestOutcomesTreatRecordsWithoutAVersionAsInvalid(t *testing.T) {
	root := t.TempDir()
	for index, content := range []string{`{}`, `null`} {
		execution := filepath.Join(root, reportsDirectory, string(MergeGate), strings.Repeat(fmt.Sprint(index), 64), executionsDirectory, "run-"+strings.Repeat("a", 32))
		if err := os.MkdirAll(execution, 0o700); err != nil {
			t.Fatal(err)
		}
		writeRuntimeFile(t, filepath.Join(execution, reportFilename), []byte(content))
	}

	history, err := StoredTestOutcomes(root)
	if err != nil || history.Invalid != 2 || history.OtherFormat != 0 {
		t.Fatalf("history = %+v, error = %v", history, err)
	}
}

func TestStoredTestOutcomesTreatMissingRecordsAsEmptyHistory(t *testing.T) {
	history, err := StoredTestOutcomes(t.TempDir())
	if err != nil || history.OtherFormat != 0 || history.Invalid != 0 || len(history.Outcomes) != 0 {
		t.Fatalf("history = %+v, error = %v", history, err)
	}
}
