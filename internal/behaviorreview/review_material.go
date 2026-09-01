package behaviorreview

import (
	"fmt"
	"reflect"

	"github.com/riteofstring/code-polishy/internal/finalstate"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type currentPacketMaterial struct {
	patch        []byte
	documents    []packetDesignDocument
	instructions []byte
	intents      []intentCapture
	intentSHA256 string
	requirements TaskRequirementsResult
	finalState   finalstate.Evidence
}

func validatePacketMaterial(repo repository.Repository, root *artifactHandle, packet reviewPacket) error {
	material, err := collectCurrentPacketMaterial(repo, root, packet)
	if err != nil {
		return err
	}
	if !packetMatchesCurrentMaterial(packet, material) {
		return fmt.Errorf("%w: prepared packet material differs from the bound candidate", ErrStaleReview)
	}
	if err := validateSelectionIncludesFeatures(packet.Selection, material.requirements.RequestedFeatures); err != nil {
		return fmt.Errorf("%w: prepared selection does not retain task requirements", ErrStaleReview)
	}
	decisionSHA256, err := DecisionBindingSHA256(packet.Selection, material.requirements)
	if err != nil || packet.DecisionSHA256 != decisionSHA256 {
		return fmt.Errorf("%w: prepared decision binding differs from the bound candidate", ErrStaleReview)
	}
	return nil
}

func collectCurrentPacketMaterial(repo repository.Repository, root *artifactHandle, packet reviewPacket) (currentPacketMaterial, error) {
	patch, documents, finalState, err := prepareCandidateMaterial(repo, packet.Base, packet.Candidate)
	if err != nil {
		return currentPacketMaterial{}, err
	}
	instructions, err := readCanonicalInstructions(repo)
	if err != nil {
		return currentPacketMaterial{}, err
	}
	journal, err := readIntentJournal(repo, root, false)
	if err != nil {
		return currentPacketMaterial{}, fmt.Errorf("%w: prepared intent journal is unavailable", ErrStaleReview)
	}
	intents, intentSHA256, err := selectIntentCaptures(repo, journal, packet.Base, packet.Candidate)
	if err != nil {
		return currentPacketMaterial{}, fmt.Errorf("%w: prepared intent journal differs from the bound candidate", ErrStaleReview)
	}
	requirements, err := taskRequirementsFor(repo, journal, packet.Base, packet.Candidate)
	if err != nil {
		return currentPacketMaterial{}, fmt.Errorf("%w: prepared task requirements differ from the bound candidate", ErrStaleReview)
	}
	return currentPacketMaterial{
		patch: patch, documents: documents, instructions: instructions, intents: intents, intentSHA256: intentSHA256, requirements: requirements, finalState: finalState,
	}, nil
}

func packetMatchesCurrentMaterial(packet reviewPacket, material currentPacketMaterial) bool {
	return packet.Patch == string(material.patch) && packet.Instructions == string(material.instructions) &&
		samePacketDocuments(packet.DesignDocuments, material.documents) && sameIntentCaptures(packet.Intents, material.intents) &&
		packet.IntentSHA256 == material.intentSHA256 && sameTaskRequirements(packet.TaskRequirements, material.requirements.Requirements) &&
		packet.RequirementSHA256 == material.requirements.SHA256 && reflect.DeepEqual(packet.FinalState, material.finalState)
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
