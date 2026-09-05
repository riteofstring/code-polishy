package behaviorreview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestExplicitFeatureAliasesPersistOnlyCanonicalRequirements(t *testing.T) {
	t.Parallel()
	repo, base := newFeatureTaskBaseRepository(t)
	repo.Config.Verification.BehaviorReview.Features[0].Aliases = []string{"purchase completion"}
	repo.Config.Verification.BehaviorReview.Features[1].Aliases = []string{"Straße"}
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Review the requested behavior.\n")
	captured, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{
		IntentPath: intent, Features: []string{"STRASSE", "SEARCH"},
	})
	if err != nil || !slices.Equal(captured.Features, []string{"search"}) {
		t.Fatalf("capture = %+v, error = %v", captured, err)
	}
	commitFeatureCandidate(t, repo)
	required, err := Require(context.Background(), repo, RequireOptions{
		Base: base, Features: []string{" PURCHASE\u00a0COMPLETION ", "checkout"},
	})
	if err != nil || !slices.Equal(required.Features, []string{"checkout"}) {
		t.Fatalf("require = %+v, error = %v", required, err)
	}
	snapshot, err := TaskRequirements(context.Background(), repo, base)
	if err != nil || !slices.Equal(snapshot.RequestedFeatures, []string{"checkout", "search"}) || len(snapshot.Requirements) != 2 {
		t.Fatalf("requirements = %+v, error = %v", snapshot, err)
	}
	if !slices.Equal(snapshot.Requirements[0].Features, []string{"search"}) || !slices.Equal(snapshot.Requirements[1].Features, []string{"checkout"}) {
		t.Fatalf("persisted requirements = %+v", snapshot.Requirements)
	}
}

func TestIntentKeywordsDoNotActivateFeaturesAndUnknownOperandsDoNotAppend(t *testing.T) {
	t.Parallel()
	repo, base := newFeatureTaskBaseRepository(t)
	repo.Config.Verification.BehaviorReview.Features[0].Aliases = []string{"purchase completion"}
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Explain checkout, search, and purchase completion.\n")
	captured, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent})
	if err != nil || len(captured.Features) != 0 || captured.RequirementID != "" {
		t.Fatalf("capture = %+v, error = %v", captured, err)
	}
	journalPath := filepath.Join(repo.Root, captured.JournalPath)
	before, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent, Features: []string{"purchase"}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("inexact feature error = %v", err)
	}
	if _, err := Require(context.Background(), repo, RequireOptions{Base: base, Features: []string{"purchase completion now"}}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("inexact requirement error = %v", err)
	}
	after, err := os.ReadFile(journalPath)
	if err != nil || string(before) != string(after) {
		t.Fatalf("invalid operand changed the journal: %v", err)
	}
	snapshot, err := TaskRequirements(context.Background(), repo, base)
	if err != nil || len(snapshot.RequestedFeatures) != 0 || len(snapshot.Requirements) != 0 {
		t.Fatalf("requirements = %+v, error = %v", snapshot, err)
	}
}
