package behaviorreview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestPreparedIntentCapturePublishesItsExactPreviewOnce(t *testing.T) {
	t.Parallel()
	repo, _ := newFeatureTaskBaseRepository(t)
	repo.Config.Verification.BehaviorReview.Features[0].Aliases = []string{"purchase"}
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Original request.\n")
	prepared, err := PrepareIntentCapture(t.Context(), repo, CaptureIntentOptions{IntentPath: intent, Features: []string{"PURCHASE"}})
	if err != nil {
		t.Fatal(err)
	}
	preview := prepared.Result()
	if preview.ID == "" || !slices.Equal(preview.Features, []string{"checkout"}) {
		t.Fatalf("preview = %+v", preview)
	}
	if _, err := os.Lstat(filepath.Join(repo.Root, ".code-polishy-reports")); !os.IsNotExist(err) {
		t.Fatalf("preparation created an artifact root: %v", err)
	}
	altered := prepared.Result()
	altered.Features[0] = "search"
	if err := os.WriteFile(intent, []byte("Later external text.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	captured, err := prepared.Commit(t.Context())
	if err != nil || !reflect.DeepEqual(captured, preview) {
		t.Fatalf("capture = %+v, preview = %+v, error = %v", captured, preview, err)
	}
	journal, err := readCaptureJournal(repo)
	if err != nil || len(journal.Entries) != 1 || journal.Entries[0].Intent != "Original request.\n" || len(journal.Requirements) != 1 {
		t.Fatalf("journal = %+v, error = %v", journal, err)
	}
	if _, err := prepared.Commit(t.Context()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("repeated publication: %v", err)
	}
}

func TestPreparedIntentCaptureRejectsCandidateChangesBeforeMutation(t *testing.T) {
	t.Parallel()
	repo, _ := newFeatureTaskBaseRepository(t)
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Original request.\n")
	prepared, err := PrepareIntentCapture(t.Context(), repo, CaptureIntentOptions{IntentPath: intent})
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorFile(t, repo.Root, "app.txt", "changed after preparation\n")
	if _, err := prepared.Commit(t.Context()); !errors.Is(err, ErrCandidateChanged) {
		t.Fatalf("changed candidate publication: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo.Root, ".code-polishy-reports")); !os.IsNotExist(err) {
		t.Fatalf("failed publication created artifacts: %v", err)
	}
}

func TestPreparedIntentCapturePreservesConcurrentJournalAppends(t *testing.T) {
	t.Parallel()
	repo, _ := newFeatureTaskBaseRepository(t)
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Original request.\n")
	prepared, err := PrepareIntentCapture(t.Context(), repo, CaptureIntentOptions{IntentPath: intent})
	if err != nil {
		t.Fatal(err)
	}
	other := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Another capture.\n")
	captured, err := CaptureIntent(t.Context(), repo, CaptureIntentOptions{IntentPath: other})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo.Root, captured.JournalPath)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Commit(t.Context()); !errors.Is(err, ErrCandidateChanged) {
		t.Fatalf("stale prepared journal overwrote another capture: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !slices.Equal(before, after) {
		t.Fatalf("stale publication changed the journal: %v", err)
	}
}

func TestPreparedIntentCaptureCancellationCreatesNoArtifacts(t *testing.T) {
	t.Parallel()
	repo, _ := newFeatureTaskBaseRepository(t)
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Original request.\n")
	prepared, err := PrepareIntentCapture(t.Context(), repo, CaptureIntentOptions{IntentPath: intent})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := prepared.Commit(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publication: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo.Root, ".code-polishy-reports")); !os.IsNotExist(err) {
		t.Fatalf("canceled publication created artifacts: %v", err)
	}
}
