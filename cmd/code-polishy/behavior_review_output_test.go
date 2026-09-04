package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func assertBehaviorReviewCLIReceiptConfirmation(t *testing.T, common []string) {
	t.Helper()
	status, stdout, stderr := runBehaviorReviewCLI(t, append(slices.Clone(common), "behavior-review", "status", "--base", "main"))
	assertBehaviorReviewCLISuccess(t, status, stdout, stderr)
	assertBehaviorReviewCLIOutputContains(t, stdout, "BEHAVIOR REVIEW: PASSED (all changes)")
	assertBehaviorReviewCLIOutputContains(t, stdout, "BEHAVIOR REVIEW RECEIPT: .code-polishy-reports/behavior-review/receipt.json")
}

func TestBehaviorReviewCLIConfirmsCanonicalAliasesInCapturedHumanAndJSONOutput(t *testing.T) {
	for _, format := range []string{"human", "json"} {
		t.Run(format, func(t *testing.T) {
			root, intent := newBehaviorReviewCLIBaseRepositoryWithReviewPolicy(t, `{
  "features": [
    {"name":"checkout","description":"Purchase completion.","aliases":["purchase completion"],"modules":["application"],"suites":["regression"]},
    {"name":"search","description":"Search results.","aliases":["Straße"],"paths":["value.go"],"suites":["regression"]}
  ]
}`)
			common := []string{"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t), "behavior-review"}
			status, stdout, stderr := runBehaviorReviewCLI(t, append(slices.Clone(common),
				"capture-intent", "--intent-file", intent, "--feature", " PURCHASE\u00a0COMPLETION ", "--feature=STRASSE", "--format", format,
			))
			assertBehaviorReviewCLISuccess(t, status, stdout, stderr)
			journal := readBehaviorReviewCLIIntentJournal(t, root)
			if len(journal.Requirements) != 1 || !slices.Equal(journal.Requirements[0].Features, []string{"checkout", "search"}) {
				t.Fatalf("journal requirements = %+v", journal.Requirements)
			}
			journalPath := ".code-polishy-reports/behavior-review/intent-journal.json"
			assertCanonicalCaptureConfirmation(t, format, stdout, journalPath)
			commitBehaviorReviewCLICandidate(t, root)
			before, err := os.ReadFile(filepath.Join(root, journalPath))
			if err != nil {
				t.Fatal(err)
			}
			status, stdout, stderr = runBehaviorReviewCLI(t, append(slices.Clone(common), "status", "--base", "main", "--format="+format))
			assertBehaviorReviewCLISuccess(t, status, stdout, stderr)
			assertCanonicalStatusConfirmation(t, format, stdout)
			after, err := os.ReadFile(filepath.Join(root, journalPath))
			if err != nil || string(before) != string(after) {
				t.Fatalf("status changed the journal: %v", err)
			}
			for _, artifact := range []string{"packet.json", "prepare.json", "receipt.json"} {
				if _, err := os.Stat(filepath.Join(root, ".code-polishy-reports", "behavior-review", artifact)); !os.IsNotExist(err) {
					t.Fatalf("capture or status created review artifact %s: %v", artifact, err)
				}
			}
		})
	}
}

func TestBehaviorReviewCLIRejectsOutputOptionsBeforeCapturingIntent(t *testing.T) {
	root, intent := newFeatureBehaviorReviewCLIBaseRepository(t)
	common := []string{"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t), "behavior-review", "capture-intent", "--intent-file", intent}
	for _, flags := range [][]string{
		{"--format", "sarif"}, {"--format", "json", "--format=human"}, {"--format"}, {"--format="}, {"--output", "report.json"},
	} {
		status, stdout, stderr := runBehaviorReviewCLI(t, append(slices.Clone(common), flags...))
		assertBehaviorReviewCLIRejected(t, status, stdout, stderr)
		if _, err := os.Stat(filepath.Join(root, ".code-polishy-reports", "behavior-review", "intent-journal.json")); !os.IsNotExist(err) {
			t.Fatalf("invalid flags %v wrote a journal: %v", flags, err)
		}
	}
}

func TestBehaviorReviewCaptureWithoutFeaturesConfirmsNone(t *testing.T) {
	root, intent := newFeatureBehaviorReviewCLIBaseRepository(t)
	status, stdout, stderr := runBehaviorReviewCLI(t, []string{
		"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t), "behavior-review", "capture-intent", "--intent-file", intent,
	})
	assertBehaviorReviewCLISuccess(t, status, stdout, stderr)
	if !strings.Contains(stdout, "features none") {
		t.Fatalf("capture confirmation omitted empty explicit selection: %q", stdout)
	}
	if journal := readBehaviorReviewCLIIntentJournal(t, root); len(journal.Requirements) != 0 {
		t.Fatalf("capture selected unrequested features: %+v", journal.Requirements)
	}
}

func assertCanonicalCaptureConfirmation(t *testing.T, format, stdout, journalPath string) {
	t.Helper()
	if format == "json" {
		var document behaviorReviewOutputDocument
		if err := json.Unmarshal([]byte(stdout), &document); err != nil {
			t.Fatalf("capture did not emit one JSON document: %v; stdout=%q", err, stdout)
		}
		if document.Protocol != "behavior-review/v1" || document.Action != "capture-intent" || document.State != "captured" || document.Capture == nil || document.Status != nil {
			t.Fatalf("capture confirmation = %+v", document)
		}
		if document.Capture.JournalPath != journalPath || document.Capture.ID == "" || !slices.Equal(document.Capture.Features, []string{"checkout", "search"}) {
			t.Fatalf("capture result = %+v", document.Capture)
		}
	} else {
		for _, expected := range []string{"Behavior review intent captured:", journalPath, "features checkout, search"} {
			assertBehaviorReviewCLIOutputContains(t, stdout, expected)
		}
	}
}

func assertCanonicalStatusConfirmation(t *testing.T, format, stdout string) {
	t.Helper()
	if format == "json" {
		var document behaviorReviewOutputDocument
		if err := json.Unmarshal([]byte(stdout), &document); err != nil {
			t.Fatalf("status did not emit one JSON document: %v; stdout=%q", err, stdout)
		}
		if document.Protocol != "behavior-review/v1" || document.Action != "status" || document.State != string(engine.BehaviorReviewRequired) || document.Status == nil || document.Capture != nil {
			t.Fatalf("status confirmation = %+v", document)
		}
		if !slices.Equal(document.Status.TaskRequested, []string{"checkout", "search"}) || !slices.Equal(document.Status.Required, []string{"checkout", "search"}) {
			t.Fatalf("status = %+v", document.Status)
		}
	} else {
		assertBehaviorReviewCLIOutputContains(t, stdout, "BEHAVIOR REVIEW: REQUIRED (checkout, search)")
		assertBehaviorReviewCLIOutputContains(t, stdout, "TASK-REQUESTED FEATURES: checkout, search")
	}
}
