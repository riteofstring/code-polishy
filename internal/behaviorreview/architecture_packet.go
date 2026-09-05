package behaviorreview

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture"
	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func PrepareArchitectureReview(ctx context.Context, repo repository.Repository, base string, input ArchitectureReviewInput) (ArchitecturePrepareResult, error) {
	packet, err := currentArchitecturePacket(ctx, repo, base, input)
	if err != nil {
		return ArchitecturePrepareResult{}, err
	}
	root, err := managedArtifactRoot(repo, architectureDirectory, "architecture review")
	if err != nil {
		return ArchitecturePrepareResult{}, err
	}
	defer root.Close()
	previous, err := architectureBaseline(root)
	if err != nil {
		return ArchitecturePrepareResult{}, err
	}
	if previous != nil {
		packet.PreviousCandidate = previous.packet.Candidate
		packet.TopologyDiff = architecture.CompareReviewTopology(previous.packet.Topology, packet.Topology)
	}
	packet.ReviewID, err = newReviewID()
	if err != nil {
		return ArchitecturePrepareResult{}, err
	}
	packet.ResultPath = architectureDisplayPath(packet.ReviewID, defaultResultFilename)
	if err := publishArchitecturePacket(repo, root, packet); err != nil {
		return ArchitecturePrepareResult{}, err
	}
	return ArchitecturePrepareResult{ReviewID: packet.ReviewID, Base: packet.Base, Candidate: packet.Candidate, Topology: packet.Topology.Identity, PacketPath: architectureDisplayPath(packet.ReviewID, packetFilename), ResultPath: packet.ResultPath}, nil
}

func publishArchitecturePacket(repo repository.Repository, root *artifactHandle, packet architecturePacket) error {
	data, err := architectureArtifactData(packet, maximumArchitecturePacket)
	if err != nil {
		return err
	}
	for _, directory := range []string{"reviews", "reviews/" + packet.ReviewID} {
		if err := root.ensureDirectory(directory, "architecture review"); err != nil {
			return err
		}
	}
	if err := root.writeArtifactAtomic(architectureReviewPath(packet.ReviewID, packetFilename), data); err != nil {
		return err
	}
	if err := ensureCandidateUnchanged(repo, packet.Candidate); err != nil {
		return err
	}
	marker, err := architectureArtifactData(bindArchitecturePacket(packet, data), maximumResultBytes)
	if err != nil {
		return err
	}
	return root.writeArtifactAtomic(prepareMarkerFilename, marker)
}

func currentArchitecturePacket(ctx context.Context, repo repository.Repository, reference string, input ArchitectureReviewInput) (architecturePacket, error) {
	if err := reviewContext(ctx).Err(); err != nil {
		return architecturePacket{}, err
	}
	if strings.TrimSpace(reference) == "" {
		return architecturePacket{}, fmt.Errorf("%w: base is required", ErrArchitectureReview)
	}
	base, candidate, err := cleanCandidateAtMergeBase(repo, reference)
	if err != nil {
		return architecturePacket{}, err
	}
	topology, err := architecture.BuildReviewTopology(repo, input.Graph)
	if err != nil {
		return architecturePacket{}, err
	}
	if err := validateArchitectureInput(input); err != nil {
		return architecturePacket{}, err
	}
	patch, err := readArchitecturePatch(repo, base, candidate)
	if err != nil {
		return architecturePacket{}, err
	}
	paths := make([]string, 0, len(input.Graph.Nodes))
	for _, node := range input.Graph.Nodes {
		paths = append(paths, node.Path)
	}
	sources, err := readArchitectureSources(repo, candidate, paths)
	if err != nil {
		return architecturePacket{}, err
	}
	documents, err := readCurrentDesignDocuments(repo, paths)
	if err != nil {
		return architecturePacket{}, err
	}
	instructions, err := readExternalRegularUTF8(filepath.Join(repo.PolicyRoot, "templates", "architecture-review.md"), maximumTemplateBytes, true, "architecture reviewer instructions")
	if err != nil {
		return architecturePacket{}, err
	}
	return architecturePacket{
		Protocol: architectureReviewProtocol, Base: base, Candidate: candidate, Graph: input.Graph, Topology: topology,
		Signals: architecture.ReviewSignals(topology), Cycles: []sourcegraph.Component{}, Summary: architecture.Summary(repo, paths, &input.Graph),
		TopologyDiff: architecture.CompareReviewTopology(architecture.ReviewTopology{}, topology),
		Patch:        patch, Sources: sources, DesignDocuments: documents, Instructions: string(instructions),
	}, nil
}

func readArchitecturePatch(repo repository.Repository, base, candidate string) (string, error) {
	patch, err := repo.Patch(base, candidate, nil)
	if err != nil {
		return "", err
	}
	if len(patch) > maximumArchitecturePatch {
		return "", fmt.Errorf("%w: candidate patch exceeds %d bytes", ErrArchitectureReview, maximumArchitecturePatch)
	}
	return string(patch), nil
}

func validateArchitectureInput(input ArchitectureReviewInput) error {
	for _, finding := range input.Findings {
		if finding.Severity == "" || finding.Severity == policy.FindingError {
			return fmt.Errorf("%w: resolve %s at %s before review", ErrArchitectureReview, finding.Check, finding.Path)
		}
	}
	cycles, err := sourcegraph.CyclicComponents(input.Graph)
	if err != nil {
		return err
	}
	if len(cycles) > 0 {
		return fmt.Errorf("%w: resolve source cycles before review", ErrArchitectureReview)
	}
	return nil
}

func bindArchitecturePacket(packet architecturePacket, data []byte) architectureBinding {
	signals, _ := architectureArtifactData(packet.Signals, maximumArchitecturePacket)
	return architectureBinding{
		Protocol: architectureReviewProtocol, ReviewID: packet.ReviewID, Base: packet.Base, Candidate: packet.Candidate,
		Graph: packet.Graph.Identity, Topology: packet.Topology.Identity, SignalsSHA256: sha256Hex(signals), PacketSHA256: sha256Hex(data),
	}
}

func sameArchitectureMaterial(prepared, current architecturePacket) bool {
	prepared.ReviewID, prepared.ResultPath, prepared.PreviousCandidate = "", "", ""
	prepared.TopologyDiff = current.TopologyDiff
	before, beforeErr := architectureArtifactData(prepared, maximumArchitecturePacket)
	after, afterErr := architectureArtifactData(current, maximumArchitecturePacket)
	return beforeErr == nil && afterErr == nil && bytes.Equal(before, after)
}

func architectureReviewPath(id, name string) string {
	return "reviews/" + id + "/" + name
}

func architectureDisplayPath(id, name string) string {
	return architectureDirectory + "/" + architectureReviewPath(id, name)
}
