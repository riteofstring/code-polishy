package behaviorreview

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type architectureArtifactFingerprint struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Missing bool   `json:"missing"`
}

func ArchitectureReviewArtifactsSHA256(repo repository.Repository) (string, error) {
	root, err := existingArchitectureRoot(repo)
	if errors.Is(err, ErrArchitectureRequired) {
		return architectureFingerprintDigest([]architectureArtifactFingerprint{
			{Path: prepareMarkerFilename, Missing: true}, {Path: receiptFilename, Missing: true},
		})
	}
	if err != nil {
		return "", err
	}
	defer root.Close()
	fingerprints, err := architectureArtifactFingerprints(root)
	if err != nil {
		return "", err
	}
	return architectureFingerprintDigest(fingerprints)
}

func architectureFingerprintDigest(fingerprints []architectureArtifactFingerprint) (string, error) {
	data, err := architectureArtifactData(fingerprints, maximumResultBytes)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func architectureArtifactFingerprints(root *artifactHandle) ([]architectureArtifactFingerprint, error) {
	fingerprints := []architectureArtifactFingerprint{}
	ids := map[string]bool{}
	for _, name := range []string{prepareMarkerFilename, receiptFilename} {
		fingerprint, data, err := fingerprintArchitectureArtifact(root, name, maximumResultBytes)
		if err != nil {
			return nil, err
		}
		fingerprints = append(fingerprints, fingerprint)
		if fingerprint.Missing {
			continue
		}
		id, err := architectureArtifactReviewID(data)
		if err != nil {
			return nil, err
		}
		ids[id] = true
	}
	for id := range ids {
		for _, artifact := range []struct {
			name  string
			limit int
		}{{packetFilename, maximumArchitecturePacket}, {defaultResultFilename, maximumResultBytes}} {
			fingerprint, _, err := fingerprintArchitectureArtifact(root, architectureReviewPath(id, artifact.name), artifact.limit)
			if err != nil {
				return nil, err
			}
			fingerprints = append(fingerprints, fingerprint)
		}
	}
	sort.Slice(fingerprints, func(i, j int) bool { return fingerprints[i].Path < fingerprints[j].Path })
	return fingerprints, nil
}

func architectureArtifactReviewID(data []byte) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", fmt.Errorf("%w: unreadable artifact identity", ErrArchitectureReview)
	}
	var id string
	if err := json.Unmarshal(fields["reviewId"], &id); err != nil || !validIdentifier(id) {
		return "", fmt.Errorf("%w: malformed artifact identity", ErrArchitectureReview)
	}
	return id, nil
}

func fingerprintArchitectureArtifact(root *artifactHandle, name string, limit int) (architectureArtifactFingerprint, []byte, error) {
	data, err := root.readArtifact(name, limit)
	if errors.Is(err, os.ErrNotExist) {
		return architectureArtifactFingerprint{Path: name, Missing: true}, nil, nil
	}
	if err != nil {
		return architectureArtifactFingerprint{}, nil, err
	}
	return architectureArtifactFingerprint{Path: name, SHA256: sha256Hex(data)}, data, nil
}
