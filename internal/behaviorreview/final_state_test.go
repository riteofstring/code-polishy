package behaviorreview

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/finalstate"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestFinalizeBlocksEvidenceBoundFinalStateFinding(t *testing.T) {
	t.Parallel()
	repo, _, _ := newBehaviorRepository(t)
	prepared := prepareReview(t, repo)
	packet, _, err := readPacket(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	path, hunk := firstCitableFinalStateHunk(t, packet)
	finding := finalstate.Finding{
		Kind: finalstate.KindCorrectionResidue, Path: path.Path, Line: hunk.Context.StartLine,
		PatchHunkSHA256: hunk.SHA256, IntentIDs: []string{packet.Intents[0].ID},
		Summary: "The rejected approach remains as a guard instead of being removed at its source.",
	}
	writeReviewResult(t, repo, prepared, ReviewResult{
		Version: artifactVersion, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: prepared.Candidate, IntentSHA256: prepared.IntentSHA256,
		Behaviors: []Behavior{{Before: "old", After: "new", Classification: "preserved", ProofIDs: []string{}}},
		Findings:  []string{}, FinalStateFindings: []finalstate.Finding{finding},
	})
	_, err = Finalize(context.Background(), repo, FinalizeOptions{Base: "main"})
	if !errors.Is(err, ErrInvalidReview) || !strings.Contains(err.Error(), path.Path) || !strings.Contains(err.Error(), "correction-residue") {
		t.Fatalf("Finalize() error = %v", err)
	}
}

func TestFinalizeRejectsInventedFinalStateEvidence(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*finalstate.Finding){
		"path":   func(finding *finalstate.Finding) { finding.Path = "invented.go" },
		"line":   func(finding *finalstate.Finding) { finding.Line = 99999 },
		"hunk":   func(finding *finalstate.Finding) { finding.PatchHunkSHA256 = strings.Repeat("0", 64) },
		"intent": func(finding *finalstate.Finding) { finding.IntentIDs = []string{"intent-invented"} },
	} {
		t.Run(name, func(t *testing.T) {
			repo, _, _ := newBehaviorRepository(t)
			prepared := prepareReview(t, repo)
			packet, _, err := readPacket(behaviorArtifact(t, repo))
			if err != nil {
				t.Fatal(err)
			}
			path, hunk := firstCitableFinalStateHunk(t, packet)
			finding := finalstate.Finding{
				Kind: finalstate.KindMetaNote, Path: path.Path, Line: hunk.Context.StartLine,
				PatchHunkSHA256: hunk.SHA256, IntentIDs: []string{}, Summary: "This text narrates the editing task.",
			}
			mutate(&finding)
			writeReviewResult(t, repo, prepared, ReviewResult{
				Version: artifactVersion, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: prepared.Candidate, IntentSHA256: prepared.IntentSHA256,
				Behaviors: []Behavior{{Before: "old", After: "new", Classification: "preserved", ProofIDs: []string{}}},
				Findings:  []string{}, FinalStateFindings: []finalstate.Finding{finding},
			})
			if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); !errors.Is(err, ErrInvalidReview) || !strings.Contains(err.Error(), "final-state findings are invalid") {
				t.Fatalf("Finalize() error = %v", err)
			}
		})
	}
}

func TestPreparedPacketCarriesTypedFinalStateEvidence(t *testing.T) {
	t.Parallel()
	repo, _, _ := newBehaviorRepository(t)
	prepareReview(t, repo)
	packet, _, err := readPacket(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.FinalState.Paths) == 0 || !validSHA256(packet.FinalState.SHA256) {
		t.Fatalf("final-state evidence = %+v", packet.FinalState)
	}
	for _, path := range packet.FinalState.Paths {
		if path.Role == "" || path.Hunks == nil {
			t.Fatalf("path evidence = %+v", path)
		}
	}
}

func TestFinalStatePathRoleUsesDeclaredPolicyBeforeFileShape(t *testing.T) {
	t.Parallel()
	repo := repository.Repository{Config: policy.Config{
		Scope:         policy.Scope{Generated: []string{"generated/**"}},
		Documentation: policy.Documentation{ProductInputs: []string{"docs/product.md"}},
	}}
	for path, want := range map[string]finalstate.PathRole{
		"generated/api.md":         finalstate.RoleGenerated,
		"docs/product.md":          finalstate.RoleProductInput,
		"internal/testdata/a.txt":  finalstate.RoleFixture,
		"internal/value_test.go":   finalstate.RoleTest,
		"docs/plans/change.md":     finalstate.RolePlan,
		"CHANGELOG.md":             finalstate.RoleChangelog,
		"docs/current-state.md":    finalstate.RoleDocumentation,
		"internal/domain/value.go": finalstate.RoleProductionSource,
	} {
		t.Run(path, func(t *testing.T) {
			if got := finalStatePathRole(repo, path); got != want {
				t.Fatalf("finalStatePathRole(%q) = %q, want %q", path, got, want)
			}
		})
	}
}

func firstCitableFinalStateHunk(t *testing.T, packet reviewPacket) (finalstate.PathEvidence, finalstate.HunkEvidence) {
	t.Helper()
	for _, path := range packet.FinalState.Paths {
		for _, hunk := range path.Hunks {
			if hunk.Context.Available {
				return path, hunk
			}
		}
	}
	t.Fatal("packet has no citable final-state hunk")
	return finalstate.PathEvidence{}, finalstate.HunkEvidence{}
}
