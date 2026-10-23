package javascript

import (
	"context"
	"fmt"
	"slices"
)

type PeerFinding struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Message string `json:"message"`
}

type PeerResult struct {
	Covered     []string      `json:"covered"`
	Findings    []PeerFinding `json:"findings"`
	Unsupported []Unsupported `json:"unsupported"`
}

func (bundle Bundle) Peers(ctx context.Context, root string, paths []string) (PeerResult, error) {
	payload, err := fileRequest(OperationPeers, root, paths)
	if err != nil {
		return PeerResult{}, err
	}
	data, err := bundle.exchange(ctx, payload, packagesTimeout)
	if err != nil {
		return PeerResult{}, err
	}
	return decodePeerResult(data, paths)
}

func decodePeerResult(data []byte, paths []string) (PeerResult, error) {
	var wire struct {
		Covered     *[]string      `json:"covered"`
		Findings    *[]PeerFinding `json:"findings"`
		Unsupported *[]Unsupported `json:"unsupported"`
	}
	if err := decodeExactly(data, &wire); err != nil {
		return PeerResult{}, err
	}
	if wire.Covered == nil || wire.Findings == nil || wire.Unsupported == nil {
		return PeerResult{}, fmt.Errorf("the peers result is missing required fields")
	}
	if err := validateParseCoverage(*wire.Covered, *wire.Unsupported, paths); err != nil {
		return PeerResult{}, fmt.Errorf("the peers result has invalid coverage: %w", err)
	}
	for _, finding := range *wire.Findings {
		if !slices.Contains(*wire.Covered, finding.Path) || finding.Name == "" || !validParseReason(finding.Message) {
			return PeerResult{}, fmt.Errorf("the peers result has an invalid finding")
		}
	}
	return PeerResult{Covered: *wire.Covered, Findings: *wire.Findings, Unsupported: *wire.Unsupported}, nil
}
