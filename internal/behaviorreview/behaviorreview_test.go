package behaviorreview

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPrepareWritesPacketBoundToCleanCommittedCandidate(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	intent := writeBehaviorFile(t, t.TempDir(), "request.txt", "Change the value to new.\n")
	prepared, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main", IntentPath: intent})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Base != base || prepared.Candidate != candidate || !strings.HasPrefix(prepared.ReviewID, "review-") || prepared.PacketPath != artifactDisplayPath(packetFilename) {
		t.Fatalf("prepare result = %+v", prepared)
	}
	root := behaviorRoot(t, repo)
	packet, data, err := readPacket(root)
	if err != nil {
		t.Fatal(err)
	}
	if packet.ReviewID != prepared.ReviewID || packet.Intent != "Change the value to new.\n" || packet.ResultPath != artifactDisplayPath(defaultResultFilename) ||
		packet.ProofDirectory != artifactDisplayPath(proofDirectory) || packet.PatchSHA256 == "" || !strings.Contains(packet.Patch, "diff --git") || len(data) == 0 {
		t.Fatalf("packet = %+v", packet)
	}
	if got, err := repo.CleanHead(); err != nil || got != candidate {
		t.Fatalf("candidate after prepare = %q, %v", got, err)
	}
}

func TestPrepareRejectsUnsafeIntentAndDirtyCandidate(t *testing.T) {
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
			_, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main", IntentPath: path})
			if !errors.Is(err, test.want) {
				t.Fatalf("Prepare() error = %v, want %v", err, test.want)
			}
			removeBehaviorFile(t, path)
		})
	}
	intent := writeBehaviorFile(t, t.TempDir(), "request.txt", "request\n")
	writeBehaviorFile(t, repo.Root, "dirty.txt", "dirty\n")
	_, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main", IntentPath: intent})
	if !errors.Is(err, repository.ErrDirtyCandidate) {
		t.Fatalf("dirty Prepare() error = %v, want dirty candidate", err)
	}
}

func TestFinalizeAndValidateMergeReceipt(t *testing.T) {
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
	receipt, err := ValidateMergeReceipt(context.Background(), repo, ValidateMergeReceiptOptions{Base: "main"})
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
				return `{"version":1,"review_id":"` + prepared.ReviewID + `","base":"` + base + `","candidate":"` + candidate + `","intent_sha256":"` + prepared.IntentSHA256 + `","behaviors":[],"findings":[],"extra":true}`
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

func TestValidateMergeReceiptRejectsPacketAndCandidateChanges(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	prepared := prepareReview(t, repo)
	proof := proveReview(t, repo, "proof-1", 1)
	writeRequestedResult(t, repo, prepared, proof.ID)
	if _, err := Finalize(context.Background(), repo, FinalizeOptions{Base: "main"}); err != nil {
		t.Fatal(err)
	}
	root := behaviorRoot(t, repo)
	writeBehaviorFile(t, root, packetFilename, "{}\n")
	if _, err := ValidateMergeReceipt(context.Background(), repo, ValidateMergeReceiptOptions{Base: "main"}); !errors.Is(err, ErrStaleReceipt) {
		t.Fatalf("packet change validation error = %v, want stale receipt", err)
	}
	prepareReview(t, repo)
	writeBehaviorFile(t, repo.Root, "app.txt", "changed outside commit\n")
	if _, err := ValidateMergeReceipt(context.Background(), repo, ValidateMergeReceiptOptions{Base: "main"}); !errors.Is(err, repository.ErrDirtyCandidate) {
		t.Fatalf("candidate change validation error = %v, want dirty candidate", err)
	}
}

func TestProveCreatesRedGreenEvidenceAndCleansWorktree(t *testing.T) {
	repo, base, candidate := newBehaviorRepository(t)
	commandRunner := &behaviorRunner{candidateRoot: repo.Root}
	result, err := Prove(context.Background(), repo, commandRunner, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "proof-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Base != base || result.Candidate != candidate || result.ProofPath != artifactDisplayPath(proofArtifactName("proof-1", ".json")) {
		t.Fatalf("proof result = %+v", result)
	}
	root := behaviorRoot(t, repo)
	proof, _, err := readProof(root, "proof-1")
	if err != nil {
		t.Fatal(err)
	}
	if proof.ExpectedRedExitStatus != 1 || proof.Baseline.ExitStatus != 1 || proof.CandidateExecution.ExitStatus != 0 || proof.ApplyLogSHA256 == "" || proof.Baseline.LogSHA256 == "" || proof.CandidateExecution.LogSHA256 == "" {
		t.Fatalf("proof = %+v", proof)
	}
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
	result, err := Prove(context.Background(), repo, &behaviorRunner{candidateRoot: repo.Root}, ProveOptions{Base: "main", Suite: "unit", Evidence: []string{"evidence_test.go"}, ID: "existing"})
	if err != nil {
		t.Fatal(err)
	}
	proof, _, err := readProof(behaviorRoot(t, repo), result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proof.EvidencePatchSHA256 != sha256Hex(nil) || proof.Baseline.ExitStatus != 1 || proof.CandidateExecution.ExitStatus != 0 {
		t.Fatalf("proof = %+v", proof)
	}
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
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repo, _, _ := newBehaviorRepository(t)
			prepared := prepareReview(t, repo)
			proofResult := proveReview(t, repo, "proof-1", 1)
			root := behaviorRoot(t, repo)
			proof, _, err := readProof(root, proofResult.ID)
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

func TestProveRejectsWrongRedStatusAndUnsafeEvidence(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
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

type behaviorRunner struct {
	candidateRoot   string
	baselineStatus  int
	mutateCandidate bool
	roots           []string
}

func (commandRunner *behaviorRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, _, err := commandRunner.RunWithOutput(ctx, root, command)
	return err
}

func (commandRunner *behaviorRunner) RunWithOutput(_ context.Context, root string, _ policy.Command) (runner.Result, runner.Output, error) {
	commandRunner.roots = append(commandRunner.roots, root)
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
		if commandRunner.mutateCandidate && root == commandRunner.candidateRoot {
			if err := os.WriteFile(filepath.Join(root, "app.txt"), []byte("mutated\n"), 0o600); err != nil {
				return runner.Result{ExitStatus: -1}, runner.Output{}, err
			}
		}
		return runner.Result{ExitStatus: 0}, runner.Output{Stdout: []byte("candidate passed\n")}, nil
	}
	return runner.Result{ExitStatus: 1}, runner.Output{Stdout: []byte("baseline failed\n")}, errors.New("test failed")
}

func newBehaviorRepository(t *testing.T) (repository.Repository, string, string) {
	t.Helper()
	root := t.TempDir()
	writeBehaviorFile(t, root, "templates/behavior-review.md", "Review the packet.\n")
	writeBehaviorFile(t, root, "app.txt", "old\n")
	writeBehaviorFile(t, root, "README.md", "# Test\n")
	gitBehavior(t, root, "init", "-b", "main")
	gitBehavior(t, root, "config", "user.email", "test@example.com")
	gitBehavior(t, root, "config", "user.name", "Test")
	gitBehavior(t, root, "add", ".")
	gitBehavior(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(gitBehavior(t, root, "rev-parse", "HEAD"))
	gitBehavior(t, root, "switch", "-c", "feature")
	writeBehaviorFile(t, root, "app.txt", "new\n")
	writeBehaviorFile(t, root, "evidence_test.go", "expect-new\n")
	gitBehavior(t, root, "add", ".")
	gitBehavior(t, root, "commit", "-m", "candidate")
	candidate := strings.TrimSpace(gitBehavior(t, root, "rev-parse", "HEAD"))
	config := policy.Config{
		Modules: []policy.Module{{Name: "app", Paths: []string{"**"}}},
		Tests: policy.Testing{Suites: []policy.TestSuite{{
			Name: "unit", Kind: "unit", Scope: "repository", Argv: []string{"runner"}, Cwd: ".", RunOn: []string{"full"}, TimeoutSeconds: 30,
		}}},
	}
	return repository.Repository{Root: root, PolicyRoot: root, Config: config}, base, candidate
}

func newExistingEvidenceRepository(t *testing.T) (repository.Repository, string, string) {
	t.Helper()
	root := t.TempDir()
	writeBehaviorFile(t, root, "templates/behavior-review.md", "Review the packet.\n")
	writeBehaviorFile(t, root, "app.txt", "old\n")
	writeBehaviorFile(t, root, "evidence_test.go", "expect-new\n")
	gitBehavior(t, root, "init", "-b", "main")
	gitBehavior(t, root, "config", "user.email", "test@example.com")
	gitBehavior(t, root, "config", "user.name", "Test")
	gitBehavior(t, root, "add", ".")
	gitBehavior(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(gitBehavior(t, root, "rev-parse", "HEAD"))
	gitBehavior(t, root, "switch", "-c", "feature")
	writeBehaviorFile(t, root, "app.txt", "new\n")
	gitBehavior(t, root, "add", "app.txt")
	gitBehavior(t, root, "commit", "-m", "candidate")
	candidate := strings.TrimSpace(gitBehavior(t, root, "rev-parse", "HEAD"))
	config := policy.Config{
		Modules: []policy.Module{{Name: "app", Paths: []string{"**"}}},
		Tests: policy.Testing{Suites: []policy.TestSuite{{
			Name: "unit", Kind: "unit", Scope: "repository", Argv: []string{"runner"}, Cwd: ".", RunOn: []string{"full"}, TimeoutSeconds: 30,
		}}},
	}
	return repository.Repository{Root: root, PolicyRoot: root, Config: config}, base, candidate
}

func prepareReview(t *testing.T, repo repository.Repository) PrepareResult {
	t.Helper()
	intent := writeBehaviorFile(t, t.TempDir(), "intent.txt", "Set the application value to new.\n")
	result, err := Prepare(context.Background(), repo, PrepareOptions{Base: "main", IntentPath: intent})
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
