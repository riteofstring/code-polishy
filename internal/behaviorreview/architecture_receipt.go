package behaviorreview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/riteofstring/code-polishy/internal/architecture"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func FinalizeArchitectureReview(ctx context.Context, repo repository.Repository, base string, input ArchitectureReviewInput) (ArchitectureReviewStatus, error) {
	current, err := currentArchitecturePacket(ctx, repo, base, input)
	if err != nil {
		return ArchitectureReviewStatus{}, err
	}
	root, err := existingArchitectureRoot(repo)
	if err != nil {
		return ArchitectureReviewStatus{}, err
	}
	defer root.Close()
	marker, packet, err := readPreparedArchitecture(root)
	if err != nil {
		return ArchitectureReviewStatus{}, err
	}
	if !sameArchitectureMaterial(packet, current) {
		return ArchitectureReviewStatus{}, fmt.Errorf("%w: prepared inputs no longer match the clean candidate", ErrArchitectureReview)
	}
	if err := validateArchitectureBaseline(root, packet); err != nil {
		return ArchitectureReviewStatus{}, err
	}
	digest, err := publishArchitectureAcceptance(repo, root, marker, packet)
	if err != nil {
		return ArchitectureReviewStatus{}, err
	}
	return acceptedArchitectureStatus(current, packet.ReviewID, digest), nil
}

func publishArchitectureAcceptance(repo repository.Repository, root *artifactHandle, marker architectureBinding, packet architecturePacket) (string, error) {
	data, err := root.readRegularUTF8(architectureReviewPath(packet.ReviewID, defaultResultFilename), maximumResultBytes, true, "architecture review result")
	if err != nil {
		return "", err
	}
	result, err := validateArchitectureResult(packet, data)
	if err != nil {
		return "", err
	}
	if result.Decision != "accept" {
		return "", fmt.Errorf("%w: resolve the %d architecture findings before finalization", ErrArchitectureRequired, len(result.Findings))
	}
	if err := ensureCandidateUnchanged(repo, packet.Candidate); err != nil {
		return "", err
	}
	receiptData, err := architectureArtifactData(architectureReceipt{architectureBinding: marker, ResultSHA256: sha256Hex(data)}, maximumResultBytes)
	if err != nil {
		return "", err
	}
	if err := root.writeArtifactAtomic(receiptFilename, receiptData); err != nil {
		return "", err
	}
	return sha256Hex(receiptData), nil
}

func ArchitectureReviewStatusFor(ctx context.Context, repo repository.Repository, base string, input ArchitectureReviewInput) (ArchitectureReviewStatus, error) {
	current, err := currentArchitecturePacket(ctx, repo, base, input)
	if err != nil {
		return ArchitectureReviewStatus{}, err
	}
	status := ArchitectureReviewStatus{State: "required", Base: current.Base, Candidate: current.Candidate, Topology: current.Topology.Identity, Signals: current.Signals}
	root, err := existingArchitectureRoot(repo)
	if errors.Is(err, ErrArchitectureRequired) {
		return unreviewedArchitectureStatus(status, current), nil
	}
	if err != nil {
		return status, err
	}
	defer root.Close()
	accepted, err := readAcceptedArchitecture(root)
	if err != nil {
		status.Reason = err.Error()
		return status, nil
	}
	if accepted == nil {
		return unreviewedArchitectureStatus(status, current), nil
	}
	if err := validateReusableArchitecture(repo, root, *accepted, current); err != nil {
		status.Reason = err.Error()
		if accepted.packet.Topology.Identity != current.Topology.Identity {
			status.Signals = append(status.Signals, architecture.ReviewSignal{ID: "reviewed-topology-changed", Summary: "the module contract, ownership, roots, or source topology differs from the accepted review", Paths: []string{}, Units: []string{}})
		}
		return status, nil
	}
	return acceptedArchitectureStatus(current, accepted.receipt.ReviewID, accepted.digest), nil
}

func unreviewedArchitectureStatus(status ArchitectureReviewStatus, packet architecturePacket) ArchitectureReviewStatus {
	if len(packet.Graph.Nodes) == 0 && len(status.Signals) == 0 {
		status.State = "not-required"
		return status
	}
	status.Reason = "the current source architecture has no accepted review"
	status.Signals = append(status.Signals, architecture.ReviewSignal{ID: "unreviewed-architecture", Summary: status.Reason, Paths: []string{}, Units: []string{}})
	return status
}

func acceptedArchitectureStatus(current architecturePacket, id, digest string) ArchitectureReviewStatus {
	return ArchitectureReviewStatus{State: "passed", Base: current.Base, Candidate: current.Candidate, Topology: current.Topology.Identity, Signals: current.Signals, ReviewID: id, ReceiptSHA256: digest}
}

func validateReusableArchitecture(repo repository.Repository, root *artifactHandle, accepted architectureAccepted, current architecturePacket) error {
	marker, _, err := readPreparedArchitecture(root)
	if err != nil || marker != accepted.receipt.architectureBinding {
		return fmt.Errorf("%w: the latest prepared review has no matching acceptance", ErrArchitectureRequired)
	}
	if accepted.packet.Base != current.Base || accepted.packet.Topology.Identity != current.Topology.Identity || !reflect.DeepEqual(accepted.packet.Signals, current.Signals) {
		return fmt.Errorf("%w: review base or architecture identity changed", ErrArchitectureRequired)
	}
	if accepted.packet.Instructions != current.Instructions || !samePacketDocuments(accepted.packet.DesignDocuments, current.DesignDocuments) {
		return fmt.Errorf("%w: reviewer instructions or mapped design documents changed", ErrArchitectureRequired)
	}
	ancestor, err := repo.IsAncestor(accepted.packet.Candidate, current.Candidate)
	if err != nil {
		return err
	}
	if !ancestor {
		return fmt.Errorf("%w: reviewed candidate is not an ancestor of the current candidate", ErrArchitectureRequired)
	}
	return nil
}

func validateArchitectureBaseline(root *artifactHandle, packet architecturePacket) error {
	previous, err := architectureBaseline(root)
	if err != nil {
		return err
	}
	baseline := architecture.ReviewTopology{}
	candidate := ""
	if previous != nil {
		if previous.receipt.ReviewID == packet.ReviewID {
			return nil
		}
		baseline, candidate = previous.packet.Topology, previous.packet.Candidate
	}
	if candidate != packet.PreviousCandidate || !reflect.DeepEqual(architecture.CompareReviewTopology(baseline, packet.Topology), packet.TopologyDiff) {
		return fmt.Errorf("%w: topology diff does not match the last accepted review", ErrArchitectureReview)
	}
	return nil
}

func architectureBaseline(root *artifactHandle) (*architectureAccepted, error) {
	accepted, err := readAcceptedArchitecture(root)
	if errors.Is(err, ErrArchitectureReview) || errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return accepted, err
}

func existingArchitectureRoot(repo repository.Repository) (*artifactHandle, error) {
	root, err := openArtifactRoot(repo.Root, artifactComponents(architectureDirectory), false)
	if err != nil {
		return nil, artifactRootError("architecture review", err, ErrArchitectureRequired)
	}
	return root, nil
}

func readPreparedArchitecture(root *artifactHandle) (architectureBinding, architecturePacket, error) {
	var marker architectureBinding
	data, err := root.readArtifact(prepareMarkerFilename, maximumResultBytes)
	if err != nil {
		return marker, architecturePacket{}, err
	}
	if err := decodeArchitectureArtifact(data, &marker); err != nil {
		return marker, architecturePacket{}, err
	}
	packet, err := readBoundArchitecturePacket(root, marker)
	return marker, packet, err
}

func readBoundArchitecturePacket(root *artifactHandle, binding architectureBinding) (architecturePacket, error) {
	var packet architecturePacket
	if binding.Protocol != architectureReviewProtocol || !validIdentifier(binding.ReviewID) || !validRevision(binding.Base) || !validRevision(binding.Candidate) || !validSHA256(binding.PacketSHA256) {
		return packet, fmt.Errorf("%w: malformed packet binding", ErrArchitectureReview)
	}
	data, err := root.readRegularUTF8(architectureReviewPath(binding.ReviewID, packetFilename), maximumArchitecturePacket, true, "architecture review packet")
	if err != nil {
		return packet, err
	}
	if err := decodeArchitectureArtifact(data, &packet); err != nil {
		return packet, err
	}
	if binding != bindArchitecturePacket(packet, data) || packet.ResultPath != architectureDisplayPath(binding.ReviewID, defaultResultFilename) {
		return packet, fmt.Errorf("%w: packet binding mismatch", ErrArchitectureReview)
	}
	return packet, nil
}

func readAcceptedArchitecture(root *artifactHandle) (*architectureAccepted, error) {
	data, err := root.readArtifact(receiptFilename, maximumResultBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt architectureReceipt
	if err := decodeArchitectureArtifact(data, &receipt); err != nil {
		return nil, err
	}
	packet, err := readBoundArchitecturePacket(root, receipt.architectureBinding)
	if err != nil {
		return nil, err
	}
	resultData, err := root.readRegularUTF8(architectureReviewPath(receipt.ReviewID, defaultResultFilename), maximumResultBytes, true, "accepted architecture result")
	if err != nil {
		return nil, err
	}
	result, err := validateArchitectureResult(packet, resultData)
	if err != nil || result.Decision != "accept" || receipt.ResultSHA256 != sha256Hex(resultData) {
		return nil, fmt.Errorf("%w: accepted result is missing, changed, or invalid", ErrArchitectureReview)
	}
	return &architectureAccepted{packet: packet, receipt: receipt, digest: sha256Hex(data)}, nil
}
