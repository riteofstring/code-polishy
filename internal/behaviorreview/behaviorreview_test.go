package behaviorreview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPrepareWritesPacketBoundToCleanCommittedCandidate(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	prepared, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Base != base || prepared.Candidate != candidate || !strings.HasPrefix(prepared.ReviewID, "review-") || prepared.PacketPath != artifactDisplayPath(packetFilename) {
		t.Fatalf("prepare result = %+v", prepared)
	}
	packet, data, err := readPacket(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if packet.ReviewID != prepared.ReviewID || len(packet.Intents) != 1 || packet.Intents[0].Intent != "Set the application value to new.\n" ||
		packet.Intents[0].CapturedAtCommit != base || packet.ResultPath != artifactDisplayPath(defaultResultFilename) ||
		packet.ProofDirectory != artifactDisplayPath(proofDirectory) || packet.PatchSHA256 == "" || !strings.Contains(packet.Patch, "diff --git") || len(data) == 0 {
		t.Fatalf("packet = %+v", packet)
	}
	if got, err := repo.CleanHead(); err != nil || got != candidate {
		t.Fatalf("candidate after prepare = %q, %v", got, err)
	}
}

func TestCaptureIntentRejectsUnsafeIntentAndDirtyCandidate(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	cases := []struct {
		name    string
		prepare func(t *testing.T) string
		want    error
	}{
		{
			name: "empty", prepare: func(t *testing.T) string {
				return writeBehaviorFile(t, t.TempDir(), "empty.txt", " \n")
			}, want: ErrInvalidInput,
		},
		{
			name: "invalid utf8", prepare: func(t *testing.T) string {
				return writeBehaviorBytes(t, t.TempDir(), "invalid.txt", []byte{0xff})
			}, want: ErrInvalidInput,
		},
		{
			name: "too large", prepare: func(t *testing.T) string {
				return writeBehaviorFile(t, t.TempDir(), "large.txt", strings.Repeat("x", maximumIntentBytes+1))
			}, want: ErrInvalidInput,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := test.prepare(t)
			_, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: path})
			if !errors.Is(err, test.want) {
				t.Fatalf("Prepare() error = %v, want %v", err, test.want)
			}
			removeBehaviorFile(t, path)
		})
	}
	intent := writeBehaviorFile(t, t.TempDir(), "request.txt", "request\n")
	writeBehaviorFile(t, repo.Root, "dirty.txt", "dirty\n")
	_, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent})
	if !errors.Is(err, repository.ErrDirtyCandidate) {
		t.Fatalf("dirty Prepare() error = %v, want dirty candidate", err)
	}
}

func TestPrepareSelectsOneTaskOrTheWholeIntentChain(t *testing.T) {
	repo, base, firstCandidate := newBehaviorRepository(t)
	second := captureReviewIntent(t, repo, "Also write the selected value to a second file.\n")
	if second.Commit != firstCandidate {
		t.Fatalf("second capture commit = %s, want %s", second.Commit, firstCandidate)
	}
	writeBehaviorFile(t, repo.Root, "second.txt", "new\n")
	gitBehavior(t, repo.Root, "add", "second.txt")
	gitBehavior(t, repo.Root, "commit", "-m", "second task")
	secondCandidate := strings.TrimSpace(gitBehavior(t, repo.Root, "rev-parse", "HEAD"))

	task, err := Prepare(context.Background(), repo, PrepareOptions{Base: firstCandidate})
	if err != nil {
		t.Fatal(err)
	}
	taskPacket, _, err := readPacket(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if task.Base != firstCandidate || task.Candidate != secondCandidate || len(taskPacket.Intents) != 1 ||
		taskPacket.Intents[0].ID != second.ID || taskPacket.Intents[0].Intent != "Also write the selected value to a second file.\n" {
		t.Fatalf("task packet = %+v", taskPacket)
	}

	merge, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	mergePacket, _, err := readPacket(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	if merge.Base != base || merge.Candidate != secondCandidate || len(mergePacket.Intents) != 2 ||
		mergePacket.Intents[1].ID != second.ID || mergePacket.IntentSHA256 == taskPacket.IntentSHA256 {
		t.Fatalf("merge packet = %+v", mergePacket)
	}
}

func TestPrepareRejectsMissingOrAfterTheFactIntent(t *testing.T) {
	repo, _, candidate := newBehaviorRepository(t)
	root := filepath.Join(repo.Root, filepath.FromSlash(artifactDirectory))
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main"}); !errors.Is(err, ErrInvalidInput) ||
		!strings.Contains(err.Error(), "capture the original request") {
		t.Fatalf("missing intent Prepare() error = %v", err)
	}
	late := captureReviewIntent(t, repo, "Retell the request after implementation.\n")
	if late.Commit != candidate {
		t.Fatalf("late capture commit = %s, want %s", late.Commit, candidate)
	}
	if _, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main"}); !errors.Is(err, ErrInvalidInput) ||
		!strings.Contains(err.Error(), "review base before implementation") {
		t.Fatalf("late intent Prepare() error = %v", err)
	}

	repo, _, candidate = newBehaviorRepository(t)
	late = captureReviewIntent(t, repo, "Add an inaccurate summary after implementation.\n")
	if late.Commit != candidate {
		t.Fatalf("second late capture commit = %s, want %s", late.Commit, candidate)
	}
	if _, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main"}); !errors.Is(err, ErrInvalidInput) ||
		!strings.Contains(err.Error(), "intent must be captured before implementation") {
		t.Fatalf("after-the-fact intent Prepare() error = %v", err)
	}
}

func TestCaptureIntentRejectsStagedUnstagedAndUntrackedChanges(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, repository.Repository)
	}{
		{name: "staged", change: func(t *testing.T, repo repository.Repository) {
			writeBehaviorFile(t, repo.Root, "app.txt", "staged\n")
			gitBehavior(t, repo.Root, "add", "app.txt")
		}},
		{name: "unstaged", change: func(t *testing.T, repo repository.Repository) {
			writeBehaviorFile(t, repo.Root, "app.txt", "unstaged\n")
		}},
		{name: "untracked", change: func(t *testing.T, repo repository.Repository) {
			writeBehaviorFile(t, repo.Root, "untracked.txt", "untracked\n")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, _, _ := newBehaviorRepository(t)
			test.change(t, repo)
			intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Next request.\n")
			if _, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent}); !errors.Is(err, repository.ErrDirtyCandidate) {
				t.Fatalf("CaptureIntent() error = %v, want dirty candidate", err)
			}
		})
	}
}

func TestPrepareRejectsEditedRemovedAndReorderedIntentEntries(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*intentJournal)
	}{
		{name: "edited", mutate: func(journal *intentJournal) { journal.Entries[0].Intent = "edited\n" }},
		{name: "removed", mutate: func(journal *intentJournal) { journal.Entries = journal.Entries[1:] }},
		{name: "reordered", mutate: func(journal *intentJournal) {
			journal.Entries[0], journal.Entries[1] = journal.Entries[1], journal.Entries[0]
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, _, _ := newBehaviorRepository(t)
			captureReviewIntent(t, repo, "Second request.\n")
			writeBehaviorFile(t, repo.Root, "second.txt", "second\n")
			gitBehavior(t, repo.Root, "add", "second.txt")
			gitBehavior(t, repo.Root, "commit", "-m", "second task")
			root := behaviorArtifact(t, repo)
			journal, err := readIntentJournal(repo, root, false)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&journal)
			data, err := marshalArtifact(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := root.writeArtifactAtomic(intentJournalFilename, data); err != nil {
				t.Fatal(err)
			}
			if _, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main"}); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Prepare() error = %v, want invalid input", err)
			}
		})
	}
}

func TestCaptureRejectsAJournalFromADivergedBranch(t *testing.T) {
	repo, _, candidate := newBehaviorRepository(t)
	captureReviewIntent(t, repo, "Continue the feature.\n")
	gitBehavior(t, repo.Root, "switch", "main")
	gitBehavior(t, repo.Root, "switch", "-c", "other")
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Unrelated request.\n")
	_, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent})
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "one history") {
		t.Fatalf("CaptureIntent() error = %v, want diverged journal rejection from %s", err, candidate)
	}
}

func TestFinalizeAndValidateGateReceipt(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	prepared := prepareReview(t, repo)
	proof := proveReview(t, repo, "proof-1", 1)
	writeReviewResult(t, repo, prepared, ReviewResult{
		Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256,
		Behaviors: []Behavior{{Before: "old value", After: "new value", Classification: "requested", ProofIDs: []string{proof.ID}}}, Findings: []string{},
	})
	finalized, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.ReceiptPath != artifactDisplayPath(receiptFilename) || finalized.ReviewID != prepared.ReviewID {
		t.Fatalf("finalize result = %+v", finalized)
	}
	receipt, err := ValidateGateReceipt(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Base != base || receipt.Candidate != candidate || len(receipt.Proofs) != 1 || receipt.Proofs[0].ID != proof.ID {
		t.Fatalf("receipt = %+v", receipt)
	}
}

func TestFinalizeRejectsStrictStaleAndBlockingResults(t *testing.T) {
	cases := []struct {
		name   string
		result func(PrepareResult, string, string) string
		want   error
	}{
		{
			name: "unknown field", result: func(prepared PrepareResult, base, candidate string) string {
				return `{"version":2,"review_id":"` + prepared.ReviewID + `","base":"` + base + `","candidate":"` + candidate + `","intent_sha256":"` + prepared.IntentSHA256 + `","behaviors":[],"findings":[],"extra":true}`
			}, want: ErrInvalidReview,
		},
		{
			name: "stale candidate", result: func(prepared PrepareResult, base, candidate string) string {
				return reviewJSON(ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: strings.Repeat("0", len(candidate)), IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{{Before: "old", After: "new", Classification: "preserved", ProofIDs: []string{}}}, Findings: []string{}})
			}, want: ErrStaleReview,
		},
		{
			name: "unintended", result: func(prepared PrepareResult, base, candidate string) string {
				return reviewJSON(ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{{Before: "old", After: "new", Classification: "unintended", ProofIDs: []string{}}}, Findings: []string{}})
			}, want: ErrInvalidReview,
		},
		{
			name: "finding", result: func(prepared PrepareResult, base, candidate string) string {
				return reviewJSON(ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{}, Findings: []string{"ambiguous outcome"}})
			}, want: ErrInvalidReview,
		},
		{
			name: "empty behaviors", result: func(prepared PrepareResult, base, candidate string) string {
				return reviewJSON(ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{}, Findings: []string{}})
			}, want: ErrInvalidReview,
		},
		{
			name: "missing proof", result: func(prepared PrepareResult, base, candidate string) string {
				return reviewJSON(ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{{Before: "old", After: "new", Classification: "requested", ProofIDs: []string{"absent"}}}, Findings: []string{}})
			}, want: ErrInvalidEvidence,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repo, base, candidate := newBehaviorRepository(t)
			prepared := prepareReview(t, repo)
			writeBehaviorFile(t, behaviorRoot(t, repo), defaultResultFilename, test.result(prepared, base, candidate))
			_, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"})
			if !errors.Is(err, test.want) {
				t.Fatalf("Finalize() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestValidateGateReceiptRejectsPacketAndCandidateChanges(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	prepared := prepareReview(t, repo)
	proof := proveReview(t, repo, "proof-1", 1)
	writeRequestedResult(t, repo, prepared, proof.ID)
	if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); err != nil {
		t.Fatal(err)
	}
	root := behaviorRoot(t, repo)
	writeBehaviorFile(t, root, packetFilename, "{}\n")
	if _, err := ValidateGateReceipt(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"}); !errors.Is(err, ErrStaleReceipt) {
		t.Fatalf("packet change validation error = %v, want stale receipt", err)
	}
	prepareReview(t, repo)
	writeBehaviorFile(t, repo.Root, "app.txt", "changed outside commit\n")
	if _, err := ValidateGateReceipt(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"}); !errors.Is(err, repository.ErrDirtyCandidate) {
		t.Fatalf("candidate change validation error = %v, want dirty candidate", err)
	}
}

func TestValidateGateReceiptMissingReceiptDoesNotCreateArtifacts(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	path := filepath.Join(repo.Root, filepath.FromSlash(artifactDirectory))
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact root before validation = %v, want missing", err)
	}
	_, err := ValidateGateReceipt(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"})
	if !errors.Is(err, ErrMissingReceipt) {
		t.Fatalf("ValidateGateReceipt() error = %v, want missing receipt", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact root after validation = %v, want missing", err)
	}
}

func TestFinalizeAndReceiptRejectRehashedPacketMaterial(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*reviewPacket)
	}{
		{name: "patch", mutate: func(packet *reviewPacket) { packet.Patch += "tampered patch\n" }},
		{name: "instructions", mutate: func(packet *reviewPacket) { packet.Instructions = "tampered instructions\n" }},
		{name: "design documents", mutate: func(packet *reviewPacket) { packet.DesignDocuments[0].Content = "tampered design\n" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assertFinalizeRejectsRehashedPacket(t, test.mutate)
			assertReceiptRejectsRehashedPacket(t, test.mutate)
		})
	}
}

func TestPrepareMarkerRejectsRehashedIntent(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	prepareReview(t, repo)
	root := behaviorRoot(t, repo)
	packet, _, err := readPacket(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	packet.Intents[0].Intent = "Different requested change.\n"
	refreshPacketDigests(&packet)
	data, err := marshalArtifact(packet)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorBytes(t, root, packetFilename, data)
	if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); !errors.Is(err, ErrStaleReview) {
		t.Fatalf("Finalize() error = %v, want stale review", err)
	}
}

func TestValidateGateReceiptBindsPrepareMarker(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	prepared, receipt := finalizedPreservedReview(t, repo)
	root := behaviorRoot(t, repo)
	packet, _, err := readPacket(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	packet.Intents[0].Intent = "Different requested change.\n"
	refreshPacketDigests(&packet)
	packetData, markerData := writePacketAndMarker(t, root, packet)
	if sha256Hex(packetData) == receipt.PacketSHA256 || sha256Hex(markerData) == receipt.PrepareSHA256 || packet.IntentSHA256 == prepared.IntentSHA256 {
		t.Fatal("tampered packet did not change receipt-bound values")
	}
	if _, err := ValidateGateReceipt(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"}); !errors.Is(err, ErrStaleReceipt) {
		t.Fatalf("ValidateGateReceipt() error = %v, want stale receipt", err)
	}
}

func assertFinalizeRejectsRehashedPacket(t *testing.T, mutate func(*reviewPacket)) {
	t.Helper()
	repo, _, _ := newBehaviorRepository(t)
	prepared := prepareReview(t, repo)
	writePreservedResult(t, repo, prepared)
	root := behaviorRoot(t, repo)
	packet, _, err := readPacket(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	mutate(&packet)
	refreshPacketDigests(&packet)
	writePacketAndMarker(t, root, packet)
	if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); !errors.Is(err, ErrStaleReview) {
		t.Fatalf("Finalize() error = %v, want stale review", err)
	}
}

func assertReceiptRejectsRehashedPacket(t *testing.T, mutate func(*reviewPacket)) {
	t.Helper()
	repo, _, _ := newBehaviorRepository(t)
	_, receipt := finalizedPreservedReview(t, repo)
	root := behaviorRoot(t, repo)
	packet, _, err := readPacket(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	mutate(&packet)
	refreshPacketDigests(&packet)
	packetData, markerData := writePacketAndMarker(t, root, packet)
	receipt.PacketSHA256 = sha256Hex(packetData)
	receipt.PrepareSHA256 = sha256Hex(markerData)
	writeReceiptRecord(t, root, receipt)
	if _, err := ValidateGateReceipt(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"}); !errors.Is(err, ErrStaleReceipt) {
		t.Fatalf("ValidateGateReceipt() error = %v, want stale receipt", err)
	}
}

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
	receipt, err := ReplayGateReceipt(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ValidateGateReceiptOptions{Base: "main"})
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
	plan, err := ReplayPlan(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Proofs) != 1 {
		t.Fatalf("replay phases = %+v", plan.Proofs)
	}
	phase := plan.Proofs[0]
	wantCommand := suiteCommand(repo.Config.Tests.Suites[0])
	if phase.ProofID != proof.ID || phase.ProofSHA256 != plan.Receipt.Proofs[0].SHA256 || !reflect.DeepEqual(phase.Baseline, wantCommand) || !reflect.DeepEqual(phase.Candidate, wantCommand) {
		t.Fatalf("replay phase = %+v, want proof %q and command %+v", phase, proof.ID, wantCommand)
	}
	phase.Baseline.Argv[0] = "changed"
	if phase.Candidate.Argv[0] == "changed" {
		t.Fatal("baseline and candidate commands share mutable data")
	}
	first, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ReplayPlan(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(again)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) == string(second) {
		t.Fatal("mutating a returned plan altered its deterministic replay representation")
	}
	clean, err := ReplayPlan(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	cleanData, err := json.Marshal(clean)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(cleanData) {
		t.Fatal("replay plan is not deterministic")
	}
	after := gitBehavior(t, repo.Root, "worktree", "list", "--porcelain")
	if after != before {
		t.Fatalf("ReplayPlan created or removed a worktree:\n before=%q\n after=%q", before, after)
	}
}

func TestReplayPlanRejectsTamperedProofArtifacts(t *testing.T) {
	repo, proof := finalizedRequestedReview(t)
	root := behaviorRoot(t, repo)
	writeBehaviorFile(t, root, proofArtifactName(proof.ID, ".candidate.log"), "tampered\n")
	if _, err := ReplayPlan(context.Background(), repo, ValidateGateReceiptOptions{Base: "main"}); !errors.Is(err, ErrStaleReceipt) {
		t.Fatalf("ReplayPlan() error = %v, want stale receipt", err)
	}
}

func TestReplayGateReceiptRejectsSemanticFailureAndCandidateWorktreeMutation(t *testing.T) {
	t.Run("semantic failure", func(t *testing.T) {
		repo, _ := finalizedRequestedReview(t)
		_, err := ReplayGateReceipt(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root, baselineStatus: 2}, ValidateGateReceiptOptions{Base: "main"})
		if !errors.Is(err, ErrInvalidEvidence) {
			t.Fatalf("ReplayGateReceipt() error = %v, want invalid evidence", err)
		}
	})
	t.Run("candidate worktree mutation", func(t *testing.T) {
		repo, _ := finalizedRequestedReview(t)
		_, err := ReplayGateReceipt(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root, mutateReplayCandidate: true}, ValidateGateReceiptOptions{Base: "main"})
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

func TestBehaviorReviewArtifactRootRejectsEscapingSymlink(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	if err := os.MkdirAll(filepath.Join(repo.Root, ".code-polishy-reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo.Root, ".code-polishy-reports", "behavior-review")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	if _, err := behaviorReviewRoot(repo); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("artifact root error = %v, want invalid input", err)
	}
}

func TestArtifactHandleKeepsReadsAndWritesInsideHeldRootAfterSymlinkSwap(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.writeArtifactAtomic(packetFilename, []byte("original\n")); err != nil {
		t.Fatal(err)
	}
	held := root.path
	moved := held + ".held"
	if err := os.Rename(held, moved); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, held); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	data, err := root.readArtifact(packetFilename, maximumArtifactReadByte)
	if err != nil || string(data) != "original\n" {
		t.Fatalf("held artifact read = %q, %v", data, err)
	}
	if err := root.ensureDirectory(proofDirectory, "regression proof"); err != nil {
		t.Fatal(err)
	}
	if err := root.writeArtifactAtomic(proofArtifactName("proof-1", ".candidate.log"), []byte("held\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, proofDirectory, "proof-1.candidate.log")); err != nil {
		t.Fatalf("held artifact was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, proofDirectory, "proof-1.candidate.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink target received artifact: %v", err)
	}
}

func TestArtifactHandleRejectsSymlinkArtifactTarget(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	outside := filepath.Join(t.TempDir(), "receipt.json")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root.path, receiptFilename)); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	if _, err := root.readArtifact(receiptFilename, maximumArtifactReadByte); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("read symlink artifact error = %v, want invalid input", err)
	}
	if err := root.writeArtifactAtomic(receiptFilename, []byte("replacement\n")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("replace symlink artifact error = %v, want invalid input", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "outside\n" {
		t.Fatalf("outside target = %q, %v", data, err)
	}
}

func TestRecordCheckpointWritesCandidateBoundReceipt(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	result, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReviewID: "review-123", GateRun: checkpointGateEvidence(t, repo, base, candidate),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReceiptPath != CheckpointReceiptPath || result.Receipt.Base != base || result.Receipt.Candidate != candidate ||
		result.Receipt.Scope != CheckpointScopeChanged || result.Receipt.BehaviorReviewID != "review-123" {
		t.Fatalf("checkpoint result = %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(CheckpointReceiptPath)))
	if err != nil {
		t.Fatal(err)
	}
	var receipt CheckpointReceipt
	if err := decodeStrict(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt != result.Receipt {
		t.Fatalf("checkpoint receipt = %+v, want %+v", receipt, result.Receipt)
	}
}

func TestRecordCheckpointAcceptsDocumentationWithoutBehaviorReview(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	result, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeDocumentation, GateRun: checkpointGateEvidence(t, repo, base, candidate),
	})
	if err != nil || result.Receipt.BehaviorReviewID != "" {
		t.Fatalf("checkpoint result = %+v, error = %v", result, err)
	}
}

func TestReadCheckpointReturnsOnlyTheCurrentValidReceipt(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	written, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReviewID: "review-123", GateRun: checkpointGateEvidence(t, repo, base, candidate),
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := ReadCheckpoint(context.Background(), repo)
	if err != nil || read != written.Receipt {
		t.Fatalf("checkpoint=%+v error=%v want=%+v", read, err, written.Receipt)
	}
}

func TestReadCheckpointRejectsSymlinkReceiptWithoutFollowingIt(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	if _, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReviewID: "review-123", GateRun: checkpointGateEvidence(t, repo, base, candidate),
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo.Root, filepath.FromSlash(CheckpointReceiptPath))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "receipt.json")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	if _, err := ReadCheckpoint(context.Background(), repo); !errors.Is(err, ErrStaleCheckpoint) {
		t.Fatalf("ReadCheckpoint() error = %v, want stale checkpoint", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "outside\n" {
		t.Fatalf("outside target = %q, %v", data, err)
	}
}

func TestReadCheckpointMissingReceiptDoesNotCreateArtifacts(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	if _, err := ReadCheckpoint(context.Background(), repo); !errors.Is(err, ErrMissingCheckpoint) {
		t.Fatalf("ReadCheckpoint() error = %v, want missing checkpoint", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, filepath.FromSlash(checkpointDirectory))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing checkpoint read created artifacts: %v", err)
	}
}

func TestReadCheckpointRejectsMissingMalformedStaleAndDivergedReceipts(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*testing.T, repository.Repository, string, string)
		want    error
	}{
		{
			name:    "missing",
			prepare: func(_ *testing.T, _ repository.Repository, _ string, _ string) {},
			want:    ErrMissingCheckpoint,
		},
		{
			name: "malformed",
			prepare: func(t *testing.T, repo repository.Repository, _ string, _ string) {
				writeBehaviorFile(t, repo.Root, CheckpointReceiptPath, "{\"version\":1}\n")
			},
			want: ErrStaleCheckpoint,
		},
		{
			name: "stale candidate",
			prepare: func(t *testing.T, repo repository.Repository, base, candidate string) {
				if _, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
					Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReviewID: "review-123", GateRun: checkpointGateEvidence(t, repo, base, candidate),
				}); err != nil {
					t.Fatal(err)
				}
				gitBehavior(t, repo.Root, "commit", "--allow-empty", "-m", "next candidate")
			},
			want: ErrStaleCheckpoint,
		},
		{
			name: "diverged base",
			prepare: func(t *testing.T, repo repository.Repository, base, candidate string) {
				result, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
					Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReviewID: "review-123", GateRun: checkpointGateEvidence(t, repo, base, candidate),
				})
				if err != nil {
					t.Fatal(err)
				}
				unrelated := strings.TrimSpace(gitBehavior(t, repo.Root, "commit-tree", candidate+"^{tree}", "-m", "unrelated"))
				result.Receipt.Base = unrelated
				data, err := marshalArtifact(result.Receipt)
				if err != nil {
					t.Fatal(err)
				}
				writeBehaviorBytes(t, repo.Root, CheckpointReceiptPath, data)
			},
			want: ErrStaleCheckpoint,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo, base, candidate := newBehaviorRepository(t)
			testCase.prepare(t, repo, base, candidate)
			_, err := ReadCheckpoint(context.Background(), repo)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("ReadCheckpoint() error=%v, want %v", err, testCase.want)
			}
		})
	}
}

func TestRecordCheckpointRejectsInvalidOrChangedCandidateBeforeWriting(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, repository.Repository, string, string) RecordCheckpointOptions{
		"missing review": func(_ *testing.T, _ repository.Repository, base, candidate string) RecordCheckpointOptions {
			return RecordCheckpointOptions{Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, GateRun: checkpointEvidenceInput()}
		},
		"dirty candidate": func(t *testing.T, repo repository.Repository, base, candidate string) RecordCheckpointOptions {
			writeBehaviorFile(t, repo.Root, "dirty.txt", "dirty\n")
			return RecordCheckpointOptions{Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReviewID: "review-123", GateRun: checkpointEvidenceInput()}
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, base, candidate := newBehaviorRepository(t)
			_, err := RecordCheckpoint(context.Background(), repo, mutate(t, repo, base, candidate))
			if err == nil {
				t.Fatal("invalid checkpoint was accepted")
			}
			if _, statErr := os.Stat(filepath.Join(repo.Root, filepath.FromSlash(checkpointDirectory))); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("invalid checkpoint created artifacts: %v", statErr)
			}
		})
	}
}

func TestCheckpointArtifactRootRejectsEscapingSymlink(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	if err := os.MkdirAll(filepath.Join(repo.Root, ".code-polishy-reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo.Root, ".code-polishy-reports", "checkpoint-gate")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	_, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReviewID: "review-123", GateRun: checkpointEvidenceInput(),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("checkpoint artifact root error = %v, want invalid input", err)
	}
}

type behaviorRunner struct {
	candidateRoot          string
	baselineStatus         int
	mutateCandidate        bool
	mutateReplayCandidate  bool
	timeoutCandidate       bool
	attachBaselineWorktree bool
	roots                  []string
}

func (commandRunner *behaviorRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, _, err := commandRunner.RunWithOutput(ctx, root, command)
	return err
}

func (commandRunner *behaviorRunner) RunWithOutput(_ context.Context, root string, _ policy.Command) (runner.Result, runner.Output, error) {
	commandRunner.roots = append(commandRunner.roots, root)
	if commandRunner.attachBaselineWorktree && root != commandRunner.candidateRoot {
		if err := exec.Command("git", "-C", root, "switch", "-c", "cleanup-block").Run(); err != nil {
			return runner.Result{ExitStatus: -1}, runner.Output{}, err
		}
		commandRunner.attachBaselineWorktree = false
	}
	test, err := os.ReadFile(filepath.Join(root, "evidence_test.go"))
	if err != nil {
		return runner.Result{ExitStatus: -1}, runner.Output{}, err
	}
	app, err := os.ReadFile(filepath.Join(root, "app.txt"))
	if err != nil {
		return runner.Result{ExitStatus: -1}, runner.Output{}, err
	}
	if root != commandRunner.candidateRoot && commandRunner.baselineStatus != 0 {
		return runner.Result{ExitStatus: commandRunner.baselineStatus}, runner.Output{Stdout: []byte("baseline failed\n")}, errors.New("test failed")
	}
	if strings.Contains(string(test), "expect-new") && strings.TrimSpace(string(app)) == "new" {
		if commandRunner.mutateCandidate && root == commandRunner.candidateRoot || commandRunner.mutateReplayCandidate && root != commandRunner.candidateRoot {
			if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("mutated\n"), 0o600); err != nil {
				return runner.Result{ExitStatus: -1}, runner.Output{}, err
			}
		}
		if commandRunner.timeoutCandidate && root == commandRunner.candidateRoot {
			return runner.Result{ExitStatus: 124}, runner.Output{Stdout: []byte("candidate timed out\n")}, context.DeadlineExceeded
		}
		return runner.Result{ExitStatus: 0}, runner.Output{Stdout: []byte("candidate passed\n")}, nil
	}
	return runner.Result{ExitStatus: 1}, runner.Output{Stdout: []byte("baseline failed\n")}, errors.New("test failed")
}

func newBehaviorRepository(t *testing.T) (repository.Repository, string, string) {
	t.Helper()
	root := t.TempDir()
	writeBehaviorFile(t, root, "templates/behavior-review.md", "Review the packet.\n")
	writeBehaviorFile(t, root, "docs/design.md", "Application design.\n")
	writeBehaviorFile(t, root, "app.txt", "old\n")
	writeBehaviorFile(t, root, "README.md", "# Test\n")
	writeBehaviorFile(t, root, ".gitignore", ".code-polishy-reports/\n")
	gitBehavior(t, root, "init", "-b", "main")
	gitBehavior(t, root, "config", "user.email", "test@example.com")
	gitBehavior(t, root, "config", "user.name", "Test")
	gitBehavior(t, root, "add", ".")
	gitBehavior(t, root, "commit", "-m", "base")
	gitBehavior(t, root, "commit", "--allow-empty", "-m", "review base")
	base := strings.TrimSpace(gitBehavior(t, root, "rev-parse", "HEAD"))
	repo := repository.Repository{Root: root, PolicyRoot: root, Config: behaviorTestConfig()}
	gitBehavior(t, root, "switch", "-c", "feature")
	captureReviewIntent(t, repo, "Set the application value to new.\n")
	writeBehaviorFile(t, root, "app.txt", "new\n")
	writeBehaviorFile(t, root, "evidence_test.go", "expect-new\n")
	gitBehavior(t, root, "add", ".")
	gitBehavior(t, root, "commit", "-m", "candidate")
	candidate := strings.TrimSpace(gitBehavior(t, root, "rev-parse", "HEAD"))
	return repo, base, candidate
}

func checkpointGateEvidence(t *testing.T, repo repository.Repository, base, candidate string) gaterun.ExecutionEvidence {
	t.Helper()
	identity, err := gaterun.NewIdentity(gaterun.IdentityInput{
		Gate: gaterun.CheckpointGate, RequestedBase: "main", ExactBase: base, Candidate: candidate, PolicyLevel: CheckpointScopeChanged,
		Release: gaterun.ReleaseIdentity{Version: "test", Digest: strings.Repeat("a", 64)}, ConfigurationSHA256: strings.Repeat("b", 64),
		Platform: gaterun.Platform{OS: "test", Arch: "test"}, Commands: []gaterun.CommandSpec{}, Environment: []gaterun.EnvironmentInput{}, AmbientEnvironment: []gaterun.EnvironmentInput{},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := gaterun.Start(gaterun.StartOptions{RepositoryRoot: repo.Root, Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	report, err := run.Finalize(gaterun.FinalizeOptions{Status: gaterun.RunPassed, Findings: []gaterun.Finding{}, Notes: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return gaterun.ExecutionEvidence{
		Gate: gaterun.CheckpointGate, IdentitySHA256: report.IdentitySHA256, ExecutionID: report.ExecutionID, ReportSHA256: report.SHA256,
	}
}

func checkpointEvidenceInput() gaterun.ExecutionEvidence {
	return gaterun.ExecutionEvidence{
		Gate: gaterun.CheckpointGate, IdentitySHA256: strings.Repeat("a", 64),
		ExecutionID: "run-" + strings.Repeat("b", 32), ReportSHA256: strings.Repeat("c", 64),
	}
}

func newExistingEvidenceRepository(t *testing.T) (repository.Repository, string, string) {
	t.Helper()
	root := t.TempDir()
	writeBehaviorFile(t, root, "templates/behavior-review.md", "Review the packet.\n")
	writeBehaviorFile(t, root, "docs/design.md", "Application design.\n")
	writeBehaviorFile(t, root, "app.txt", "old\n")
	writeBehaviorFile(t, root, "evidence_test.go", "expect-new\n")
	writeBehaviorFile(t, root, ".gitignore", ".code-polishy-reports/\n")
	gitBehavior(t, root, "init", "-b", "main")
	gitBehavior(t, root, "config", "user.email", "test@example.com")
	gitBehavior(t, root, "config", "user.name", "Test")
	gitBehavior(t, root, "add", ".")
	gitBehavior(t, root, "commit", "-m", "base")
	gitBehavior(t, root, "commit", "--allow-empty", "-m", "review base")
	base := strings.TrimSpace(gitBehavior(t, root, "rev-parse", "HEAD"))
	repo := repository.Repository{Root: root, PolicyRoot: root, Config: behaviorTestConfig()}
	gitBehavior(t, root, "switch", "-c", "feature")
	captureReviewIntent(t, repo, "Set the application value to new.\n")
	writeBehaviorFile(t, root, "app.txt", "new\n")
	gitBehavior(t, root, "add", "app.txt")
	gitBehavior(t, root, "commit", "-m", "candidate")
	candidate := strings.TrimSpace(gitBehavior(t, root, "rev-parse", "HEAD"))
	return repo, base, candidate
}

func behaviorTestConfig() policy.Config {
	return policy.Config{
		Modules:       []policy.Module{{Name: "app", Paths: []string{"**"}}},
		Documentation: policy.Documentation{Design: []policy.DesignDocument{{Path: "docs/design.md", Module: "app"}}},
		Tests: policy.Testing{Suites: []policy.TestSuite{{
			Name: "unit", Kind: "unit", Scope: "repository", Argv: []string{"runner"}, Cwd: ".", RunOn: []string{"full"}, TimeoutSeconds: 30,
		}}},
	}
}

func prepareReview(t *testing.T, repo repository.Repository) PrepareResult {
	t.Helper()
	result, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main"})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func captureReviewIntent(t *testing.T, repo repository.Repository, contents string) CaptureIntentResult {
	t.Helper()
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", contents)
	result, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func proveReview(t *testing.T, repo repository.Repository, id string, expectedRedExitStatus int) ProveResult {
	t.Helper()
	result, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{
		Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: id, ExpectedRedExitStatus: expectedRedExitStatus,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func writeRequestedResult(t *testing.T, repo repository.Repository, prepared PrepareResult, proofID string) {
	t.Helper()
	writeReviewResult(t, repo, prepared, ReviewResult{
		Version: artifactVersion, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: prepared.Candidate, IntentSHA256: prepared.IntentSHA256,
		Behaviors: []Behavior{{Before: "old value", After: "new value", Classification: "requested", ProofIDs: []string{proofID}}}, Findings: []string{},
	})
}

func assertBoundProof(t *testing.T, proof regressionProof, prepared PrepareResult) {
	t.Helper()
	if proof.ReviewID != prepared.ReviewID || proof.ReviewBase != prepared.Base || proof.ExpectedRedExitStatus != 1 || proof.Baseline.ExitStatus != 1 || proof.CandidateExecution.ExitStatus != 0 || proof.ApplyLogSHA256 == "" || proof.Baseline.LogSHA256 == "" || proof.CandidateExecution.LogSHA256 == "" {
		t.Fatalf("proof = %+v", proof)
	}
}

func finalizedPreservedReview(t *testing.T, repo repository.Repository) (PrepareResult, GateReceipt) {
	t.Helper()
	prepared := prepareReview(t, repo)
	writePreservedResult(t, repo, prepared)
	if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); err != nil {
		t.Fatal(err)
	}
	receipt, err := readReceipt(behaviorArtifact(t, repo))
	if err != nil {
		t.Fatal(err)
	}
	return prepared, receipt
}

func finalizedRequestedReview(t *testing.T) (repository.Repository, ProveResult) {
	t.Helper()
	repo, _, _ := newBehaviorRepository(t)
	prepared := prepareReview(t, repo)
	proof := proveReview(t, repo, "proof-1", 1)
	writeRequestedResult(t, repo, prepared, proof.ID)
	if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); err != nil {
		t.Fatal(err)
	}
	return repo, proof
}

func writePreservedResult(t *testing.T, repo repository.Repository, prepared PrepareResult) {
	t.Helper()
	writeReviewResult(t, repo, prepared, ReviewResult{
		Version: artifactVersion, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: prepared.Candidate, IntentSHA256: prepared.IntentSHA256,
		Behaviors: []Behavior{{Before: "existing interface", After: "existing interface", Classification: "preserved", ProofIDs: []string{}}}, Findings: []string{},
	})
}

func writeReviewResult(t *testing.T, repo repository.Repository, prepared PrepareResult, review ReviewResult) {
	t.Helper()
	writeBehaviorFile(t, behaviorRoot(t, repo), defaultResultFilename, reviewJSON(review))
}

func writeProofRecord(t *testing.T, root string, proof regressionProof) {
	t.Helper()
	data, err := marshalArtifact(proof)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorBytes(t, root, proofArtifactName(proof.ID, ".json"), data)
}

func writePacketAndMarker(t *testing.T, root string, packet reviewPacket) ([]byte, []byte) {
	t.Helper()
	data, err := marshalArtifact(packet)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorBytes(t, root, packetFilename, data)
	markerData, err := marshalArtifact(prepareMarker{
		Version: artifactVersion, ReviewID: packet.ReviewID, Base: packet.Base, Candidate: packet.Candidate, PacketSHA256: sha256Hex(data),
	})
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorBytes(t, root, prepareMarkerFilename, markerData)
	return data, markerData
}

func writeReceiptRecord(t *testing.T, root string, receipt GateReceipt) {
	t.Helper()
	data, err := marshalArtifact(receipt)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorBytes(t, root, receiptFilename, data)
}

func readBehaviorArtifact(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(artifactPath(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cleanupAttachedWorktree(t *testing.T, repo repository.Repository, worktree string) {
	t.Helper()
	if err := exec.Command("git", "-C", worktree, "switch", "--detach").Run(); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveWorktree(context.Background(), worktree); err != nil {
		t.Fatal(err)
	}
}

func refreshPacketDigests(packet *reviewPacket) {
	previous := packet.Intents[0].PreviousSHA256
	for index := range packet.Intents {
		packet.Intents[index].PreviousSHA256 = previous
		packet.Intents[index].IntentSHA256 = sha256Hex([]byte(packet.Intents[index].Intent))
		digest, err := intentEntryDigest(packet.Intents[index])
		if err != nil {
			panic(err)
		}
		packet.Intents[index].EntrySHA256 = digest
		previous = digest
	}
	intentSHA256, err := intentSelectionDigest(packet.Intents)
	if err != nil {
		panic(err)
	}
	packet.IntentSHA256 = intentSHA256
	packet.PatchSHA256 = sha256Hex([]byte(packet.Patch))
	for index := range packet.DesignDocuments {
		packet.DesignDocuments[index].SHA256 = sha256Hex([]byte(packet.DesignDocuments[index].Content))
	}
}

func reviewJSON(review ReviewResult) string {
	data, err := json.Marshal(review)
	if err != nil {
		panic(err)
	}
	return string(data) + "\n"
}

func behaviorRoot(t *testing.T, repo repository.Repository) string {
	t.Helper()
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	return root.path
}

func behaviorArtifact(t *testing.T, repo repository.Repository) *artifactHandle {
	t.Helper()
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

func writeBehaviorFile(t *testing.T, root, name, contents string) string {
	t.Helper()
	return writeBehaviorBytes(t, root, name, []byte(contents))
}

func writeBehaviorBytes(t *testing.T, root, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func removeBehaviorFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func gitBehavior(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}
