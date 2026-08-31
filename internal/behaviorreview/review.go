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

const artifactVersion = 3

const prepareMarkerFilename = "prepare.json"

type packetDesignDocument struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type reviewPacket struct {
	Version           int                    `json:"version"`
	ReviewID          string                 `json:"review_id"`
	Base              string                 `json:"base"`
	Candidate         string                 `json:"candidate"`
	Intents           []intentCapture        `json:"intents"`
	IntentSHA256      string                 `json:"intent_sha256"`
	TaskRequirements  []TaskRequirement      `json:"task_requirements"`
	RequirementSHA256 string                 `json:"requirement_sha256"`
	Selection         ReviewSelection        `json:"selection"`
	SelectionSHA256   string                 `json:"selection_sha256"`
	DecisionSHA256    string                 `json:"decision_sha256"`
	Patch             string                 `json:"patch"`
	PatchSHA256       string                 `json:"patch_sha256"`
	DesignDocuments   []packetDesignDocument `json:"design_documents"`
	Instructions      string                 `json:"instructions"`
	ResultPath        string                 `json:"result_path"`
	ProofDirectory    string                 `json:"proof_directory"`
}

type prepareMarker struct {
	Version           int    `json:"version"`
	ReviewID          string `json:"review_id"`
	Base              string `json:"base"`
	Candidate         string `json:"candidate"`
	SelectionSHA256   string `json:"selection_sha256"`
	RequirementSHA256 string `json:"requirement_sha256"`
	DecisionSHA256    string `json:"decision_sha256"`
	PacketSHA256      string `json:"packet_sha256"`
}

type Behavior struct {
	Before         string        `json:"before"`
	After          string        `json:"after"`
	Classification string        `json:"classification"`
	ProofIDs       []string      `json:"proof_ids"`
	Scope          BehaviorScope `json:"scope"`
}

type ReviewResult struct {
	Version         int        `json:"version"`
	ReviewID        string     `json:"review_id"`
	Base            string     `json:"base"`
	Candidate       string     `json:"candidate"`
	IntentSHA256    string     `json:"intent_sha256"`
	SelectionSHA256 string     `json:"selection_sha256"`
	DecisionSHA256  string     `json:"decision_sha256"`
	Behaviors       []Behavior `json:"behaviors"`
	Findings        []string   `json:"findings"`
}

type prepareInputs struct {
	base              string
	candidate         string
	intents           []intentCapture
	intentSHA256      string
	requirements      []TaskRequirement
	requirementSHA256 string
	selection         ReviewSelection
	selectionSHA256   string
	decisionSHA256    string
	instructions      []byte
	patch             []byte
	documents         []packetDesignDocument
	reviewID          string
}

type finalizationState struct {
	root       *artifactHandle
	base       string
	candidate  string
	packet     reviewPacket
	packetData []byte
	markerData []byte
	review     ReviewResult
	reviewData []byte
	proofs     []ProofReference
}

type preparedPacketState struct {
	packet     reviewPacket
	packetData []byte
	markerData []byte
}

type receiptState struct {
	root      *artifactHandle
	base      string
	candidate string
	selection ReviewSelection
	receipt   GateReceipt
}

func prepare(ctx context.Context, repo repository.Repository, options PrepareOptions) (PrepareResult, error) {
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		return PrepareResult{}, err
	}
	defer root.Close()
	inputs, err := collectPrepareInputs(reviewContext(ctx), repo, root, options)
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
	if err := root.writeArtifactAtomic(packetFilename, data); err != nil {
		return PrepareResult{}, err
	}
	if err := writePrepareMarker(root, packet, data); err != nil {
		return PrepareResult{}, err
	}
	return PrepareResult{
		ReviewID: inputs.reviewID, Base: inputs.base, Candidate: inputs.candidate, IntentSHA256: packet.IntentSHA256,
		SelectionSHA256: packet.SelectionSHA256, RequirementSHA256: packet.RequirementSHA256, DecisionSHA256: packet.DecisionSHA256,
		Selection:  cloneReviewSelection(packet.Selection),
		PacketPath: artifactDisplayPath(packetFilename),
	}, nil
}

func writePrepareMarker(root *artifactHandle, packet reviewPacket, packetData []byte) error {
	marker := prepareMarker{
		Version: artifactVersion, ReviewID: packet.ReviewID, Base: packet.Base, Candidate: packet.Candidate,
		SelectionSHA256: packet.SelectionSHA256, RequirementSHA256: packet.RequirementSHA256, DecisionSHA256: packet.DecisionSHA256, PacketSHA256: sha256Hex(packetData),
	}
	data, err := marshalArtifact(marker)
	if err != nil {
		return err
	}
	return root.writeArtifactAtomic(prepareMarkerFilename, data)
}

func collectPrepareInputs(ctx context.Context, repo repository.Repository, root *artifactHandle, options PrepareOptions) (prepareInputs, error) {
	if err := validatePrepareRequest(ctx, options); err != nil {
		return prepareInputs{}, err
	}
	selection, err := NormalizeReviewSelection(options.Selection)
	if err != nil {
		return prepareInputs{}, err
	}
	selectionSHA256, err := SelectionSHA256(selection)
	if err != nil {
		return prepareInputs{}, err
	}
	base, candidate, err := cleanCandidateAtMergeBase(repo, options.Base)
	if err != nil {
		return prepareInputs{}, err
	}
	journal, err := readIntentJournal(repo, root, false)
	if err != nil {
		return prepareInputs{}, err
	}
	intents, intentSHA256, err := selectIntentCaptures(repo, journal, base, candidate)
	if err != nil {
		return prepareInputs{}, err
	}
	requirements, err := taskRequirementsFor(repo, journal, base, candidate)
	if err != nil {
		return prepareInputs{}, err
	}
	if err := validateSelectionIncludesFeatures(selection, requirements.RequestedFeatures); err != nil {
		return prepareInputs{}, err
	}
	decisionSHA256, err := DecisionBindingSHA256(selection, requirements)
	if err != nil {
		return prepareInputs{}, err
	}
	instructions, err := readCanonicalInstructions(repo)
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
	return prepareInputs{
		base: base, candidate: candidate, intents: intents, intentSHA256: intentSHA256,
		requirements: requirements.Requirements, requirementSHA256: requirements.SHA256, selection: selection, selectionSHA256: selectionSHA256,
		decisionSHA256: decisionSHA256,
		instructions:   instructions, patch: patch, documents: documents, reviewID: reviewID,
	}, nil
}

func validatePrepareRequest(ctx context.Context, options PrepareOptions) error {
	if err := ctx.Err(); err != nil {
		return operational("prepare behavior review", err)
	}
	if strings.TrimSpace(options.Base) == "" {
		return fmt.Errorf("%w: base is required", ErrInvalidInput)
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
		Intents: append([]intentCapture{}, inputs.intents...), IntentSHA256: inputs.intentSHA256,
		TaskRequirements: cloneTaskRequirements(inputs.requirements), RequirementSHA256: inputs.requirementSHA256,
		Selection: cloneReviewSelection(inputs.selection), SelectionSHA256: inputs.selectionSHA256, DecisionSHA256: inputs.decisionSHA256,
		Patch: string(inputs.patch), PatchSHA256: sha256Hex(inputs.patch),
		DesignDocuments: inputs.documents, Instructions: string(inputs.instructions), ResultPath: artifactDisplayPath(defaultResultFilename), ProofDirectory: artifactDisplayPath(proofDirectory),
	}, nil
}

func readCanonicalInstructions(repo repository.Repository) ([]byte, error) {
	path := filepath.Join(repo.PolicyRoot, "templates", "behavior-review.md")
	return readExternalRegularUTF8(path, maximumTemplateBytes, true, "behavior review instructions")
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
	defer state.root.Close()
	if err := writeFinalizedReview(state); err != nil {
		return FinalizeResult{}, err
	}
	if err := writeGateReceipt(state); err != nil {
		return FinalizeResult{}, err
	}
	return FinalizeResult{
		ReviewID: state.review.ReviewID, Base: state.base, Candidate: state.candidate, SelectionSHA256: state.packet.SelectionSHA256,
		DecisionSHA256: state.packet.DecisionSHA256, Selection: cloneReviewSelection(state.packet.Selection), ReceiptPath: artifactDisplayPath(receiptFilename),
	}, nil
}

func loadFinalization(ctx context.Context, repo repository.Repository, options FinalizeOptions) (finalizationState, error) {
	root, base, candidate, err := finalizationIdentity(ctx, repo, options.Base)
	if err != nil {
		return finalizationState{}, err
	}
	success := false
	defer func() {
		if !success {
			_ = root.Close()
		}
	}()
	prepared, err := preparedPacketFor(repo, root, base, candidate)
	if err != nil {
		return finalizationState{}, err
	}
	review, reviewData, err := finalizedReviewInput(root, prepared.packet, candidate)
	if err != nil {
		return finalizationState{}, err
	}
	proofs, err := proofReferences(repo, root, review, candidate, prepared.packet)
	if err != nil {
		return finalizationState{}, err
	}
	success = true
	return finalizationState{root: root, base: base, candidate: candidate, packet: prepared.packet, packetData: prepared.packetData, markerData: prepared.markerData, review: review, reviewData: reviewData, proofs: proofs}, nil
}

func finalizationIdentity(ctx context.Context, repo repository.Repository, reference string) (*artifactHandle, string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", "", operational("finalize behavior review", err)
	}
	base, candidate, err := cleanCandidateAtMergeBase(repo, reference)
	if err != nil {
		return nil, "", "", err
	}
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		return nil, "", "", err
	}
	return root, base, candidate, nil
}

func preparedPacketFor(repo repository.Repository, root *artifactHandle, base, candidate string) (preparedPacketState, error) {
	packet, data, err := readPacket(root)
	if err != nil {
		return preparedPacketState{}, err
	}
	if packet.Base != base || packet.Candidate != candidate {
		return preparedPacketState{}, fmt.Errorf("%w: prepared packet does not match the current base and candidate", ErrStaleReview)
	}
	markerData, err := validatePrepareMarker(root, packet, data)
	if err != nil {
		return preparedPacketState{}, err
	}
	if err := validatePacketMaterial(repo, root, packet); err != nil {
		return preparedPacketState{}, err
	}
	return preparedPacketState{packet: packet, packetData: data, markerData: markerData}, nil
}

func validatePrepareMarker(root *artifactHandle, packet reviewPacket, packetData []byte) ([]byte, error) {
	data, err := root.readArtifact(prepareMarkerFilename, maximumArtifactReadByte)
	if err != nil {
		return nil, fmt.Errorf("%w: prepare marker is unavailable", ErrStaleReview)
	}
	var marker prepareMarker
	if err := decodeStrict(data, &marker); err != nil {
		return nil, fmt.Errorf("%w: prepare marker is invalid", ErrStaleReview)
	}
	if !markerMatchesPacket(marker, packet, packetData) {
		return nil, fmt.Errorf("%w: prepared packet differs from its prepare marker", ErrStaleReview)
	}
	return data, nil
}

func markerMatchesPacket(marker prepareMarker, packet reviewPacket, packetData []byte) bool {
	return allValid(
		marker.Version == artifactVersion,
		validIdentifier(marker.ReviewID),
		validRevision(marker.Base),
		validRevision(marker.Candidate),
		validSHA256(marker.SelectionSHA256),
		validSHA256(marker.RequirementSHA256),
		validSHA256(marker.DecisionSHA256),
		validSHA256(marker.PacketSHA256),
		marker.ReviewID == packet.ReviewID,
		marker.Base == packet.Base,
		marker.Candidate == packet.Candidate,
		marker.SelectionSHA256 == packet.SelectionSHA256,
		marker.RequirementSHA256 == packet.RequirementSHA256,
		marker.DecisionSHA256 == packet.DecisionSHA256,
		marker.PacketSHA256 == sha256Hex(packetData),
	)
}

func validatePacketMaterial(repo repository.Repository, root *artifactHandle, packet reviewPacket) error {
	patch, documents, err := prepareCandidateMaterial(repo, packet.Base, packet.Candidate)
	if err != nil {
		return err
	}
	instructions, err := readCanonicalInstructions(repo)
	if err != nil {
		return err
	}
	journal, err := readIntentJournal(repo, root, false)
	if err != nil {
		return fmt.Errorf("%w: prepared intent journal is unavailable", ErrStaleReview)
	}
	intents, intentSHA256, err := selectIntentCaptures(repo, journal, packet.Base, packet.Candidate)
	if err != nil {
		return fmt.Errorf("%w: prepared intent journal differs from the bound candidate", ErrStaleReview)
	}
	requirements, err := taskRequirementsFor(repo, journal, packet.Base, packet.Candidate)
	if err != nil {
		return fmt.Errorf("%w: prepared task requirements differ from the bound candidate", ErrStaleReview)
	}
	if packet.Patch != string(patch) || packet.Instructions != string(instructions) ||
		!samePacketDocuments(packet.DesignDocuments, documents) || !sameIntentCaptures(packet.Intents, intents) || packet.IntentSHA256 != intentSHA256 ||
		!sameTaskRequirements(packet.TaskRequirements, requirements.Requirements) || packet.RequirementSHA256 != requirements.SHA256 {
		return fmt.Errorf("%w: prepared packet material differs from the bound candidate", ErrStaleReview)
	}
	if err := validateSelectionIncludesFeatures(packet.Selection, requirements.RequestedFeatures); err != nil {
		return fmt.Errorf("%w: prepared selection does not retain task requirements", ErrStaleReview)
	}
	decisionSHA256, err := DecisionBindingSHA256(packet.Selection, requirements)
	if err != nil || packet.DecisionSHA256 != decisionSHA256 {
		return fmt.Errorf("%w: prepared decision binding differs from the bound candidate", ErrStaleReview)
	}
	return nil
}

func samePacketDocuments(left, right []packetDesignDocument) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func finalizedReviewInput(root *artifactHandle, packet reviewPacket, candidate string) (ReviewResult, []byte, error) {
	data, err := root.readRegularUTF8(defaultResultFilename, maximumResultBytes, true, "behavior review result")
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

func reviewMatchesPacket(review ReviewResult, packet reviewPacket, candidate string) bool {
	if !allValid(
		review.ReviewID == packet.ReviewID,
		review.Base == packet.Base,
		review.Candidate == candidate,
		review.IntentSHA256 == packet.IntentSHA256,
		review.SelectionSHA256 == packet.SelectionSHA256,
		review.DecisionSHA256 == packet.DecisionSHA256,
	) {
		return false
	}
	for _, behavior := range review.Behaviors {
		if err := validateBehaviorScope(behavior.Scope, packet.Selection); err != nil {
			return false
		}
	}
	return true
}

func writeFinalizedReview(state finalizationState) error {
	return state.root.writeArtifactAtomic(finalReviewFilename, state.reviewData)
}

func writeGateReceipt(state finalizationState) error {
	receipt := GateReceipt{
		Version: artifactVersion, ReviewID: state.review.ReviewID, Base: state.base, Candidate: state.candidate,
		IntentSHA256: state.packet.IntentSHA256, SelectionSHA256: state.packet.SelectionSHA256, RequirementSHA256: state.packet.RequirementSHA256,
		DecisionSHA256: state.packet.DecisionSHA256, Selection: cloneReviewSelection(state.packet.Selection), PacketSHA256: sha256Hex(state.packetData), PrepareSHA256: sha256Hex(state.markerData),
		ReviewSHA256: sha256Hex(state.reviewData), Proofs: state.proofs,
	}
	data, err := marshalArtifact(receipt)
	if err != nil {
		return err
	}
	return state.root.writeArtifactAtomic(receiptFilename, data)
}

func validateGateReceipt(ctx context.Context, repo repository.Repository, options ValidateGateReceiptOptions) (GateReceipt, error) {
	state, err := currentReceiptState(reviewContext(ctx), repo, options)
	if err != nil {
		return GateReceipt{}, err
	}
	defer state.root.Close()
	packet, err := validateReceiptPacket(repo, state)
	if err != nil {
		return GateReceipt{}, err
	}
	review, err := validateReceiptReview(state, packet)
	if err != nil {
		return GateReceipt{}, err
	}
	if err := validateReceiptProofs(repo, state, packet, review); err != nil {
		return GateReceipt{}, err
	}
	return state.receipt, nil
}

func currentReceiptState(ctx context.Context, repo repository.Repository, options ValidateGateReceiptOptions) (receiptState, error) {
	if err := ctx.Err(); err != nil {
		return receiptState{}, operational("validate behavior review receipt", err)
	}
	if strings.TrimSpace(options.Base) == "" {
		return receiptState{}, fmt.Errorf("%w: behavior review receipt base is required", ErrInvalidInput)
	}
	selection, err := NormalizeReviewSelection(options.Selection)
	if err != nil {
		return receiptState{}, err
	}
	base, candidate, err := cleanCandidateAtMergeBase(repo, options.Base)
	if err != nil {
		return receiptState{}, err
	}
	root, err := existingBehaviorReviewRoot(repo)
	if err != nil {
		return receiptState{}, err
	}
	success := false
	defer func() {
		if !success {
			_ = root.Close()
		}
	}()
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
	success = true
	return receiptState{root: root, base: base, candidate: candidate, selection: selection, receipt: receipt}, nil
}

func validateReceiptPacket(repo repository.Repository, state receiptState) (reviewPacket, error) {
	prepared, err := preparedPacketFor(repo, state.root, state.base, state.candidate)
	if err != nil {
		return reviewPacket{}, staleReceipt("prepared packet is unavailable", err)
	}
	if !packetMatchesReceipt(prepared.packet, prepared.packetData, prepared.markerData, state) || !sameReviewSelection(prepared.packet.Selection, state.selection) {
		return reviewPacket{}, staleReceipt("prepared packet does not match the receipt", nil)
	}
	return prepared.packet, nil
}

func packetMatchesReceipt(packet reviewPacket, data, markerData []byte, state receiptState) bool {
	return allValid(
		sha256Hex(data) == state.receipt.PacketSHA256,
		sha256Hex(markerData) == state.receipt.PrepareSHA256,
		packet.ReviewID == state.receipt.ReviewID,
		packet.Base == state.base,
		packet.Candidate == state.candidate,
		packet.IntentSHA256 == state.receipt.IntentSHA256,
		packet.SelectionSHA256 == state.receipt.SelectionSHA256,
		packet.RequirementSHA256 == state.receipt.RequirementSHA256,
		packet.DecisionSHA256 == state.receipt.DecisionSHA256,
		sameReviewSelection(packet.Selection, state.receipt.Selection),
	)
}

func validateReceiptReview(state receiptState, packet reviewPacket) (ReviewResult, error) {
	data, err := state.root.readArtifact(finalReviewFilename, maximumResultBytes)
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

func validateReceiptProofs(repo repository.Repository, state receiptState, packet reviewPacket, review ReviewResult) error {
	proofs, err := proofReferences(repo, state.root, review, state.candidate, packet)
	if err != nil {
		return staleReceipt("proofs do not validate", err)
	}
	if !slices.Equal(proofs, state.receipt.Proofs) {
		return staleReceipt("proof references do not match the receipt", nil)
	}
	return nil
}

func readPacket(root *artifactHandle) (reviewPacket, []byte, error) {
	data, err := root.readArtifact(packetFilename, maximumArtifactReadByte)
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

func readReceipt(root *artifactHandle) (GateReceipt, error) {
	data, err := root.readArtifact(receiptFilename, maximumArtifactReadByte)
	if err != nil {
		return GateReceipt{}, fmt.Errorf("%w: behavior review receipt is unavailable", ErrMissingReceipt)
	}
	var receipt GateReceipt
	if err := decodeStrict(data, &receipt); err != nil {
		return GateReceipt{}, staleReceipt("receipt JSON is invalid", err)
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
	if err := validatePacketIntents(packet); err != nil {
		return err
	}
	if err := validatePacketRequirements(packet); err != nil {
		return err
	}
	if err := validatePacketSelection(packet); err != nil {
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

func validatePacketRequirements(packet reviewPacket) error {
	if packet.TaskRequirements == nil || !validSHA256(packet.RequirementSHA256) {
		return fmt.Errorf("%w: packet task requirements are invalid", ErrInvalidReview)
	}
	digest, err := taskRequirementsDigest(packet.TaskRequirements)
	if err != nil || digest != packet.RequirementSHA256 {
		return fmt.Errorf("%w: packet task requirement digest is invalid", ErrInvalidReview)
	}
	for _, requirement := range packet.TaskRequirements {
		if err := validateTaskRequirementFeatures(requirement.Features); err != nil {
			return fmt.Errorf("%w: packet task requirements are invalid", ErrInvalidReview)
		}
	}
	requested := requestedRequirementFeatures(packet.TaskRequirements)
	if err := validateSelectionIncludesFeatures(packet.Selection, requested); err != nil {
		return fmt.Errorf("%w: packet selection omits task requirements", ErrInvalidReview)
	}
	decisionSHA256, err := DecisionBindingSHA256(packet.Selection, TaskRequirementsResult{Requirements: packet.TaskRequirements, SHA256: packet.RequirementSHA256})
	if err != nil || !validSHA256(packet.DecisionSHA256) || decisionSHA256 != packet.DecisionSHA256 {
		return fmt.Errorf("%w: packet decision binding is invalid", ErrInvalidReview)
	}
	return nil
}

func validatePacketSelection(packet reviewPacket) error {
	if err := validateReviewSelection(packet.Selection); err != nil {
		return err
	}
	digest, err := SelectionSHA256(packet.Selection)
	if err != nil || !validSHA256(packet.SelectionSHA256) || digest != packet.SelectionSHA256 {
		return fmt.Errorf("%w: packet selection digest is invalid", ErrInvalidReview)
	}
	return nil
}

func validPacketIdentifiers(packet reviewPacket) bool {
	return allValid(validIdentifier(packet.ReviewID), validRevision(packet.Base), validRevision(packet.Candidate))
}

func validatePacketIntents(packet reviewPacket) error {
	if !validPacketIntentBoundary(packet) {
		return fmt.Errorf("%w: packet intents are invalid", ErrInvalidReview)
	}
	seen := map[string]bool{}
	expectedPrevious := packet.Intents[0].PreviousSHA256
	if expectedPrevious != "" && !validSHA256(expectedPrevious) {
		return fmt.Errorf("%w: packet intent chain is invalid", ErrInvalidReview)
	}
	for index, entry := range packet.Intents {
		if err := validateIntentCapture(entry, expectedPrevious, seen); err != nil {
			return fmt.Errorf("%w: packet intent %d is invalid", ErrInvalidReview, index+1)
		}
		seen[entry.ID] = true
		expectedPrevious = entry.EntrySHA256
	}
	digest, err := intentSelectionDigest(packet.Intents)
	if err != nil || !validSHA256(packet.IntentSHA256) || digest != packet.IntentSHA256 {
		return fmt.Errorf("%w: packet intent digest is invalid", ErrInvalidReview)
	}
	return nil
}

func validPacketIntentBoundary(packet reviewPacket) bool {
	if len(packet.Intents) == 0 {
		return false
	}
	return allValid(
		packet.Intents[0].CapturedAtCommit == packet.Base,
		packet.Intents[len(packet.Intents)-1].CapturedAtCommit != packet.Candidate,
	)
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
	if !allValid(validIdentifier(review.ReviewID), validRevision(review.Base), validRevision(review.Candidate), validSHA256(review.IntentSHA256), validSHA256(review.SelectionSHA256), validSHA256(review.DecisionSHA256)) {
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
	if err := validateBehaviorScopeShape(behavior.Scope); err != nil {
		return fmt.Errorf("%w: behavior %d scope is invalid", ErrInvalidReview, index)
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

func validateReceipt(receipt GateReceipt) error {
	if !validReceiptHeader(receipt) {
		return errors.New("receipt fields are malformed")
	}
	if receipt.Proofs == nil {
		return errors.New("receipt proof references are missing")
	}
	if err := validateReviewSelection(receipt.Selection); err != nil {
		return errors.New("receipt selection is invalid")
	}
	selectionSHA256, err := SelectionSHA256(receipt.Selection)
	if err != nil || selectionSHA256 != receipt.SelectionSHA256 || !validSHA256(receipt.DecisionSHA256) {
		return errors.New("receipt selection digest is invalid")
	}
	return validateReceiptProofReferences(receipt.Proofs)
}

func validReceiptHeader(receipt GateReceipt) bool {
	return allValid(
		receipt.Version == artifactVersion,
		validIdentifier(receipt.ReviewID),
		validRevision(receipt.Base),
		validRevision(receipt.Candidate),
		validSHA256(receipt.IntentSHA256),
		validSHA256(receipt.SelectionSHA256),
		validSHA256(receipt.RequirementSHA256),
		validSHA256(receipt.PacketSHA256),
		validSHA256(receipt.PrepareSHA256),
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
