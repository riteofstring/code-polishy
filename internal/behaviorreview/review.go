package behaviorreview

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const artifactVersion = 1

type packetDesignDocument struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type reviewPacket struct {
	Version         int                    `json:"version"`
	ReviewID        string                 `json:"review_id"`
	Base            string                 `json:"base"`
	Candidate       string                 `json:"candidate"`
	Intent          string                 `json:"intent"`
	IntentSHA256    string                 `json:"intent_sha256"`
	Patch           string                 `json:"patch"`
	PatchSHA256     string                 `json:"patch_sha256"`
	DesignDocuments []packetDesignDocument `json:"design_documents"`
	Instructions    string                 `json:"instructions"`
	ResultPath      string                 `json:"result_path"`
	ProofDirectory  string                 `json:"proof_directory"`
}

type Behavior struct {
	Before         string   `json:"before"`
	After          string   `json:"after"`
	Classification string   `json:"classification"`
	ProofIDs       []string `json:"proof_ids"`
}

type ReviewResult struct {
	Version      int        `json:"version"`
	ReviewID     string     `json:"review_id"`
	Base         string     `json:"base"`
	Candidate    string     `json:"candidate"`
	IntentSHA256 string     `json:"intent_sha256"`
	Behaviors    []Behavior `json:"behaviors"`
	Findings     []string   `json:"findings"`
}

type prepareInputs struct {
	base         string
	candidate    string
	intent       []byte
	instructions []byte
	patch        []byte
	documents    []packetDesignDocument
	reviewID     string
}

type finalizationState struct {
	root       string
	base       string
	candidate  string
	packet     reviewPacket
	packetData []byte
	review     ReviewResult
	reviewData []byte
	proofs     []ProofReference
}

type receiptState struct {
	root      string
	base      string
	candidate string
	receipt   MergeReceipt
}

func prepare(ctx context.Context, repo repository.Repository, options PrepareOptions) (PrepareResult, error) {
	inputs, err := collectPrepareInputs(reviewContext(ctx), repo, options)
	if err != nil {
		return PrepareResult{}, err
	}
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		return PrepareResult{}, err
	}
	packet, err := packetFromInputs(inputs)
	if err != nil {
		return PrepareResult{}, err
	}
	data, err := marshalArtifact(packet)
	if err != nil {
		return PrepareResult{}, err
	}
	if err := writeArtifactAtomic(artifactPath(root, packetFilename), data); err != nil {
		return PrepareResult{}, err
	}
	return PrepareResult{ReviewID: inputs.reviewID, Base: inputs.base, Candidate: inputs.candidate, IntentSHA256: packet.IntentSHA256, PacketPath: artifactDisplayPath(packetFilename)}, nil
}

func collectPrepareInputs(ctx context.Context, repo repository.Repository, options PrepareOptions) (prepareInputs, error) {
	if err := validatePrepareRequest(ctx, options); err != nil {
		return prepareInputs{}, err
	}
	base, candidate, err := cleanCandidateAtMergeBase(repo, options.Base)
	if err != nil {
		return prepareInputs{}, err
	}
	intent, instructions, err := prepareTextInputs(repo, options.IntentPath)
	if err != nil {
		return prepareInputs{}, err
	}
	patch, documents, err := prepareCandidateMaterial(repo, base, candidate)
	if err != nil {
		return prepareInputs{}, err
	}
	reviewID, err := newReviewID()
	if err != nil {
		return prepareInputs{}, operational("generate behavior review identifier", err)
	}
	return prepareInputs{base: base, candidate: candidate, intent: intent, instructions: instructions, patch: patch, documents: documents, reviewID: reviewID}, nil
}

func validatePrepareRequest(ctx context.Context, options PrepareOptions) error {
	if err := ctx.Err(); err != nil {
		return operational("prepare behavior review", err)
	}
	if strings.TrimSpace(options.IntentPath) == "" {
		return fmt.Errorf("%w: intent path is required", ErrInvalidInput)
	}
	return nil
}

func cleanCandidateAtMergeBase(repo repository.Repository, reference string) (string, string, error) {
	candidate, err := repo.CleanHead()
	if err != nil {
		return "", "", err
	}
	base, err := repo.MergeBase(reference)
	if err != nil {
		return "", "", err
	}
	return base, candidate, nil
}

func prepareTextInputs(repo repository.Repository, intentPath string) ([]byte, []byte, error) {
	intent, err := readRegularUTF8(intentPath, maximumIntentBytes, true, "intent file")
	if err != nil {
		return nil, nil, err
	}
	instructions, err := readCanonicalInstructions(repo)
	if err != nil {
		return nil, nil, err
	}
	return intent, instructions, nil
}

func prepareCandidateMaterial(repo repository.Repository, base, candidate string) ([]byte, []packetDesignDocument, error) {
	selection, err := repo.SelectBase(base)
	if err != nil {
		return nil, nil, operational("select behavior review candidate", err)
	}
	patch, err := repo.Patch(base, candidate, nil)
	if err != nil {
		return nil, nil, operational("build behavior review patch", err)
	}
	documents, err := readCurrentDesignDocuments(repo, selection.Candidate.Paths())
	if err != nil {
		return nil, nil, err
	}
	return patch, documents, nil
}

func packetFromInputs(inputs prepareInputs) (reviewPacket, error) {
	if !utf8.Valid(inputs.patch) {
		return reviewPacket{}, fmt.Errorf("%w: Git binary patch is not readable UTF-8", ErrOperational)
	}
	return reviewPacket{
		Version: artifactVersion, ReviewID: inputs.reviewID, Base: inputs.base, Candidate: inputs.candidate,
		Intent: string(inputs.intent), IntentSHA256: sha256Hex(inputs.intent), Patch: string(inputs.patch), PatchSHA256: sha256Hex(inputs.patch),
		DesignDocuments: inputs.documents, Instructions: string(inputs.instructions), ResultPath: artifactDisplayPath(defaultResultFilename), ProofDirectory: artifactDisplayPath(proofDirectory),
	}, nil
}

func readCanonicalInstructions(repo repository.Repository) ([]byte, error) {
	path := filepath.Join(repo.PolicyRoot, "templates", "behavior-review.md")
	return readRegularUTF8(path, maximumTemplateBytes, true, "behavior review instructions")
}

func newReviewID() (string, error) {
	data := make([]byte, 16)
	if _, err := cryptorand.Read(data); err != nil {
		return "", err
	}
	return "review-" + fmt.Sprintf("%x", data), nil
}

func finalize(ctx context.Context, repo repository.Repository, options FinalizeOptions) (FinalizeResult, error) {
	state, err := loadFinalization(reviewContext(ctx), repo, options)
	if err != nil {
		return FinalizeResult{}, err
	}
	if err := writeFinalizedReview(state); err != nil {
		return FinalizeResult{}, err
	}
	if err := writeMergeReceipt(state); err != nil {
		return FinalizeResult{}, err
	}
	return FinalizeResult{ReviewID: state.review.ReviewID, Base: state.base, Candidate: state.candidate, ReceiptPath: artifactDisplayPath(receiptFilename)}, nil
}

func loadFinalization(ctx context.Context, repo repository.Repository, options FinalizeOptions) (finalizationState, error) {
	root, base, candidate, err := finalizationIdentity(ctx, repo, options.Base)
	if err != nil {
		return finalizationState{}, err
	}
	packet, packetData, err := preparedPacketFor(root, base, candidate)
	if err != nil {
		return finalizationState{}, err
	}
	review, reviewData, err := finalizedReviewInput(root, options.ResultPath, packet, candidate)
	if err != nil {
		return finalizationState{}, err
	}
	proofs, err := proofReferences(repo, root, requestedProofIDs(review), candidate)
	if err != nil {
		return finalizationState{}, err
	}
	return finalizationState{root: root, base: base, candidate: candidate, packet: packet, packetData: packetData, review: review, reviewData: reviewData, proofs: proofs}, nil
}

func finalizationIdentity(ctx context.Context, repo repository.Repository, reference string) (string, string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", "", operational("finalize behavior review", err)
	}
	base, candidate, err := cleanCandidateAtMergeBase(repo, reference)
	if err != nil {
		return "", "", "", err
	}
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		return "", "", "", err
	}
	return root, base, candidate, nil
}

func preparedPacketFor(root, base, candidate string) (reviewPacket, []byte, error) {
	packet, data, err := readPacket(root)
	if err != nil {
		return reviewPacket{}, nil, err
	}
	if packet.Base != base || packet.Candidate != candidate {
		return reviewPacket{}, nil, fmt.Errorf("%w: prepared packet does not match the current base and candidate", ErrStaleReview)
	}
	return packet, data, nil
}

func finalizedReviewInput(root, resultPath string, packet reviewPacket, candidate string) (ReviewResult, []byte, error) {
	data, err := readRegularUTF8(reviewResultPath(root, resultPath), maximumResultBytes, true, "behavior review result")
	if err != nil {
		return ReviewResult{}, nil, err
	}
	var review ReviewResult
	if err := decodeStrict(data, &review); err != nil {
		return ReviewResult{}, nil, err
	}
	if err := validateReviewResult(review); err != nil {
		return ReviewResult{}, nil, err
	}
	if !reviewMatchesPacket(review, packet, candidate) {
		return ReviewResult{}, nil, fmt.Errorf("%w: result identifiers do not match the prepared packet", ErrStaleReview)
	}
	if err := rejectBlockingReview(review); err != nil {
		return ReviewResult{}, nil, err
	}
	return review, data, nil
}

func reviewResultPath(root, path string) string {
	if path == "" {
		return artifactPath(root, defaultResultFilename)
	}
	return path
}

func reviewMatchesPacket(review ReviewResult, packet reviewPacket, candidate string) bool {
	return allValid(
		review.ReviewID == packet.ReviewID,
		review.Base == packet.Base,
		review.Candidate == candidate,
		review.IntentSHA256 == packet.IntentSHA256,
	)
}

func writeFinalizedReview(state finalizationState) error {
	return writeArtifactAtomic(artifactPath(state.root, finalReviewFilename), state.reviewData)
}

func writeMergeReceipt(state finalizationState) error {
	receipt := MergeReceipt{
		Version: artifactVersion, ReviewID: state.review.ReviewID, Base: state.base, Candidate: state.candidate,
		IntentSHA256: state.packet.IntentSHA256, PacketSHA256: sha256Hex(state.packetData), ReviewSHA256: sha256Hex(state.reviewData), Proofs: state.proofs,
	}
	data, err := marshalArtifact(receipt)
	if err != nil {
		return err
	}
	return writeArtifactAtomic(artifactPath(state.root, receiptFilename), data)
}

func validateMergeReceipt(ctx context.Context, repo repository.Repository, options ValidateMergeReceiptOptions) (MergeReceipt, error) {
	state, err := currentReceiptState(reviewContext(ctx), repo, options.Base)
	if err != nil {
		return MergeReceipt{}, err
	}
	packet, err := validateReceiptPacket(state)
	if err != nil {
		return MergeReceipt{}, err
	}
	review, err := validateReceiptReview(state, packet)
	if err != nil {
		return MergeReceipt{}, err
	}
	if err := validateReceiptProofs(repo, state, review); err != nil {
		return MergeReceipt{}, err
	}
	return state.receipt, nil
}

func currentReceiptState(ctx context.Context, repo repository.Repository, reference string) (receiptState, error) {
	if err := ctx.Err(); err != nil {
		return receiptState{}, operational("validate behavior review receipt", err)
	}
	base, candidate, err := cleanCandidateAtMergeBase(repo, reference)
	if err != nil {
		return receiptState{}, err
	}
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		return receiptState{}, err
	}
	receipt, err := readReceipt(root)
	if err != nil {
		return receiptState{}, err
	}
	if err := validateReceipt(receipt); err != nil {
		return receiptState{}, staleReceipt("receipt structure is invalid", err)
	}
	if receipt.Base != base || receipt.Candidate != candidate {
		return receiptState{}, staleReceipt("receipt does not match the current base and candidate", nil)
	}
	return receiptState{root: root, base: base, candidate: candidate, receipt: receipt}, nil
}

func validateReceiptPacket(state receiptState) (reviewPacket, error) {
	packet, data, err := readPacket(state.root)
	if err != nil {
		return reviewPacket{}, staleReceipt("prepared packet is unavailable", err)
	}
	if !packetMatchesReceipt(packet, data, state) {
		return reviewPacket{}, staleReceipt("prepared packet does not match the receipt", nil)
	}
	return packet, nil
}

func packetMatchesReceipt(packet reviewPacket, data []byte, state receiptState) bool {
	return allValid(
		sha256Hex(data) == state.receipt.PacketSHA256,
		packet.ReviewID == state.receipt.ReviewID,
		packet.Base == state.base,
		packet.Candidate == state.candidate,
		packet.IntentSHA256 == state.receipt.IntentSHA256,
	)
}

func validateReceiptReview(state receiptState, packet reviewPacket) (ReviewResult, error) {
	data, err := readArtifact(artifactPath(state.root, finalReviewFilename), maximumResultBytes)
	if err != nil {
		return ReviewResult{}, staleReceipt("finalized review is unavailable", err)
	}
	if sha256Hex(data) != state.receipt.ReviewSHA256 {
		return ReviewResult{}, staleReceipt("finalized review digest does not match the receipt", nil)
	}
	var review ReviewResult
	if err := decodeStrict(data, &review); err != nil {
		return ReviewResult{}, staleReceipt("finalized review is invalid", err)
	}
	if err := validateReviewResult(review); err != nil {
		return ReviewResult{}, staleReceipt("finalized review is invalid", err)
	}
	if !reviewMatchesPacket(review, packet, state.candidate) {
		return ReviewResult{}, staleReceipt("finalized review identifiers do not match the packet", nil)
	}
	if err := rejectBlockingReview(review); err != nil {
		return ReviewResult{}, staleReceipt("finalized review has unresolved findings", err)
	}
	return review, nil
}

func validateReceiptProofs(repo repository.Repository, state receiptState, review ReviewResult) error {
	proofs, err := proofReferences(repo, state.root, requestedProofIDs(review), state.candidate)
	if err != nil {
		return staleReceipt("proofs do not validate", err)
	}
	if !slices.Equal(proofs, state.receipt.Proofs) {
		return staleReceipt("proof references do not match the receipt", nil)
	}
	return nil
}

func readPacket(root string) (reviewPacket, []byte, error) {
	data, err := readArtifact(artifactPath(root, packetFilename), maximumArtifactReadByte)
	if err != nil {
		return reviewPacket{}, nil, fmt.Errorf("%w: prepared behavior review packet is unavailable: %v", ErrInvalidReview, err)
	}
	var packet reviewPacket
	if err := decodeStrict(data, &packet); err != nil {
		return reviewPacket{}, nil, err
	}
	if err := validatePacket(packet); err != nil {
		return reviewPacket{}, nil, err
	}
	return packet, data, nil
}

func readReceipt(root string) (MergeReceipt, error) {
	data, err := readArtifact(artifactPath(root, receiptFilename), maximumArtifactReadByte)
	if err != nil {
		return MergeReceipt{}, fmt.Errorf("%w: behavior review receipt is unavailable", ErrMissingReceipt)
	}
	var receipt MergeReceipt
	if err := decodeStrict(data, &receipt); err != nil {
		return MergeReceipt{}, staleReceipt("receipt JSON is invalid", err)
	}
	return receipt, nil
}

func validatePacket(packet reviewPacket) error {
	if packet.Version != artifactVersion {
		return fmt.Errorf("%w: packet version must be %d", ErrInvalidReview, artifactVersion)
	}
	if !validPacketIdentifiers(packet) {
		return fmt.Errorf("%w: packet identifiers are malformed", ErrInvalidReview)
	}
	if err := validatePacketIntent(packet); err != nil {
		return err
	}
	if err := validatePacketPatch(packet); err != nil {
		return err
	}
	if err := validatePacketInstructions(packet); err != nil {
		return err
	}
	return validatePacketDocuments(packet.DesignDocuments)
}

func validPacketIdentifiers(packet reviewPacket) bool {
	return allValid(validIdentifier(packet.ReviewID), validRevision(packet.Base), validRevision(packet.Candidate))
}

func validatePacketIntent(packet reviewPacket) error {
	if strings.TrimSpace(packet.Intent) == "" || !utf8.ValidString(packet.Intent) {
		return fmt.Errorf("%w: packet intent is invalid", ErrInvalidReview)
	}
	if !validSHA256(packet.IntentSHA256) || sha256Hex([]byte(packet.Intent)) != packet.IntentSHA256 {
		return fmt.Errorf("%w: packet intent digest is invalid", ErrInvalidReview)
	}
	return nil
}

func validatePacketPatch(packet reviewPacket) error {
	if !utf8.ValidString(packet.Patch) {
		return fmt.Errorf("%w: packet patch is not UTF-8", ErrInvalidReview)
	}
	if !validSHA256(packet.PatchSHA256) || sha256Hex([]byte(packet.Patch)) != packet.PatchSHA256 {
		return fmt.Errorf("%w: packet patch digest is invalid", ErrInvalidReview)
	}
	return nil
}

func validatePacketInstructions(packet reviewPacket) error {
	if strings.TrimSpace(packet.Instructions) == "" || !utf8.ValidString(packet.Instructions) {
		return fmt.Errorf("%w: packet instructions are invalid", ErrInvalidReview)
	}
	if packet.ResultPath != artifactDisplayPath(defaultResultFilename) || packet.ProofDirectory != artifactDisplayPath(proofDirectory) {
		return fmt.Errorf("%w: packet artifact paths are invalid", ErrInvalidReview)
	}
	return nil
}

func validatePacketDocuments(documents []packetDesignDocument) error {
	seen := map[string]bool{}
	for _, document := range documents {
		if err := validatePacketDocument(document, seen); err != nil {
			return err
		}
		seen[document.Path] = true
	}
	return nil
}

func validatePacketDocument(document packetDesignDocument, seen map[string]bool) error {
	if document.Path == "" || seen[document.Path] || !utf8.ValidString(document.Content) {
		return fmt.Errorf("%w: packet design documents are invalid", ErrInvalidReview)
	}
	if !validSHA256(document.SHA256) || sha256Hex([]byte(document.Content)) != document.SHA256 {
		return fmt.Errorf("%w: packet design document digest is invalid", ErrInvalidReview)
	}
	return nil
}

func validateReviewResult(review ReviewResult) error {
	if err := validateReviewHeader(review); err != nil {
		return err
	}
	if len(review.Behaviors) == 0 {
		return fmt.Errorf("%w: result must contain at least one behavior", ErrInvalidReview)
	}
	if review.Behaviors == nil || review.Findings == nil {
		return fmt.Errorf("%w: result must explicitly contain behaviors and findings arrays", ErrInvalidReview)
	}
	if err := validateFindings(review.Findings); err != nil {
		return err
	}
	return validateBehaviors(review.Behaviors)
}

func validateReviewHeader(review ReviewResult) error {
	if review.Version != artifactVersion {
		return fmt.Errorf("%w: result version must be %d", ErrInvalidReview, artifactVersion)
	}
	if !allValid(validIdentifier(review.ReviewID), validRevision(review.Base), validRevision(review.Candidate), validSHA256(review.IntentSHA256)) {
		return fmt.Errorf("%w: result identifiers are malformed", ErrInvalidReview)
	}
	return nil
}

func validateFindings(findings []string) error {
	for _, finding := range findings {
		if strings.TrimSpace(finding) == "" || !utf8.ValidString(finding) {
			return fmt.Errorf("%w: result findings must be non-empty UTF-8 strings", ErrInvalidReview)
		}
	}
	return nil
}

func validateBehaviors(behaviors []Behavior) error {
	for index, behavior := range behaviors {
		if err := validateBehavior(behavior, index+1); err != nil {
			return err
		}
	}
	return nil
}

func validateBehavior(behavior Behavior, index int) error {
	if !validBehaviorDescription(behavior) {
		return fmt.Errorf("%w: behavior %d must contain non-empty before and after values", ErrInvalidReview, index)
	}
	if err := validateBehaviorProofIDs(behavior, index); err != nil {
		return err
	}
	if behavior.Classification == "requested" && len(behavior.ProofIDs) == 0 {
		return fmt.Errorf("%w: requested behavior %d needs at least one proof identifier", ErrInvalidReview, index)
	}
	if !validClassification(behavior.Classification) {
		return fmt.Errorf("%w: behavior %d has an unknown classification", ErrInvalidReview, index)
	}
	return nil
}

func validBehaviorDescription(behavior Behavior) bool {
	return allValid(strings.TrimSpace(behavior.Before) != "", strings.TrimSpace(behavior.After) != "", utf8.ValidString(behavior.Before), utf8.ValidString(behavior.After))
}

func validateBehaviorProofIDs(behavior Behavior, index int) error {
	if behavior.ProofIDs == nil {
		return fmt.Errorf("%w: behavior %d must explicitly contain a proof_ids array", ErrInvalidReview, index)
	}
	seen := map[string]bool{}
	for _, id := range behavior.ProofIDs {
		if !validIdentifier(id) || seen[id] {
			return fmt.Errorf("%w: behavior %d has invalid proof identifiers", ErrInvalidReview, index)
		}
		seen[id] = true
	}
	return nil
}

func validClassification(value string) bool {
	return slices.Contains([]string{"requested", "preserved", "unintended", "unknown"}, value)
}

func rejectBlockingReview(review ReviewResult) error {
	if len(review.Findings) > 0 {
		return fmt.Errorf("%w: reviewer reported %d unresolved finding(s)", ErrInvalidReview, len(review.Findings))
	}
	for _, behavior := range review.Behaviors {
		if behavior.Classification == "unintended" || behavior.Classification == "unknown" {
			return fmt.Errorf("%w: reviewer classified a behavior as %s", ErrInvalidReview, behavior.Classification)
		}
	}
	return nil
}

func requestedProofIDs(review ReviewResult) []string {
	set := map[string]bool{}
	for _, behavior := range review.Behaviors {
		if behavior.Classification != "requested" {
			continue
		}
		for _, id := range behavior.ProofIDs {
			set[id] = true
		}
	}
	result := make([]string, 0, len(set))
	for id := range set {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func validateReceipt(receipt MergeReceipt) error {
	if !validReceiptHeader(receipt) {
		return errors.New("receipt fields are malformed")
	}
	if receipt.Proofs == nil {
		return errors.New("receipt proof references are missing")
	}
	return validateReceiptProofReferences(receipt.Proofs)
}

func validReceiptHeader(receipt MergeReceipt) bool {
	return allValid(
		receipt.Version == artifactVersion,
		validIdentifier(receipt.ReviewID),
		validRevision(receipt.Base),
		validRevision(receipt.Candidate),
		validSHA256(receipt.IntentSHA256),
		validSHA256(receipt.PacketSHA256),
		validSHA256(receipt.ReviewSHA256),
	)
}

func validateReceiptProofReferences(proofs []ProofReference) error {
	seen := map[string]bool{}
	for _, proof := range proofs {
		if !validProofReference(proof, seen) {
			return errors.New("receipt proof references are malformed")
		}
		seen[proof.ID] = true
	}
	if !sort.SliceIsSorted(proofs, func(left, right int) bool { return proofs[left].ID < proofs[right].ID }) {
		return errors.New("receipt proof references are not sorted")
	}
	return nil
}

func validProofReference(proof ProofReference, seen map[string]bool) bool {
	return allValid(validIdentifier(proof.ID), validSHA256(proof.SHA256), !seen[proof.ID])
}

func validIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 80 {
		return false
	}
	if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", rune(value[0])) {
		return false
	}
	return identifierTailIsValid(value[1:])
}

func identifierTailIsValid(value string) bool {
	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
	for _, character := range value {
		if !strings.ContainsRune(allowed, character) {
			return false
		}
	}
	return true
}

func validRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	return hexadecimal(value)
}

func validSHA256(value string) bool {
	return len(value) == 64 && hexadecimal(value)
}

func hexadecimal(value string) bool {
	const hexadecimalCharacters = "0123456789abcdefABCDEF"
	for _, character := range value {
		if !strings.ContainsRune(hexadecimalCharacters, character) {
			return false
		}
	}
	return true
}

func allValid(values ...bool) bool {
	for _, value := range values {
		if !value {
			return false
		}
	}
	return true
}

func staleReceipt(message string, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: %s", ErrStaleReceipt, message)
	}
	return fmt.Errorf("%w: %s: %v", ErrStaleReceipt, message, cause)
}

func reviewContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
