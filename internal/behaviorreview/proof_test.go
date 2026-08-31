package behaviorreview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestProveCreatesRedGreenEvidenceAndCleansWorktree(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	prepared := prepareReview(t, repo)
	commandRunner := &behaviorRunner{candidateRoot: repo.Root}
	result, err := Prove(context.Background(), repo, commandRunner, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "proof-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Base != base || result.Candidate != candidate || result.ProofPath != artifactDisplayPath(proofArtifactName("proof-1", ".json")) {
		t.Fatalf("proof result = %+v", result)
	}
	root := behaviorRoot(t, repo)
	proof, _, err := readProof(behaviorArtifact(t, repo), "proof-1")
	if err != nil {
		t.Fatal(err)
	}
	assertBoundProof(t, proof, prepared)
	for _, name := range []string{"proof-1.apply.log", "proof-1.baseline.log", "proof-1.candidate.log", "proof-1.json"} {
		if _, err := os.Stat(artifactPath(root, filepath.ToSlash(filepath.Join(proofDirectory, name)))); err != nil {
			t.Fatalf("artifact %s: %v", name, err)
		}
	}
	if got, err := repo.CleanHead(); err != nil || got != candidate {
		t.Fatalf("candidate after proof = %q, %v", got, err)
	}
	if records := gitBehavior(t, repo.Root, "worktree", "list", "--porcelain"); strings.Count(records, "worktree ") != 1 {
		t.Fatalf("worktree records after proof = %q", records)
	}
	if len(commandRunner.roots) < 2 || !strings.HasPrefix(commandRunner.roots[0], artifactPath(root, worktreeDirectory)+string(filepath.Separator)) {
		t.Fatalf("proof roots = %v, want baseline under %s", commandRunner.roots, artifactPath(root, worktreeDirectory))
	}
}

func TestProveAllowsAnExistingReproducerWithAnEmptyEvidencePatch(t *testing.T) {
	repo, _, _ := newExistingEvidenceRepository(t)
	prepareReview(t, repo)
	result, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "existing"})
	if err != nil {
		t.Fatal(err)
	}
	proof, _, err := readProof(behaviorArtifact(t, repo), result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proof.EvidencePatchSHA256 != sha256Hex(nil) || proof.Baseline.ExitStatus != 1 || proof.CandidateExecution.ExitStatus != 0 {
		t.Fatalf("proof = %+v", proof)
	}
}

func TestProveRequiresPreparedReviewAndRejectsOlderReviewBase(t *testing.T) {
	t.Run("missing prepare", func(t *testing.T) {
		repo, _, _ := newBehaviorRepository(t)
		_, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "missing-prepare"})
		if !errors.Is(err, ErrInvalidReview) {
			t.Fatalf("Prove() error = %v, want invalid review", err)
		}
	})
	t.Run("older base", func(t *testing.T) {
		repo, _, _ := newBehaviorRepository(t)
		prepareReview(t, repo)
		_, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{Base: "main~1", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "older-base"})
		if !errors.Is(err, ErrInvalidEvidence) {
			t.Fatalf("Prove() error = %v, want invalid evidence", err)
		}
	})
}

func TestProveRejectsDestructiveSuite(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	prepareReview(t, repo)
	repo.Config.Tests.Suites[0].Kind = "destructive"
	_, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "destructive"})
	if !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("Prove() error = %v, want invalid evidence", err)
	}
}

func TestProveChecksCandidateAfterTimeout(t *testing.T) {
	t.Run("operational timeout", func(t *testing.T) {
		repo, _, _ := newBehaviorRepository(t)
		prepareReview(t, repo)
		_, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root, timeoutCandidate: true}, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "timeout"})
		if !errors.Is(err, ErrOperational) {
			t.Fatalf("Prove() error = %v, want operational failure", err)
		}
	})
	t.Run("candidate changed", func(t *testing.T) {
		repo, _, _ := newBehaviorRepository(t)
		prepareReview(t, repo)
		_, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root, mutateCandidate: true, timeoutCandidate: true}, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "timeout-mutation"})
		if !errors.Is(err, ErrCandidateChanged) {
			t.Fatalf("Prove() error = %v, want changed candidate", err)
		}
		if _, statErr := os.Stat(artifactPath(behaviorRoot(t, repo), proofArtifactName("timeout-mutation", ".json"))); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("successful proof artifact after mutation = %v, want missing", statErr)
		}
	})
}

func TestProveReturnsCleanupFailure(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	prepareReview(t, repo)
	commandRunner := &behaviorRunner{candidateRoot: repo.Root, attachBaselineWorktree: true}
	_, err := Prove(context.Background(), repo, commandRunner, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "cleanup"})
	if !errors.Is(err, ErrCleanup) {
		t.Fatalf("Prove() error = %v, want cleanup failure", err)
	}
	if len(commandRunner.roots) == 0 {
		t.Fatal("runner did not receive the baseline worktree")
	}
	cleanupAttachedWorktree(t, repo, commandRunner.roots[0])
}

func TestFinalizeRevalidatesProofArtifactsAndConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, repo repository.Repository, root string, proof regressionProof)
	}{
		{
			name: "log digest", mutate: func(t *testing.T, _ repository.Repository, root string, proof regressionProof) {
				writeBehaviorFile(t, root, strings.TrimPrefix(proof.Baseline.LogPath, artifactDirectory+"/"), "tampered\n")
			},
		},
		{
			name: "patch digest", mutate: func(t *testing.T, _ repository.Repository, root string, proof regressionProof) {
				proof.EvidencePatchSHA256 = strings.Repeat("0", 64)
				writeProofRecord(t, root, proof)
			},
		},
		{
			name: "suite configuration", mutate: func(_ *testing.T, repo repository.Repository, _ string, _ regressionProof) {
				repo.Config.Tests.Suites[0].RunOn = []string{"supplemental"}
			},
		},
		{
			name: "review binding", mutate: func(t *testing.T, _ repository.Repository, root string, proof regressionProof) {
				proof.ReviewID = "review-other"
				writeProofRecord(t, root, proof)
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repo, _, _ := newBehaviorRepository(t)
			prepared := prepareReview(t, repo)
			proofResult := proveReview(t, repo, "proof-1", 1)
			root := behaviorRoot(t, repo)
			proof, _, err := readProof(behaviorArtifact(t, repo), proofResult.ID)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(t, repo, root, proof)
			writeRequestedResult(t, repo, prepared, proofResult.ID)
			_, err = Finalize(context.Background(), repo, FinalizeOptions{Base: "main"})
			if !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("Finalize() error = %v, want invalid evidence", err)
			}
		})
	}
}

func TestReplayGateReceiptReexecutesProofsWithoutChangingArtifacts(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	prepared := prepareReview(t, repo)
	proof := proveReview(t, repo, "proof-1", 1)
	writeRequestedResult(t, repo, prepared, proof.ID)
	if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); err != nil {
		t.Fatal(err)
	}
	root := behaviorRoot(t, repo)
	beforeReceipt := readBehaviorArtifact(t, root, receiptFilename)
	beforeProof := readBehaviorArtifact(t, root, proofArtifactName(proof.ID, ".json"))
	beforeLog := readBehaviorArtifact(t, root, proofArtifactName(proof.ID, ".candidate.log"))
	receipt, err := ReplayGateReceipt(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, validationOptions("main"))
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Proofs) != 1 || receipt.Proofs[0].ID != proof.ID {
		t.Fatalf("replay receipt = %+v", receipt)
	}
	if string(beforeReceipt) != string(readBehaviorArtifact(t, root, receiptFilename)) || string(beforeProof) != string(readBehaviorArtifact(t, root, proofArtifactName(proof.ID, ".json"))) || string(beforeLog) != string(readBehaviorArtifact(t, root, proofArtifactName(proof.ID, ".candidate.log"))) {
		t.Fatal("replay changed recorded proof artifacts")
	}
}

func TestReplayPlanReturnsStableClonedValidatedProofPhasesWithoutExecution(t *testing.T) {
	repo, proof := finalizedRequestedReview(t)
	before := gitBehavior(t, repo.Root, "worktree", "list", "--porcelain")
	plan := replayPlanForTest(t, repo)
	assertReplayPlanReceiptBindings(t, plan, readReplayPlanReceipt(t, repo))
	assertReplayPlanProofPhase(t, repo, plan, proof)
	mutateReplayPlanBaseline(t, &plan)
	assertReplayPlanDeterminism(t, repo, plan)
	assertReplayPlanWorktreesUnchanged(t, repo, before)
}

func replayPlanForTest(t *testing.T, repo repository.Repository) GateReplayPlan {
	t.Helper()
	plan, err := ReplayPlan(context.Background(), repo, validationOptions("main"))
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func readReplayPlanReceipt(t *testing.T, repo repository.Repository) GateReceipt {
	t.Helper()
	receipt, err := readReceipt(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func assertReplayPlanReceiptBindings(t *testing.T, plan GateReplayPlan, receipt GateReceipt) {
	t.Helper()
	if plan.Receipt.SelectionSHA256 != receipt.SelectionSHA256 || plan.Receipt.RequirementSHA256 != receipt.RequirementSHA256 ||
		plan.Receipt.DecisionSHA256 != receipt.DecisionSHA256 || !sameReviewSelection(plan.Receipt.Selection, receipt.Selection) {
		t.Fatalf("replay receipt bindings = %+v, stored = %+v", plan.Receipt, receipt)
	}
}

func assertReplayPlanProofPhase(t *testing.T, repo repository.Repository, plan GateReplayPlan, proof ProveResult) {
	t.Helper()
	if len(plan.Proofs) != 1 {
		t.Fatalf("replay phases = %+v", plan.Proofs)
	}
	phase := plan.Proofs[0]
	wantCommand := suiteCommand(repo.Config.Tests.Suites[0])
	if phase.ProofID != proof.ID || phase.ProofSHA256 != plan.Receipt.Proofs[0].SHA256 || !reflect.DeepEqual(phase.Baseline, wantCommand) || !reflect.DeepEqual(phase.Candidate, wantCommand) {
		t.Fatalf("replay phase = %+v, want proof %q and command %+v", phase, proof.ID, wantCommand)
	}
}

func mutateReplayPlanBaseline(t *testing.T, plan *GateReplayPlan) {
	t.Helper()
	plan.Proofs[0].Baseline.Argv[0] = "changed"
	if plan.Proofs[0].Candidate.Argv[0] == "changed" {
		t.Fatal("baseline and candidate commands share mutable data")
	}
}

func assertReplayPlanDeterminism(t *testing.T, repo repository.Repository, mutated GateReplayPlan) {
	t.Helper()
	first := replayPlanJSON(t, mutated)
	again := replayPlanForTest(t, repo)
	second := replayPlanJSON(t, again)
	if string(first) == string(second) {
		t.Fatal("mutating a returned plan altered its deterministic replay representation")
	}
	clean := replayPlanForTest(t, repo)
	if string(second) != string(replayPlanJSON(t, clean)) {
		t.Fatal("replay plan is not deterministic")
	}
}

func replayPlanJSON(t *testing.T, plan GateReplayPlan) []byte {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertReplayPlanWorktreesUnchanged(t *testing.T, repo repository.Repository, before string) {
	t.Helper()
	if after := gitBehavior(t, repo.Root, "worktree", "list", "--porcelain"); after != before {
		t.Fatalf("ReplayPlan created or removed a worktree:\n before=%q\n after=%q", before, after)
	}
}

func TestReplayPlanRejectsTamperedProofArtifacts(t *testing.T) {
	repo, proof := finalizedRequestedReview(t)
	root := behaviorRoot(t, repo)
	writeBehaviorFile(t, root, proofArtifactName(proof.ID, ".candidate.log"), "tampered\n")
	if _, err := ReplayPlan(context.Background(), repo, validationOptions("main")); !errors.Is(err, ErrStaleReceipt) {
		t.Fatalf("ReplayPlan() error = %v, want stale receipt", err)
	}
}

func TestReplayGateReceiptRejectsSemanticFailureAndCandidateWorktreeMutation(t *testing.T) {
	t.Run("semantic failure", func(t *testing.T) {
		repo, _ := finalizedRequestedReview(t)
		_, err := ReplayGateReceipt(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root, baselineStatus: 2}, validationOptions("main"))
		if !errors.Is(err, ErrInvalidEvidence) {
			t.Fatalf("ReplayGateReceipt() error = %v, want invalid evidence", err)
		}
	})
	t.Run("candidate worktree mutation", func(t *testing.T) {
		repo, _ := finalizedRequestedReview(t)
		_, err := ReplayGateReceipt(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root, mutateReplayCandidate: true}, validationOptions("main"))
		if !errors.Is(err, ErrCandidateChanged) {
			t.Fatalf("ReplayGateReceipt() error = %v, want changed candidate", err)
		}
		if _, err := repo.CleanHead(); err != nil {
			t.Fatalf("primary candidate changed during replay: %v", err)
		}
	})
}

func TestProveRejectsWrongRedStatusAndUnsafeEvidence(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	prepareReview(t, repo)
	wrongStatusRunner := &behaviorRunner{candidateRoot: repo.Root, baselineStatus: 2}
	_, err := Prove(context.Background(), repo, wrongStatusRunner, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "wrong"})
	if !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("wrong red status error = %v, want invalid evidence", err)
	}
	_, err = Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"app.txt"}, ID: "production"})
	if !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("production evidence error = %v, want invalid evidence", err)
	}
	repo.Config.Tests.Suites[0].RunOn = []string{"supplemental"}
	_, err = Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "supplemental"})
	if !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("supplemental suite error = %v, want invalid evidence", err)
	}
}

func TestProveDetectsCandidateMutation(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	prepareReview(t, repo)
	commandRunner := &behaviorRunner{candidateRoot: repo.Root, mutateCandidate: true}
	_, err := Prove(context.Background(), repo, commandRunner, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "mutation"})
	if !errors.Is(err, ErrCandidateChanged) {
		t.Fatalf("candidate mutation error = %v, want changed candidate", err)
	}
}
