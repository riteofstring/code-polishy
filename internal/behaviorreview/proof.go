package behaviorreview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const worktreeDirectory = "worktrees"

type proofExecution struct {
	ExitStatus int    `json:"exit_status"`
	LogPath    string `json:"log_path"`
	LogSHA256  string `json:"log_sha256"`
}

type regressionProof struct {
	Version               int            `json:"version"`
	ID                    string         `json:"id"`
	ReviewID              string         `json:"review_id"`
	ReviewBase            string         `json:"review_base"`
	Base                  string         `json:"base"`
	Candidate             string         `json:"candidate"`
	SelectionSHA256       string         `json:"selection_sha256"`
	DecisionSHA256        string         `json:"decision_sha256"`
	SelectedFeatures      []string       `json:"selected_features"`
	FullCandidate         bool           `json:"full_candidate"`
	Suite                 string         `json:"suite"`
	Evidence              []string       `json:"evidence"`
	EvidencePatchSHA256   string         `json:"evidence_patch_sha256"`
	ExpectedRedExitStatus int            `json:"expected_red_exit_status"`
	ApplyLogPath          string         `json:"apply_log_path"`
	ApplyLogSHA256        string         `json:"apply_log_sha256"`
	Baseline              proofExecution `json:"baseline"`
	CandidateExecution    proofExecution `json:"candidate_execution"`
}

type proofInputs struct {
	id                    string
	root                  *artifactHandle
	reviewID              string
	reviewBase            string
	base                  string
	candidate             string
	selection             ReviewSelection
	selectionSHA256       string
	decisionSHA256        string
	suite                 policy.TestSuite
	evidence              []string
	patch                 []byte
	expectedRedExitStatus int
}

func prove(ctx context.Context, repo repository.Repository, commandRunner runner.OutputRunner, options ProveOptions) (result ProveResult, resultErr error) {
	ctx = reviewContext(ctx)
	if commandRunner == nil {
		return ProveResult{}, fmt.Errorf("%w: regression proof requires an output-capturing runner", ErrInvalidInput)
	}
	inputs, err := collectProofInputs(ctx, repo, options)
	if err != nil {
		return ProveResult{}, err
	}
	defer inputs.root.Close()
	worktree, err := createProofWorktree(ctx, repo, inputs.root, inputs.base)
	if err != nil {
		return ProveResult{}, err
	}
	defer func() {
		if cleanupErr := cleanupProofWorktree(repo, worktree); cleanupErr != nil {
			result, resultErr = clearedProofResult(resultErr, cleanupErr)
		}
	}()
	proof, err := executeProof(ctx, repo, commandRunner, inputs.root, worktree, inputs)
	if err != nil {
		return ProveResult{}, err
	}
	data, err := marshalArtifact(proof)
	if err != nil {
		return ProveResult{}, err
	}
	if err := inputs.root.writeArtifactAtomic(proofArtifactName(inputs.id, ".json"), data); err != nil {
		return ProveResult{}, err
	}
	return ProveResult{
		ID: inputs.id, Base: inputs.base, Candidate: inputs.candidate, Suite: inputs.suite.Name, SelectionSHA256: inputs.selectionSHA256, DecisionSHA256: inputs.decisionSHA256,
		ProofPath: artifactDisplayPath(proofArtifactName(inputs.id, ".json")),
	}, nil
}

func replayGateReceipt(ctx context.Context, repo repository.Repository, commandRunner runner.OutputRunner, options ValidateGateReceiptOptions) (GateReceipt, error) {
	ctx = reviewContext(ctx)
	if commandRunner == nil {
		return GateReceipt{}, fmt.Errorf("%w: proof replay requires an output-capturing runner", ErrInvalidInput)
	}
	receipt, err := validateGateReceipt(ctx, repo, options)
	if err != nil {
		return GateReceipt{}, err
	}
	root, packet, err := replayPacket(repo, receipt)
	if err != nil {
		return GateReceipt{}, err
	}
	defer root.Close()
	for _, reference := range receipt.Proofs {
		if err := replayProof(ctx, repo, commandRunner, root, packet, receipt.Candidate, reference); err != nil {
			return GateReceipt{}, err
		}
	}
	return receipt, nil
}

func replayPlan(ctx context.Context, repo repository.Repository, options ValidateGateReceiptOptions) (GateReplayPlan, error) {
	state, err := currentReceiptState(reviewContext(ctx), repo, options)
	if err != nil {
		return GateReplayPlan{}, err
	}
	defer state.root.Close()
	packet, err := validateReceiptPacket(repo, state)
	if err != nil {
		return GateReplayPlan{}, err
	}
	review, err := validateReceiptReview(state, packet)
	if err != nil {
		return GateReplayPlan{}, err
	}
	phases, references, err := replayProofPhases(repo, state.root, review, state.candidate, packet)
	if err != nil {
		return GateReplayPlan{}, staleReceipt("proofs do not validate", err)
	}
	if !slices.Equal(references, state.receipt.Proofs) {
		return GateReplayPlan{}, staleReceipt("proof references do not match the receipt", nil)
	}
	return GateReplayPlan{Receipt: cloneGateReceipt(state.receipt), Proofs: phases}, nil
}

func cloneGateReceipt(receipt GateReceipt) GateReceipt {
	return GateReceipt{
		Version: receipt.Version, ReviewID: receipt.ReviewID, Base: receipt.Base, Candidate: receipt.Candidate, IntentSHA256: receipt.IntentSHA256,
		SelectionSHA256: receipt.SelectionSHA256, RequirementSHA256: receipt.RequirementSHA256, DecisionSHA256: receipt.DecisionSHA256,
		Selection:    cloneReviewSelection(receipt.Selection),
		PacketSHA256: receipt.PacketSHA256, PrepareSHA256: receipt.PrepareSHA256, ReviewSHA256: receipt.ReviewSHA256,
		Proofs: append([]ProofReference{}, receipt.Proofs...),
	}
}

func replayPacket(repo repository.Repository, receipt GateReceipt) (*artifactHandle, reviewPacket, error) {
	root, err := existingBehaviorReviewRoot(repo)
	if err != nil {
		return nil, reviewPacket{}, staleReceipt("prepared packet is unavailable for replay", err)
	}
	success := false
	defer func() {
		if !success {
			_ = root.Close()
		}
	}()
	prepared, err := preparedPacketFor(repo, root, receipt.Base, receipt.Candidate)
	if err != nil {
		return nil, reviewPacket{}, staleReceipt("prepared packet changed before replay", err)
	}
	state := receiptState{base: receipt.Base, candidate: receipt.Candidate, receipt: receipt}
	if !packetMatchesReceipt(prepared.packet, prepared.packetData, prepared.markerData, state) {
		return nil, reviewPacket{}, staleReceipt("prepared packet does not match replay receipt", nil)
	}
	success = true
	return root, prepared.packet, nil
}

func replayProof(ctx context.Context, repo repository.Repository, commandRunner runner.OutputRunner, root *artifactHandle, packet reviewPacket, candidate string, reference ProofReference) error {
	proof, data, err := readProof(root, reference.ID)
	if err != nil {
		return err
	}
	if sha256Hex(data) != reference.SHA256 {
		return fmt.Errorf("%w: proof %q does not match its receipt digest", ErrInvalidEvidence, proof.ID)
	}
	suite, patch, err := proofReplayMaterial(repo, proof, candidate, packet)
	if err != nil {
		return replayEvidenceError(proof.ID, err)
	}
	return replayProofExecution(ctx, repo, commandRunner, root, candidate, proof, suite, patch)
}

func replayEvidenceError(id string, err error) error {
	if errors.Is(err, ErrOperational) || errors.Is(err, ErrCleanup) || errors.Is(err, ErrCandidateChanged) {
		return err
	}
	return fmt.Errorf("%w: proof %q cannot be replayed: %v", ErrInvalidEvidence, id, err)
}

func replayProofExecution(ctx context.Context, repo repository.Repository, commandRunner runner.OutputRunner, root *artifactHandle, candidate string, proof regressionProof, suite policy.TestSuite, patch []byte) (resultErr error) {
	baseline, err := createReplayWorktree(ctx, repo, root, "replay-baseline-", proof.Base)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = joinReplayCleanup(resultErr, cleanupProofWorktree(repo, baseline))
	}()
	candidateWorktree, err := createReplayWorktree(ctx, repo, root, "replay-candidate-", candidate)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = joinReplayCleanup(resultErr, cleanupProofWorktree(repo, candidateWorktree))
	}()
	candidateRepository := repo
	candidateRepository.Root = candidateWorktree
	if err := replayApplyEvidencePatch(ctx, baseline, patch); err != nil {
		return err
	}
	if err := ensureCandidateUnchanged(repo, candidate); err != nil {
		return err
	}
	baselineResult, _, baselineErr := runSuite(ctx, commandRunner, baseline, suite)
	if err := ensureCandidateUnchanged(repo, candidate); err != nil {
		return err
	}
	if err := validateReplayBaseline(ctx, baselineResult.ExitStatus, baselineErr, proof); err != nil {
		return err
	}
	if err := ensureReplayCandidateUnchanged(repo, candidateRepository, candidate); err != nil {
		return err
	}
	candidateResult, _, candidateErr := runSuite(ctx, commandRunner, candidateWorktree, suite)
	if err := ensureReplayCandidateUnchanged(repo, candidateRepository, candidate); err != nil {
		return err
	}
	return validateReplayCandidate(ctx, candidateResult.ExitStatus, candidateErr, proof)
}

func ensureReplayCandidateUnchanged(repo, candidateRepository repository.Repository, candidate string) error {
	if err := ensureCandidateUnchanged(repo, candidate); err != nil {
		return err
	}
	return ensureCandidateUnchanged(candidateRepository, candidate)
}

func joinReplayCleanup(previous, cleanupErr error) error {
	if cleanupErr == nil {
		return previous
	}
	if previous == nil {
		return cleanupErr
	}
	return errors.Join(previous, cleanupErr)
}

func replayApplyEvidencePatch(ctx context.Context, worktree string, patch []byte) error {
	_, err := applyEvidencePatch(ctx, worktree, patch)
	return err
}

func validateReplayBaseline(ctx context.Context, status int, runErr error, proof regressionProof) error {
	if executionOperational(ctx, status, runErr) {
		return operational("replay baseline regression suite", runErr)
	}
	if status != proof.ExpectedRedExitStatus {
		return fmt.Errorf("%w: replay baseline suite %q exited with %d, expected %d", ErrInvalidEvidence, proof.Suite, status, proof.ExpectedRedExitStatus)
	}
	return nil
}

func validateReplayCandidate(ctx context.Context, status int, runErr error, proof regressionProof) error {
	if executionOperational(ctx, status, runErr) {
		return operational("replay candidate regression suite", runErr)
	}
	if runErr != nil || status != 0 {
		return fmt.Errorf("%w: replay candidate suite %q did not exit successfully", ErrInvalidEvidence, proof.Suite)
	}
	return nil
}

func collectProofInputs(ctx context.Context, repo repository.Repository, options ProveOptions) (proofInputs, error) {
	expected, err := validateProofRequest(ctx, options)
	if err != nil {
		return proofInputs{}, err
	}
	base, candidate, err := proofBaseAndCandidate(repo, options.Base)
	if err != nil {
		return proofInputs{}, err
	}
	root, packet, err := preparedProofPacket(repo, candidate)
	if err != nil {
		return proofInputs{}, err
	}
	success := false
	defer func() {
		if !success {
			_ = root.Close()
		}
	}()
	if err := proofBaseMatchesReview(repo, packet.Base, base); err != nil {
		return proofInputs{}, err
	}
	suite, evidence, patch, err := proofMaterial(repo, base, candidate, options, packet)
	if err != nil {
		return proofInputs{}, err
	}
	success = true
	return proofInputs{
		id: options.ID, root: root, reviewID: packet.ReviewID, reviewBase: packet.Base, base: base, candidate: candidate,
		selection: cloneReviewSelection(packet.Selection), selectionSHA256: packet.SelectionSHA256, decisionSHA256: packet.DecisionSHA256, suite: suite, evidence: evidence,
		patch: patch, expectedRedExitStatus: expected,
	}, nil
}

func preparedProofPacket(repo repository.Repository, candidate string) (*artifactHandle, reviewPacket, error) {
	root, err := existingBehaviorReviewRoot(repo)
	if err != nil {
		return nil, reviewPacket{}, fmt.Errorf("%w: prepared packet is required", ErrInvalidReview)
	}
	success := false
	defer func() {
		if !success {
			_ = root.Close()
		}
	}()
	packet, _, err := readPacket(root)
	if err != nil {
		return nil, reviewPacket{}, err
	}
	prepared, err := preparedPacketFor(repo, root, packet.Base, candidate)
	if err != nil {
		return nil, reviewPacket{}, err
	}
	success = true
	return root, prepared.packet, nil
}

func proofBaseMatchesReview(repo repository.Repository, reviewBase, proofBase string) error {
	allowed, err := repo.IsAncestor(reviewBase, proofBase)
	if err != nil {
		return operational("verify regression proof review ancestry", err)
	}
	if !allowed {
		return fmt.Errorf("%w: regression proof base predates the prepared review base", ErrInvalidEvidence)
	}
	return nil
}

func validateProofRequest(ctx context.Context, options ProveOptions) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, operational("run regression proof", err)
	}
	if !validIdentifier(options.ID) {
		return 0, fmt.Errorf("%w: proof identifier is malformed", ErrInvalidInput)
	}
	expected := options.ExpectedRedExitStatus
	if expected == 0 {
		expected = 1
	}
	if expected < 1 || expected > 255 {
		return 0, fmt.Errorf("%w: expected red exit status must be from 1 through 255", ErrInvalidInput)
	}
	return expected, nil
}

func proofBaseAndCandidate(repo repository.Repository, reference string) (string, string, error) {
	candidate, err := repo.CleanHead()
	if err != nil {
		return "", "", err
	}
	base, err := repo.ResolveAncestor(reference)
	if err != nil {
		return "", "", err
	}
	if base == candidate {
		return "", "", fmt.Errorf("%w: regression proof base must precede the candidate", ErrInvalidInput)
	}
	return base, candidate, nil
}

func proofMaterial(repo repository.Repository, base, candidate string, options ProveOptions, packet reviewPacket) (policy.TestSuite, []string, []byte, error) {
	suite, err := ordinarySuite(repo.Config, options.Suite)
	if err != nil {
		return policy.TestSuite{}, nil, nil, err
	}
	if !selectionAllowsSuite(packet.Selection, suite.Name) {
		return policy.TestSuite{}, nil, nil, fmt.Errorf("%w: suite %q is not eligible for the selected behavior review features", ErrInvalidEvidence, suite.Name)
	}
	evidence, err := validateEvidence(repo, options.Evidence)
	if err != nil {
		return policy.TestSuite{}, nil, nil, err
	}
	patch, err := repo.Patch(base, candidate, evidence)
	if err != nil {
		return policy.TestSuite{}, nil, nil, operational("build regression evidence patch", err)
	}
	return suite, evidence, patch, nil
}

func createProofWorktree(ctx context.Context, repo repository.Repository, root *artifactHandle, base string) (string, error) {
	return createDetachedProofWorktree(ctx, repo, root, "baseline-", base)
}

func createReplayWorktree(ctx context.Context, repo repository.Repository, root *artifactHandle, prefix, revision string) (string, error) {
	return createDetachedProofWorktree(ctx, repo, root, prefix, revision)
}

func createDetachedProofWorktree(ctx context.Context, repo repository.Repository, root *artifactHandle, prefix, revision string) (string, error) {
	path, err := root.reserveDirectory(worktreeDirectory, prefix)
	if err != nil {
		return "", err
	}
	if err := repo.AddDetachedWorktree(ctx, path, revision); err != nil {
		return "", operational("create regression proof worktree", err)
	}
	return path, nil
}

func executeProof(ctx context.Context, repo repository.Repository, commandRunner runner.OutputRunner, root *artifactHandle, worktree string, inputs proofInputs) (regressionProof, error) {
	applyDigest, err := applyAndRecord(ctx, root, worktree, inputs)
	if err != nil {
		return regressionProof{}, err
	}
	baseline, baselineErr := runAndRecord(ctx, repo, commandRunner, root, worktree, inputs.candidate, inputs.suite, inputs.id, ".baseline.log")
	if errors.Is(baselineErr, ErrCandidateChanged) {
		return regressionProof{}, baselineErr
	}
	if err := validateBaselineRun(ctx, baseline, baselineErr, inputs); err != nil {
		return regressionProof{}, err
	}
	candidate, candidateErr := runAndRecord(ctx, repo, commandRunner, root, repo.Root, inputs.candidate, inputs.suite, inputs.id, ".candidate.log")
	if errors.Is(candidateErr, ErrCandidateChanged) {
		return regressionProof{}, candidateErr
	}
	if err := validateCandidateRun(ctx, candidate, candidateErr, inputs.suite.Name); err != nil {
		return regressionProof{}, err
	}
	return regressionProof{
		Version: artifactVersion, ID: inputs.id, ReviewID: inputs.reviewID, ReviewBase: inputs.reviewBase, Base: inputs.base, Candidate: inputs.candidate,
		SelectionSHA256: inputs.selectionSHA256, DecisionSHA256: inputs.decisionSHA256, SelectedFeatures: selectedFeatureNames(inputs.selection), FullCandidate: inputs.selection.FullCandidate, Suite: inputs.suite.Name,
		Evidence: inputs.evidence, EvidencePatchSHA256: sha256Hex(inputs.patch), ExpectedRedExitStatus: inputs.expectedRedExitStatus,
		ApplyLogPath: artifactDisplayPath(proofArtifactName(inputs.id, ".apply.log")), ApplyLogSHA256: applyDigest,
		Baseline: baseline, CandidateExecution: candidate,
	}, nil
}

func applyAndRecord(ctx context.Context, root *artifactHandle, worktree string, inputs proofInputs) (string, error) {
	output, applyErr := applyEvidencePatch(ctx, worktree, inputs.patch)
	digest, err := writeProofArtifact(root, inputs.id, ".apply.log", hostLog(output, applyErr))
	if err != nil {
		return "", err
	}
	if applyErr != nil {
		return "", applyErr
	}
	return digest, nil
}

func runAndRecord(ctx context.Context, repo repository.Repository, commandRunner runner.OutputRunner, artifactRoot *artifactHandle, executionRoot, candidate string, suite policy.TestSuite, id, suffix string) (proofExecution, error) {
	result, output, runErr := runSuite(ctx, commandRunner, executionRoot, suite)
	digest, err := writeProofArtifact(artifactRoot, id, suffix, suiteLog(result, output, runErr))
	changedErr := ensureCandidateUnchanged(repo, candidate)
	if err != nil {
		return proofExecution{}, errors.Join(err, changedErr)
	}
	execution := proofExecution{ExitStatus: result.ExitStatus, LogPath: artifactDisplayPath(proofArtifactName(id, suffix)), LogSHA256: digest}
	return execution, errors.Join(runErr, changedErr)
}

func validateBaselineRun(ctx context.Context, execution proofExecution, runErr error, inputs proofInputs) error {
	if executionOperational(ctx, execution.ExitStatus, runErr) {
		return operational("run baseline regression suite", runErr)
	}
	if execution.ExitStatus != inputs.expectedRedExitStatus {
		return fmt.Errorf("%w: baseline suite %q exited with %d, expected %d", ErrInvalidEvidence, inputs.suite.Name, execution.ExitStatus, inputs.expectedRedExitStatus)
	}
	return nil
}

func validateCandidateRun(ctx context.Context, execution proofExecution, runErr error, suite string) error {
	if executionOperational(ctx, execution.ExitStatus, runErr) {
		return operational("run candidate regression suite", runErr)
	}
	if runErr != nil || execution.ExitStatus != 0 {
		return fmt.Errorf("%w: candidate suite %q did not exit successfully", ErrInvalidEvidence, suite)
	}
	return nil
}

func ensureCandidateUnchanged(repo repository.Repository, candidate string) error {
	current, err := repo.CleanHead()
	if err != nil {
		if errors.Is(err, repository.ErrDirtyCandidate) {
			return fmt.Errorf("%w: %v", ErrCandidateChanged, err)
		}
		return err
	}
	if current != candidate {
		return fmt.Errorf("%w: candidate commit changed from %s to %s", ErrCandidateChanged, candidate, current)
	}
	return nil
}

func applyEvidencePatch(ctx context.Context, worktree string, patch []byte) ([]byte, error) {
	if len(patch) == 0 {
		return []byte("evidence patch is empty; baseline already contains the declared evidence\n"), nil
	}
	output := &bytes.Buffer{}
	result, err := runner.Run(ctx, runner.HostCommand{
		Path: "git", Argv: []string{"git", "-C", worktree, "apply", "--binary", "--whitespace=nowarn", "-"},
		Environment: runner.Environment(nil), Stdin: bytes.NewReader(patch), Stdout: output, Stderr: output,
	})
	if err != nil || !result.Started || result.ExitStatus != 0 {
		return output.Bytes(), applyFailure(err, result.ExitStatus)
	}
	return output.Bytes(), nil
}

func applyFailure(err error, exitStatus int) error {
	if err == nil {
		err = fmt.Errorf("git apply exited with status %d", exitStatus)
	}
	return operational("apply candidate evidence patch", err)
}

func runSuite(ctx context.Context, commandRunner runner.OutputRunner, root string, suite policy.TestSuite) (runner.Result, runner.Output, error) {
	return commandRunner.RunWithOutput(ctx, root, suiteCommand(suite))
}

func suiteCommand(suite policy.TestSuite) policy.Command {
	return policy.Command{
		Name: suite.Name, Argv: append([]string{}, suite.Argv...), Cwd: suite.Cwd, Paths: append([]string{}, suite.Paths...),
		Modules: append([]string{}, suite.Modules...), RunOn: append([]string{}, suite.RunOn...), Environment: append([]string{}, suite.Environment...),
		ExclusiveResources: append([]string{}, suite.ExclusiveResources...), TimeoutSeconds: suite.TimeoutSeconds,
	}
}

func cloneCommand(command policy.Command) policy.Command {
	return policy.Command{
		Name: command.Name, Provides: slices.Clone(command.Provides), Argv: slices.Clone(command.Argv), Cwd: command.Cwd,
		Paths: slices.Clone(command.Paths), Modules: slices.Clone(command.Modules), RunOn: slices.Clone(command.RunOn),
		Environment: slices.Clone(command.Environment), ExclusiveResources: slices.Clone(command.ExclusiveResources), TimeoutSeconds: command.TimeoutSeconds,
		Managed: command.Managed, PassFiles: command.PassFiles, PassFilePaths: slices.Clone(command.PassFilePaths), SealedEnvironment: command.SealedEnvironment,
	}
}

func executionOperational(ctx context.Context, exitStatus int, err error) bool {
	if ctx.Err() != nil || exitStatus < 0 || exitStatus == 124 {
		return true
	}
	return err != nil && exitStatus == 0
}

func writeProofArtifact(root *artifactHandle, id, suffix string, data []byte) (string, error) {
	if err := ensureProofDirectory(root); err != nil {
		return "", err
	}
	if err := root.writeArtifactAtomic(proofArtifactName(id, suffix), data); err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func hostLog(output []byte, runErr error) []byte {
	data := &bytes.Buffer{}
	if runErr != nil {
		fmt.Fprintf(data, "error: %v\n", runErr)
	}
	data.Write(output)
	return data.Bytes()
}

func suiteLog(result runner.Result, output runner.Output, runErr error) []byte {
	data := &bytes.Buffer{}
	fmt.Fprintf(data, "exit_status: %d\n", result.ExitStatus)
	if runErr != nil {
		fmt.Fprintf(data, "error: %v\n", runErr)
	}
	data.WriteString("stdout:\n")
	data.Write(output.Stdout)
	data.WriteString("\nstderr:\n")
	data.Write(output.Stderr)
	return data.Bytes()
}

func ensureProofDirectory(root *artifactHandle) error {
	return root.ensureDirectory(proofDirectory, "regression proof")
}

func cleanupProofWorktree(repo repository.Repository, worktree string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := repo.RemoveWorktree(ctx, worktree); err != nil {
		return cleanup("remove regression proof worktree", err)
	}
	return nil
}

func clearedProofResult(previous, cleanupErr error) (ProveResult, error) {
	if previous == nil {
		return ProveResult{}, cleanupErr
	}
	return ProveResult{}, errors.Join(previous, cleanupErr)
}

func ordinarySuite(config policy.Config, name string) (policy.TestSuite, error) {
	if strings.TrimSpace(name) == "" {
		return policy.TestSuite{}, fmt.Errorf("%w: suite name is required", ErrInvalidInput)
	}
	matches := configuredSuites(config, name)
	if len(matches) != 1 {
		return policy.TestSuite{}, fmt.Errorf("%w: suite %q is not an exact configured suite", ErrInvalidInput, name)
	}
	if !ordinarySuiteAllowed(matches[0]) {
		return policy.TestSuite{}, fmt.Errorf("%w: suite %q is not ordinary non-credentialed evidence", ErrInvalidEvidence, name)
	}
	return matches[0], nil
}

func configuredSuites(config policy.Config, name string) []policy.TestSuite {
	matches := []policy.TestSuite{}
	for _, suite := range config.Tests.Suites {
		if suite.Name == name {
			matches = append(matches, suite)
		}
	}
	return matches
}

func ordinarySuiteAllowed(suite policy.TestSuite) bool {
	return policy.BehaviorReviewSuiteAllowed(suite)
}

func validateEvidence(repo repository.Repository, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("%w: at least one evidence path is required", ErrInvalidInput)
	}
	seen := map[string]bool{}
	evidence := make([]string, 0, len(values))
	for _, value := range values {
		normalized, err := validEvidencePath(repo, value, seen)
		if err != nil {
			return nil, err
		}
		seen[normalized] = true
		evidence = append(evidence, normalized)
	}
	sort.Strings(evidence)
	return evidence, nil
}

func validEvidencePath(repo repository.Repository, value string, seen map[string]bool) (string, error) {
	normalized, err := repo.NormalizePath(value)
	if err != nil || filepath.ToSlash(value) != normalized || seen[normalized] {
		return "", fmt.Errorf("%w: evidence path %q is not an exact contained path", ErrInvalidInput, value)
	}
	if err := evidenceFileIsRegular(repo, normalized); err != nil {
		return "", err
	}
	if repo.IsExcluded(normalized) || !repo.IsDevelopment(normalized) {
		return "", fmt.Errorf("%w: evidence path %q must be declared development or test evidence", ErrInvalidEvidence, normalized)
	}
	return normalized, nil
}

func evidenceFileIsRegular(repo repository.Repository, path string) error {
	info, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(path)))
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%w: evidence path %q must be a regular file", ErrInvalidInput, path)
	}
	if _, err := repo.Resolve(path); err != nil {
		return fmt.Errorf("%w: evidence path %q does not resolve inside the candidate", ErrInvalidInput, path)
	}
	return nil
}

func proofReferences(repo repository.Repository, root *artifactHandle, review ReviewResult, candidate string, packet reviewPacket) ([]ProofReference, error) {
	_, references, err := replayProofPhases(repo, root, review, candidate, packet)
	return references, err
}

func replayProofPhases(repo repository.Repository, root *artifactHandle, review ReviewResult, candidate string, packet reviewPacket) ([]ReplayProofPhase, []ProofReference, error) {
	ids := requestedProofIDs(review)
	phases := make([]ReplayProofPhase, 0, len(ids))
	references := make([]ProofReference, 0, len(ids))
	for _, id := range ids {
		proof, data, err := readProof(root, id)
		if err != nil {
			return nil, nil, err
		}
		suite, _, err := proofReplayMaterial(repo, proof, candidate, packet)
		if err != nil {
			return nil, nil, err
		}
		if err := verifyProofLogs(root, proof); err != nil {
			return nil, nil, err
		}
		if err := validateProofBehaviorScopes(proof, review, packet); err != nil {
			return nil, nil, err
		}
		digest := sha256Hex(data)
		command := suiteCommand(suite)
		phases = append(phases, ReplayProofPhase{ProofID: id, ProofSHA256: digest, Baseline: cloneCommand(command), Candidate: cloneCommand(command)})
		references = append(references, ProofReference{ID: id, SHA256: digest})
	}
	sort.Slice(phases, func(left, right int) bool { return phases[left].ProofID < phases[right].ProofID })
	sort.Slice(references, func(left, right int) bool { return references[left].ID < references[right].ID })
	return phases, references, nil
}

func readProof(root *artifactHandle, id string) (regressionProof, []byte, error) {
	if !validIdentifier(id) {
		return regressionProof{}, nil, fmt.Errorf("%w: proof identifier %q is malformed", ErrInvalidEvidence, id)
	}
	data, err := root.readArtifact(proofArtifactName(id, ".json"), maximumArtifactReadByte)
	if err != nil {
		return regressionProof{}, nil, fmt.Errorf("%w: proof %q is unavailable", ErrInvalidEvidence, id)
	}
	var proof regressionProof
	if err := decodeStrict(data, &proof); err != nil {
		return regressionProof{}, nil, fmt.Errorf("%w: proof %q JSON is invalid", ErrInvalidEvidence, id)
	}
	if err := validateProofStructure(proof); err != nil {
		return regressionProof{}, nil, err
	}
	if proof.ID != id {
		return regressionProof{}, nil, fmt.Errorf("%w: proof %q identifier does not match its artifact", ErrInvalidEvidence, id)
	}
	return proof, data, nil
}

func proofReplayMaterial(repo repository.Repository, proof regressionProof, candidate string, packet reviewPacket) (policy.TestSuite, []byte, error) {
	if proof.Candidate != candidate {
		return policy.TestSuite{}, nil, fmt.Errorf("%w: proof %q is bound to another candidate", ErrInvalidEvidence, proof.ID)
	}
	if proof.ReviewID != packet.ReviewID || proof.ReviewBase != packet.Base {
		return policy.TestSuite{}, nil, fmt.Errorf("%w: proof %q is not bound to the prepared review", ErrInvalidEvidence, proof.ID)
	}
	if proof.SelectionSHA256 != packet.SelectionSHA256 || proof.DecisionSHA256 != packet.DecisionSHA256 || proof.FullCandidate != packet.Selection.FullCandidate || !slices.Equal(proof.SelectedFeatures, selectedFeatureNames(packet.Selection)) {
		return policy.TestSuite{}, nil, fmt.Errorf("%w: proof %q does not match the selected behavior review features", ErrInvalidEvidence, proof.ID)
	}
	base, err := repo.ResolveAncestor(proof.Base)
	if err != nil {
		return policy.TestSuite{}, nil, fmt.Errorf("%w: proof %q base is unavailable", ErrInvalidEvidence, proof.ID)
	}
	if base == candidate {
		return policy.TestSuite{}, nil, fmt.Errorf("%w: proof %q has no pre-fix base", ErrInvalidEvidence, proof.ID)
	}
	if err := proofBaseMatchesReview(repo, packet.Base, base); err != nil {
		return policy.TestSuite{}, nil, err
	}
	suite, evidence, patch, err := proofMaterial(repo, base, candidate, ProveOptions{Suite: proof.Suite, Evidence: proof.Evidence}, packet)
	if err != nil {
		return policy.TestSuite{}, nil, err
	}
	if suite.Name != proof.Suite || !slices.Equal(evidence, proof.Evidence) || sha256Hex(patch) != proof.EvidencePatchSHA256 {
		return policy.TestSuite{}, nil, fmt.Errorf("%w: proof %q does not match current suite, evidence, or patch", ErrInvalidEvidence, proof.ID)
	}
	return suite, patch, nil
}

func validateProofStructure(proof regressionProof) error {
	if !validProofHeader(proof) {
		return fmt.Errorf("%w: proof fields are invalid", ErrInvalidEvidence)
	}
	if err := validateProofExecution(proof); err != nil {
		return err
	}
	if err := validateProofEvidence(proof.Evidence); err != nil {
		return err
	}
	if err := validateProofSelection(proof); err != nil {
		return err
	}
	return validateProofLogLocations(proof)
}

func validProofHeader(proof regressionProof) bool {
	return allValid(
		proof.Version == artifactVersion,
		validIdentifier(proof.ID),
		validIdentifier(proof.ReviewID),
		validRevision(proof.ReviewBase),
		validRevision(proof.Base),
		validRevision(proof.Candidate),
		validSHA256(proof.SelectionSHA256),
		validSHA256(proof.DecisionSHA256),
		validIdentifier(proof.Suite),
		validSHA256(proof.EvidencePatchSHA256),
		proof.ExpectedRedExitStatus >= 1,
		proof.ExpectedRedExitStatus <= 255,
	)
}

func validateProofBehaviorScopes(proof regressionProof, review ReviewResult, packet reviewPacket) error {
	for _, behavior := range review.Behaviors {
		if behavior.Classification != "requested" || !slices.Contains(behavior.ProofIDs, proof.ID) {
			continue
		}
		if !scopeAllowsSuite(behavior.Scope, packet.Selection, proof.Suite) {
			return fmt.Errorf("%w: proof %q suite %q is not allowed by its behavior scope", ErrInvalidEvidence, proof.ID, proof.Suite)
		}
	}
	return nil
}

func validateProofSelection(proof regressionProof) error {
	if proof.SelectedFeatures == nil || (!proof.FullCandidate && len(proof.SelectedFeatures) == 0) {
		return fmt.Errorf("%w: proof selected feature scope is invalid", ErrInvalidEvidence)
	}
	previous := ""
	for _, feature := range proof.SelectedFeatures {
		if !validFeatureName(feature) || feature <= previous {
			return fmt.Errorf("%w: proof selected feature scope is invalid", ErrInvalidEvidence)
		}
		previous = feature
	}
	return nil
}

func validateProofExecution(proof regressionProof) error {
	if proof.Baseline.ExitStatus != proof.ExpectedRedExitStatus || proof.CandidateExecution.ExitStatus != 0 {
		return fmt.Errorf("%w: proof exit statuses are invalid", ErrInvalidEvidence)
	}
	if !allValid(validSHA256(proof.Baseline.LogSHA256), validSHA256(proof.CandidateExecution.LogSHA256)) {
		return fmt.Errorf("%w: proof execution log digests are invalid", ErrInvalidEvidence)
	}
	return nil
}

func validateProofEvidence(evidence []string) error {
	if len(evidence) == 0 {
		return fmt.Errorf("%w: proof evidence is empty", ErrInvalidEvidence)
	}
	previous := ""
	for _, path := range evidence {
		if !canonicalProofPath(path) || path <= previous {
			return fmt.Errorf("%w: proof evidence paths are invalid", ErrInvalidEvidence)
		}
		previous = path
	}
	return nil
}

func canonicalProofPath(path string) bool {
	return allValid(path != "", !filepath.IsAbs(path), filepath.ToSlash(path) == path, !strings.Contains(path, "\\"), !strings.HasPrefix(path, "../"), !strings.Contains(path, "/../"))
}

func validateProofLogLocations(proof regressionProof) error {
	if !allValid(validSHA256(proof.ApplyLogSHA256), proof.ApplyLogPath == artifactDisplayPath(proofArtifactName(proof.ID, ".apply.log"))) {
		return fmt.Errorf("%w: proof apply log is invalid", ErrInvalidEvidence)
	}
	if !executionLogLocation(proof.Baseline, proof.ID, ".baseline.log") || !executionLogLocation(proof.CandidateExecution, proof.ID, ".candidate.log") {
		return fmt.Errorf("%w: proof execution log paths are invalid", ErrInvalidEvidence)
	}
	return nil
}

func executionLogLocation(execution proofExecution, id, suffix string) bool {
	return execution.LogPath == artifactDisplayPath(proofArtifactName(id, suffix))
}

func verifyProofLogs(root *artifactHandle, proof regressionProof) error {
	if err := verifyProofLog(root, proof.ApplyLogPath, proof.ApplyLogSHA256); err != nil {
		return err
	}
	if err := verifyProofLog(root, proof.Baseline.LogPath, proof.Baseline.LogSHA256); err != nil {
		return err
	}
	return verifyProofLog(root, proof.CandidateExecution.LogPath, proof.CandidateExecution.LogSHA256)
}

func verifyProofLog(root *artifactHandle, path, digest string) error {
	data, err := root.readArtifact(strings.TrimPrefix(path, artifactDirectory+"/"), maximumArtifactReadByte)
	if err != nil || sha256Hex(data) != digest {
		return fmt.Errorf("%w: proof log does not match its digest", ErrInvalidEvidence)
	}
	return nil
}
