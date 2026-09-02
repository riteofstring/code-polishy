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
	"sync"
	"testing"

	"github.com/riteofstring/code-polishy/internal/finalstate"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPrepareWritesPacketBoundToCleanCommittedCandidate(t *testing.T) {
	t.Parallel()
	repo, base, candidate := newBehaviorRepository(t)
	prepared, err := Prepare(context.Background(), repo, prepareOptions("main"))
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

func TestCaptureIntentRejectsUnsafeIntent(t *testing.T) {
	t.Parallel()
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
}

func TestPrepareSelectsOneTaskOrTheWholeIntentChain(t *testing.T) {
	t.Parallel()
	repo, base, firstCandidate := newBehaviorRepository(t)
	second := captureReviewIntent(t, repo, "Also write the selected value to a second file.\n")
	if second.Commit != firstCandidate {
		t.Fatalf("second capture commit = %s, want %s", second.Commit, firstCandidate)
	}
	writeBehaviorFile(t, repo.Root, "second.txt", "new\n")
	gitBehavior(t, repo.Root, "add", "second.txt")
	gitBehavior(t, repo.Root, "commit", "-m", "second task")
	secondCandidate := strings.TrimSpace(gitBehavior(t, repo.Root, "rev-parse", "HEAD"))

	task, err := Prepare(context.Background(), repo, prepareOptions(firstCandidate))
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

	merge, err := Prepare(context.Background(), repo, prepareOptions("main"))
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
	t.Parallel()
	repo, _, candidate := newBehaviorRepository(t)
	root := filepath.Join(repo.Root, filepath.FromSlash(artifactDirectory))
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(context.Background(), repo, prepareOptions("main")); !errors.Is(err, ErrInvalidInput) ||
		!strings.Contains(err.Error(), "capture the original request") {
		t.Fatalf("missing intent Prepare() error = %v", err)
	}
	late := captureReviewIntent(t, repo, "Retell the request after implementation.\n")
	if late.Commit != candidate {
		t.Fatalf("late capture commit = %s, want %s", late.Commit, candidate)
	}
	if _, err := Prepare(context.Background(), repo, prepareOptions("main")); !errors.Is(err, ErrInvalidInput) ||
		!strings.Contains(err.Error(), "review base before implementation") {
		t.Fatalf("late intent Prepare() error = %v", err)
	}

	repo, _, candidate = newBehaviorRepository(t)
	late = captureReviewIntent(t, repo, "Add an inaccurate summary after implementation.\n")
	if late.Commit != candidate {
		t.Fatalf("second late capture commit = %s, want %s", late.Commit, candidate)
	}
	if _, err := Prepare(context.Background(), repo, prepareOptions("main")); !errors.Is(err, ErrInvalidInput) ||
		!strings.Contains(err.Error(), "intent must be captured before implementation") {
		t.Fatalf("after-the-fact intent Prepare() error = %v", err)
	}
}

func TestCaptureIntentBindsStagedUnstagedDeletedAndUntrackedChanges(t *testing.T) {
	t.Parallel()
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
		{name: "deleted", change: func(t *testing.T, repo repository.Repository) {
			removeBehaviorFile(t, filepath.Join(repo.Root, "app.txt"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, _, _ := newBehaviorRepository(t)
			test.change(t, repo)
			snapshot, err := repo.CandidateState()
			if err != nil {
				t.Fatal(err)
			}
			intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Next request.\n")
			captured, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent})
			if err != nil {
				t.Fatal(err)
			}
			if captured.Commit != snapshot.Head || captured.CandidateSHA256 != snapshot.SHA256 || !validSHA256(captured.CandidateSHA256) {
				t.Fatalf("capture = %+v, snapshot = %+v", captured, snapshot)
			}
		})
	}
}

func TestCaptureIntentRequiresCleanStateForOriginalRequest(t *testing.T) {
	t.Parallel()
	repo, _, _ := newBehaviorRepository(t)
	if err := os.RemoveAll(filepath.Join(repo.Root, filepath.FromSlash(artifactDirectory))); err != nil {
		t.Fatal(err)
	}
	writeBehaviorFile(t, repo.Root, "untracked.txt", "candidate work\n")
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Original request.\n")
	if _, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent}); !errors.Is(err, repository.ErrDirtyCandidate) {
		t.Fatalf("CaptureIntent() error = %v, want dirty candidate", err)
	}
}

func TestIntentCaptureRejectsCandidateMutationAfterSnapshot(t *testing.T) {
	t.Parallel()
	repo, _, _ := newBehaviorRepository(t)
	snapshot, err := repo.CandidateState()
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorFile(t, repo.Root, "changed-after-snapshot.txt", "candidate work\n")
	if err := ensureIntentCaptureCandidate(repo, snapshot); !errors.Is(err, ErrCandidateChanged) {
		t.Fatalf("ensureIntentCaptureCandidate() error = %v, want candidate changed", err)
	}
}

func TestTaskRequirementsAllowsDirtyCandidateWithoutJournal(t *testing.T) {
	t.Parallel()
	repo, base := newFeatureTaskBaseRepository(t)
	writeBehaviorFile(t, repo.Root, "dirty.txt", "uncommitted documentation draft\n")
	candidate := strings.TrimSpace(gitBehavior(t, repo.Root, "rev-parse", "HEAD"))
	if _, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(artifactDirectory))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("behavior review artifacts before task requirement probe = %v, want missing", err)
	}
	snapshot, err := TaskRequirements(context.Background(), repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Base != base || snapshot.Candidate != candidate || len(snapshot.Requirements) != 0 || !slicesEqual(snapshot.RequestedFeatures, []string{}) || !validSHA256(snapshot.SHA256) {
		t.Fatalf("empty task requirements = %+v", snapshot)
	}
	if _, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(artifactDirectory))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("task requirement probe created artifacts: %v", err)
	}
}

func TestTaskRequirementsReadsPresentJournalWithDirtyCandidate(t *testing.T) {
	t.Parallel()
	repo, base, candidate := newFeatureTaskCandidate(t, []string{"search"})
	writeBehaviorFile(t, repo.Root, "dirty.txt", "uncommitted documentation draft\n")
	snapshot, err := TaskRequirements(context.Background(), repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Base != base || snapshot.Candidate != candidate || len(snapshot.Requirements) != 1 || !slicesEqual(snapshot.RequestedFeatures, []string{"search"}) || !validSHA256(snapshot.SHA256) {
		t.Fatalf("dirty task requirements = %+v", snapshot)
	}
}

func TestCaptureFeatureAndLaterRequireUnion(t *testing.T) {
	t.Parallel()
	repo, base := newFeatureTaskBaseRepository(t)
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Review the search behavior.\n")
	captured, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent, Features: []string{"search"}})
	if err != nil {
		t.Fatal(err)
	}
	if captured.RequirementID == "" || !slicesEqual(captured.Features, []string{"search"}) {
		t.Fatalf("capture result = %+v", captured)
	}
	candidate := commitFeatureCandidate(t, repo)
	first, err := Require(context.Background(), repo, RequireOptions{Base: "main", Features: []string{"checkout"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Base != base || first.Candidate != candidate || !slicesEqual(first.Features, []string{"checkout"}) {
		t.Fatalf("first requirement = %+v", first)
	}
	firstSnapshot, err := TaskRequirements(context.Background(), repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	selection := featureSelection(t, repo, "checkout", "search")
	firstDecision, err := DecisionBindingSHA256(selection, firstSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Require(context.Background(), repo, RequireOptions{Base: "main", Features: []string{"search", "checkout"}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := TaskRequirements(context.Background(), repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Base != base || snapshot.Candidate != candidate || len(snapshot.Requirements) != 3 || !slicesEqual(snapshot.RequestedFeatures, []string{"checkout", "search"}) || !validSHA256(snapshot.SHA256) {
		t.Fatalf("task requirements = %+v", snapshot)
	}
	decision, err := DecisionBindingSHA256(selection, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if decision == firstDecision {
		t.Fatal("decision binding did not change when a later requirement record was added")
	}
}

func TestRequireRejectsUnknownFeatureUnrelatedBaseAndDirtyCandidate(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		features []string
		prepare  func(t *testing.T, repo repository.Repository, base string) string
		want     error
	}{
		{
			name:     "unknown feature",
			features: []string{"missing"},
			prepare:  func(_ *testing.T, _ repository.Repository, base string) string { return base },
			want:     ErrInvalidInput,
		},
		{
			name:     "unrelated base",
			features: []string{"search"},
			prepare: func(t *testing.T, repo repository.Repository, _ string) string {
				candidate := commitFeatureCandidate(t, repo)
				return strings.TrimSpace(gitBehavior(t, repo.Root, "commit-tree", candidate+"^{tree}", "-m", "unrelated"))
			},
			want: repository.ErrNotAncestor,
		},
		{
			name:     "dirty candidate",
			features: []string{"search"},
			prepare: func(t *testing.T, repo repository.Repository, base string) string {
				writeBehaviorFile(t, repo.Root, "app.txt", "dirty\n")
				return base
			},
			want: repository.ErrDirtyCandidate,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, base := newFeatureTaskBaseRepository(t)
			reference := test.prepare(t, repo, base)
			if _, err := Require(context.Background(), repo, RequireOptions{Base: reference, Features: test.features}); !errors.Is(err, test.want) {
				t.Fatalf("Require() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestTaskRequirementRejectsTaskBaseAndIntentTampering(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*intentJournal, string){
		"task base":        func(journal *intentJournal, candidate string) { journal.Requirements[0].TaskBase = candidate },
		"intent reference": func(journal *intentJournal, _ string) { journal.Requirements[0].IntentIDs = []string{"intent-missing"} },
	} {
		t.Run(name, func(t *testing.T) {
			repo, _ := newFeatureTaskBaseRepository(t)
			intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Review the search behavior.\n")
			if _, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent, Features: []string{"search"}}); err != nil {
				t.Fatal(err)
			}
			candidate := commitFeatureCandidate(t, repo)
			root := behaviorArtifact(t, repo)
			journal, err := readIntentJournal(repo, root, false)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&journal, candidate)
			data, err := marshalArtifact(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := root.writeArtifactAtomic(intentJournalFilename, data); err != nil {
				t.Fatal(err)
			}
			if _, err := TaskRequirements(context.Background(), repo, "main"); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("TaskRequirements() error = %v, want invalid input", err)
			}
		})
	}
}

func TestFinalizeRejectsBehaviorOutsideSelectedFeatureScope(t *testing.T) {
	t.Parallel()
	repo, _, candidate := newFeatureTaskCandidate(t, []string{"search"})
	prepared, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main", Selection: featureSelection(t, repo, "search")})
	if err != nil {
		t.Fatal(err)
	}
	for name, scope := range map[string]BehaviorScope{
		"unselected feature": {Features: []string{"checkout"}},
		"full candidate":     {Features: []string{}, FullCandidate: true},
	} {
		t.Run(name, func(t *testing.T) {
			writeReviewResult(t, repo, prepared, ReviewResult{
				Version: artifactVersion, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256,
				Behaviors: []Behavior{{Before: "old", After: "new", Classification: "preserved", ProofIDs: []string{}, Scope: scope}}, Findings: []string{},
			})
			if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); !errors.Is(err, ErrStaleReview) {
				t.Fatalf("Finalize() error = %v, want stale review", err)
			}
		})
	}
}

func TestProofRestrictsSelectedFeatureSuitesAndAllowsFullCandidate(t *testing.T) {
	t.Parallel()
	repo, _, _ := newFeatureTaskCandidate(t, nil)
	if _, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main", Selection: featureSelection(t, repo, "search")}); err != nil {
		t.Fatal(err)
	}
	_, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{
		Base: "main", Suite: "other-unit", Evidence: []string{"evidence_test.go"}, ID: "proof-restricted", ExpectedRedExitStatus: 1,
	})
	if !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("restricted proof error = %v, want invalid evidence", err)
	}
	full := ReviewSelection{Features: []SelectedFeature{}, FullCandidate: true, FullCandidateReasons: []string{SelectionReasonDefaultRequired}}
	if _, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main", Selection: full}); err != nil {
		t.Fatal(err)
	}
	proof, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{
		Base: "main", Suite: "other-unit", Evidence: []string{"evidence_test.go"}, ID: "proof-full", ExpectedRedExitStatus: 1,
	})
	if err != nil || proof.Suite != "other-unit" {
		t.Fatalf("full candidate proof = %+v, error = %v", proof, err)
	}
}

func TestPacketRejectsSelectionRequirementAndDecisionDigestTampering(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*reviewPacket){
		"selection":   func(packet *reviewPacket) { packet.SelectionSHA256 = strings.Repeat("0", 64) },
		"requirement": func(packet *reviewPacket) { packet.RequirementSHA256 = strings.Repeat("0", 64) },
		"decision":    func(packet *reviewPacket) { packet.DecisionSHA256 = strings.Repeat("0", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			repo, _, _ := newFeatureTaskCandidate(t, []string{"search"})
			if _, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main", Selection: featureSelection(t, repo, "search")}); err != nil {
				t.Fatal(err)
			}
			root := behaviorArtifact(t, repo)
			packet, _, err := readPacket(root)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&packet)
			writePacketAndMarker(t, root.path, packet)
			if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); !errors.Is(err, ErrInvalidReview) {
				t.Fatalf("Finalize() error = %v, want invalid review", err)
			}
		})
	}
}

func TestIntentJournalLockRetainsConcurrentFeatureCaptures(t *testing.T) {
	t.Parallel()
	repo, _ := newFeatureTaskBaseRepository(t)
	const captures = 8
	paths := make([]string, 0, captures)
	for index := 0; index < captures; index++ {
		paths = append(paths, writeBehaviorFile(t, t.TempDir(), "intent.txt", "Review search behavior.\n"))
	}
	errorsByCapture := make(chan error, captures)
	var group sync.WaitGroup
	for _, path := range paths {
		group.Add(1)
		go func(path string) {
			defer group.Done()
			_, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: path, Features: []string{"search"}})
			errorsByCapture <- err
		}(path)
	}
	group.Wait()
	close(errorsByCapture)
	for err := range errorsByCapture {
		if err != nil {
			t.Fatal(err)
		}
	}
	root := behaviorArtifact(t, repo)
	journal, err := readIntentJournal(repo, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.Entries) != captures || len(journal.Requirements) != captures {
		t.Fatalf("journal lost concurrent records: %+v", journal)
	}
	snapshot, err := TaskRequirements(context.Background(), repo, "main")
	if err != nil || len(snapshot.Requirements) != captures || !slicesEqual(snapshot.RequestedFeatures, []string{"search"}) {
		t.Fatalf("concurrent requirements = %+v, error = %v", snapshot, err)
	}
}

func TestPrepareRejectsEditedRemovedAndReorderedIntentEntries(t *testing.T) {
	t.Parallel()
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
			if _, err := Prepare(context.Background(), repo, prepareOptions("main")); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Prepare() error = %v, want invalid input", err)
			}
		})
	}
}

func TestCaptureRejectsAJournalFromADivergedBranch(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	receipt, err := ValidateGateReceipt(context.Background(), repo, validationOptions("main"))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Base != base || receipt.Candidate != candidate || len(receipt.Proofs) != 1 || receipt.Proofs[0].ID != proof.ID {
		t.Fatalf("receipt = %+v", receipt)
	}
}

func TestFinalizeRejectsStrictStaleAndBlockingResults(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		result func(PrepareResult, string, string) string
		want   error
	}{
		{
			name: "unknown field", result: func(prepared PrepareResult, base, candidate string) string {
				valid := preparedReviewJSON(prepared, ReviewResult{
					Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256,
					Behaviors: []Behavior{{Before: "old", After: "new", Classification: "preserved", ProofIDs: []string{}}}, Findings: []string{},
				})
				return strings.TrimSuffix(strings.TrimSpace(valid), "}") + `,"extra":true}`
			}, want: ErrInvalidReview,
		},
		{
			name: "stale candidate", result: func(prepared PrepareResult, base, candidate string) string {
				return preparedReviewJSON(prepared, ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: strings.Repeat("0", len(candidate)), IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{{Before: "old", After: "new", Classification: "preserved", ProofIDs: []string{}}}, Findings: []string{}})
			}, want: ErrStaleReview,
		},
		{
			name: "unintended", result: func(prepared PrepareResult, base, candidate string) string {
				return preparedReviewJSON(prepared, ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{{Before: "old", After: "new", Classification: "unintended", ProofIDs: []string{}}}, Findings: []string{}})
			}, want: ErrInvalidReview,
		},
		{
			name: "finding", result: func(prepared PrepareResult, base, candidate string) string {
				return preparedReviewJSON(prepared, ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{}, Findings: []string{"ambiguous outcome"}})
			}, want: ErrInvalidReview,
		},
		{
			name: "empty behaviors", result: func(prepared PrepareResult, base, candidate string) string {
				return preparedReviewJSON(prepared, ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{}, Findings: []string{}})
			}, want: ErrInvalidReview,
		},
		{
			name: "missing proof", result: func(prepared PrepareResult, base, candidate string) string {
				return preparedReviewJSON(prepared, ReviewResult{Version: artifactVersion, ReviewID: prepared.ReviewID, Base: base, Candidate: candidate, IntentSHA256: prepared.IntentSHA256, Behaviors: []Behavior{{Before: "old", After: "new", Classification: "requested", ProofIDs: []string{"absent"}}}, Findings: []string{}})
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
	t.Parallel()
	repo, _, _ := newBehaviorRepository(t)
	prepared := prepareReview(t, repo)
	proof := proveReview(t, repo, "proof-1", 1)
	writeRequestedResult(t, repo, prepared, proof.ID)
	if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); err != nil {
		t.Fatal(err)
	}
	root := behaviorRoot(t, repo)
	writeBehaviorFile(t, root, packetFilename, "{}\n")
	if _, err := ValidateGateReceipt(context.Background(), repo, validationOptions("main")); !errors.Is(err, ErrStaleReceipt) {
		t.Fatalf("packet change validation error = %v, want stale receipt", err)
	}
	prepareReview(t, repo)
	writeBehaviorFile(t, repo.Root, "app.txt", "changed outside commit\n")
	if _, err := ValidateGateReceipt(context.Background(), repo, validationOptions("main")); !errors.Is(err, repository.ErrDirtyCandidate) {
		t.Fatalf("candidate change validation error = %v, want dirty candidate", err)
	}
}

func TestValidateGateReceiptMissingReceiptDoesNotCreateArtifacts(t *testing.T) {
	t.Parallel()
	repo, _, _ := newBehaviorRepository(t)
	path := filepath.Join(repo.Root, filepath.FromSlash(artifactDirectory))
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact root before validation = %v, want missing", err)
	}
	_, err := ValidateGateReceipt(context.Background(), repo, validationOptions("main"))
	if !errors.Is(err, ErrMissingReceipt) {
		t.Fatalf("ValidateGateReceipt() error = %v, want missing receipt", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("artifact root after validation = %v, want missing", err)
	}
}

func TestFinalizeAndReceiptRejectRehashedPacketMaterial(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	if _, err := ValidateGateReceipt(context.Background(), repo, validationOptions("main")); !errors.Is(err, ErrStaleReceipt) {
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
	if _, err := ValidateGateReceipt(context.Background(), repo, validationOptions("main")); !errors.Is(err, ErrStaleReceipt) {
		t.Fatalf("ValidateGateReceipt() error = %v, want stale receipt", err)
	}
}

func TestBehaviorReviewArtifactRootRejectsEscapingSymlink(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	repo, base, candidate := newBehaviorRepository(t)
	result, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReview: checkpointBehaviorReview(), GateRun: checkpointGateEvidence(t, repo, base, candidate),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ReceiptPath != CheckpointReceiptPath || result.Receipt.Base != base || result.Receipt.Candidate != candidate ||
		result.Receipt.Scope != CheckpointScopeChanged || result.Receipt.BehaviorReview.State != gaterun.BehaviorReviewNotRun {
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
	if !reflect.DeepEqual(receipt, result.Receipt) {
		t.Fatalf("checkpoint receipt = %+v, want %+v", receipt, result.Receipt)
	}
}

func TestRecordCheckpointAcceptsDocumentationWithoutBehaviorReview(t *testing.T) {
	t.Parallel()
	repo, base, candidate := newBehaviorRepository(t)
	result, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeDocumentation, BehaviorReview: checkpointBehaviorReview(), GateRun: checkpointGateEvidence(t, repo, base, candidate),
	})
	if err != nil || result.Receipt.BehaviorReview.State != gaterun.BehaviorReviewNotRun {
		t.Fatalf("checkpoint result = %+v, error = %v", result, err)
	}
}

func TestReadCheckpointReturnsOnlyTheCurrentValidReceipt(t *testing.T) {
	t.Parallel()
	repo, base, candidate := newBehaviorRepository(t)
	written, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReview: checkpointBehaviorReview(), GateRun: checkpointGateEvidence(t, repo, base, candidate),
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := ReadCheckpoint(context.Background(), repo)
	if err != nil || !reflect.DeepEqual(read, written.Receipt) {
		t.Fatalf("checkpoint=%+v error=%v want=%+v", read, err, written.Receipt)
	}
}

func TestReadCheckpointRejectsSymlinkReceiptWithoutFollowingIt(t *testing.T) {
	t.Parallel()
	repo, base, candidate := newBehaviorRepository(t)
	if _, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReview: checkpointBehaviorReview(), GateRun: checkpointGateEvidence(t, repo, base, candidate),
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
	t.Parallel()
	repo, _, _ := newBehaviorRepository(t)
	if _, err := ReadCheckpoint(context.Background(), repo); !errors.Is(err, ErrMissingCheckpoint) {
		t.Fatalf("ReadCheckpoint() error = %v, want missing checkpoint", err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, filepath.FromSlash(checkpointDirectory))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing checkpoint read created artifacts: %v", err)
	}
}

func TestReadCheckpointRejectsMissingMalformedStaleAndDivergedReceipts(t *testing.T) {
	t.Parallel()
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
					Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReview: checkpointBehaviorReview(), GateRun: checkpointGateEvidence(t, repo, base, candidate),
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
					Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReview: checkpointBehaviorReview(), GateRun: checkpointGateEvidence(t, repo, base, candidate),
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
	t.Parallel()
	for name, mutate := range map[string]func(*testing.T, repository.Repository, string, string) RecordCheckpointOptions{
		"missing review": func(_ *testing.T, _ repository.Repository, base, candidate string) RecordCheckpointOptions {
			return RecordCheckpointOptions{Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, GateRun: checkpointEvidenceInput()}
		},
		"dirty candidate": func(t *testing.T, repo repository.Repository, base, candidate string) RecordCheckpointOptions {
			writeBehaviorFile(t, repo.Root, "dirty.txt", "dirty\n")
			return RecordCheckpointOptions{Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReview: checkpointBehaviorReview(), GateRun: checkpointEvidenceInput()}
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
	t.Parallel()
	repo, base, candidate := newBehaviorRepository(t)
	if err := os.MkdirAll(filepath.Join(repo.Root, ".code-polishy-reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo.Root, ".code-polishy-reports", "checkpoint-gate")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	_, err := RecordCheckpoint(context.Background(), repo, RecordCheckpointOptions{
		Base: base, Candidate: candidate, Scope: CheckpointScopeChanged, BehaviorReview: checkpointBehaviorReview(), GateRun: checkpointEvidenceInput(),
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
	review := checkpointBehaviorReview()
	identity, err := gaterun.NewIdentity(gaterun.IdentityInput{
		Gate: gaterun.CheckpointGate, RequestedBase: "main", ExactBase: base, Candidate: candidate, PolicyLevel: CheckpointScopeChanged,
		Release: gaterun.ReleaseIdentity{Version: "test", Digest: strings.Repeat("a", 64)}, ConfigurationSHA256: strings.Repeat("b", 64),
		Platform: gaterun.Platform{OS: "test", Arch: "test"}, Commands: []gaterun.CommandSpec{}, Environment: []gaterun.EnvironmentInput{}, AmbientEnvironment: []gaterun.EnvironmentInput{}, BehaviorReview: review,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := gaterun.Start(gaterun.StartOptions{RepositoryRoot: repo.Root, Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	report, err := run.Finalize(gaterun.FinalizeOptions{Status: gaterun.RunPassed, Findings: []gaterun.Finding{}, Notes: []string{}, BehaviorReview: review})
	if err != nil {
		t.Fatal(err)
	}
	return gaterun.ExecutionEvidence{
		Gate: gaterun.CheckpointGate, IdentitySHA256: report.IdentitySHA256, ExecutionID: report.ExecutionID, ReportSHA256: report.SHA256,
	}
}

func checkpointBehaviorReview() gaterun.BehaviorReview {
	return gaterun.BehaviorReview{
		State: gaterun.BehaviorReviewNotRun, RequiredBoundary: gaterun.BehaviorReviewOnRequest,
		SelectedFeatures: []gaterun.BehaviorReviewFeatureSelection{}, SelectionDigest: strings.Repeat("d", 64),
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

func featureBehaviorTestConfig() policy.Config {
	config := behaviorTestConfig()
	config.Verification = policy.Verification{BehaviorReview: &policy.BehaviorReviewPolicy{
		DefaultRequiredAt: policy.BehaviorReviewOnRequest,
		Features: []policy.BehaviorReviewFeature{
			{Name: "checkout", Modules: []string{"app"}, Suites: []string{"checkout-unit"}, RequiredAt: policy.BehaviorReviewMerge},
			{Name: "search", Paths: []string{"app.txt"}, Suites: []string{"search-unit"}},
		},
	}}
	config.Tests.Suites = []policy.TestSuite{
		{Name: "checkout-unit", Kind: "unit", Scope: "repository", Argv: []string{"runner"}, Cwd: ".", RunOn: []string{"full"}, TimeoutSeconds: 30},
		{Name: "other-unit", Kind: "unit", Scope: "repository", Argv: []string{"runner"}, Cwd: ".", RunOn: []string{"full"}, TimeoutSeconds: 30},
		{Name: "search-unit", Kind: "unit", Scope: "repository", Argv: []string{"runner"}, Cwd: ".", RunOn: []string{"full"}, TimeoutSeconds: 30},
	}
	return config
}

func newFeatureTaskBaseRepository(t *testing.T) (repository.Repository, string) {
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
	repo := repository.Repository{Root: root, PolicyRoot: root, Config: featureBehaviorTestConfig()}
	gitBehavior(t, root, "switch", "-c", "feature")
	return repo, base
}

func commitFeatureCandidate(t *testing.T, repo repository.Repository) string {
	t.Helper()
	writeBehaviorFile(t, repo.Root, "app.txt", "new\n")
	writeBehaviorFile(t, repo.Root, "evidence_test.go", "expect-new\n")
	gitBehavior(t, repo.Root, "add", "app.txt", "evidence_test.go")
	gitBehavior(t, repo.Root, "commit", "-m", "candidate")
	return strings.TrimSpace(gitBehavior(t, repo.Root, "rev-parse", "HEAD"))
}

func newFeatureTaskCandidate(t *testing.T, features []string) (repository.Repository, string, string) {
	t.Helper()
	repo, base := newFeatureTaskBaseRepository(t)
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Review the selected behavior.\n")
	if _, err := CaptureIntent(context.Background(), repo, CaptureIntentOptions{IntentPath: intent, Features: features}); err != nil {
		t.Fatal(err)
	}
	return repo, base, commitFeatureCandidate(t, repo)
}

func featureSelection(t *testing.T, repo repository.Repository, names ...string) ReviewSelection {
	t.Helper()
	features := make([]SelectedFeature, 0, len(names))
	for _, name := range names {
		feature, err := ConfiguredFeatureSelection(repo.Config, name, []string{SelectionReasonTaskRequested})
		if err != nil {
			t.Fatal(err)
		}
		features = append(features, feature)
	}
	return ReviewSelection{Features: features, FullCandidate: false, FullCandidateReasons: []string{}}
}

func slicesEqual(left, right []string) bool {
	return reflect.DeepEqual(left, right)
}

func testFullCandidateSelection() ReviewSelection {
	return ReviewSelection{Features: []SelectedFeature{}, FullCandidate: true, FullCandidateReasons: []string{"test"}}
}

func prepareOptions(base string) PrepareOptions {
	return PrepareOptions{Base: base, Selection: testFullCandidateSelection()}
}

func validationOptions(base string) ValidateGateReceiptOptions {
	return ValidateGateReceiptOptions{Base: base, Selection: testFullCandidateSelection()}
}

func prepareReview(t *testing.T, repo repository.Repository) PrepareResult {
	t.Helper()
	result, err := Prepare(context.Background(), repo, prepareOptions("main"))
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
	writeBehaviorFile(t, behaviorRoot(t, repo), defaultResultFilename, preparedReviewJSON(prepared, review))
}

func preparedReviewJSON(prepared PrepareResult, review ReviewResult) string {
	if review.SelectionSHA256 == "" {
		review.SelectionSHA256 = prepared.SelectionSHA256
	}
	if review.DecisionSHA256 == "" {
		review.DecisionSHA256 = prepared.DecisionSHA256
	}
	if review.FinalStateFindings == nil {
		review.FinalStateFindings = []finalstate.Finding{}
	}
	for index := range review.Behaviors {
		if review.Behaviors[index].Scope.Features == nil && !review.Behaviors[index].Scope.FullCandidate {
			review.Behaviors[index].Scope = BehaviorScope{Features: []string{}, FullCandidate: true}
		}
	}
	return reviewJSON(review)
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
		Version: artifactVersion, ReviewID: packet.ReviewID, Base: packet.Base, Candidate: packet.Candidate,
		SelectionSHA256: packet.SelectionSHA256, RequirementSHA256: packet.RequirementSHA256, DecisionSHA256: packet.DecisionSHA256, PacketSHA256: sha256Hex(data),
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
