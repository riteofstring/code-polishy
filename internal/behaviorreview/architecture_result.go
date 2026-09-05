package behaviorreview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func validateArchitectureResult(packet architecturePacket, data []byte) (ArchitectureReviewResult, error) {
	var result ArchitectureReviewResult
	if err := decodeArchitectureArtifact(data, &result); err != nil {
		return result, err
	}
	if err := validateArchitectureDecision(packet, result); err != nil {
		return result, err
	}
	packetJSON, err := architectureCitationDocument(packet)
	if err != nil {
		return result, err
	}
	if err := validateArchitectureCitations(packetJSON, result.Evidence); err != nil {
		return result, err
	}
	for _, finding := range result.Findings {
		if strings.TrimSpace(finding.Summary) == "" {
			return result, fmt.Errorf("%w: every finding requires a summary", ErrArchitectureReview)
		}
		if err := validateArchitectureCitations(packetJSON, finding.Evidence); err != nil {
			return result, err
		}
		if err := validateArchitectureCorrection(finding.Correction); err != nil {
			return result, err
		}
	}
	return result, nil
}

func validateArchitectureDecision(packet architecturePacket, result ArchitectureReviewResult) error {
	if result.Protocol != architectureReviewProtocol || result.ReviewID != packet.ReviewID || result.Base != packet.Base || result.Candidate != packet.Candidate || result.Topology != packet.Topology.Identity {
		return fmt.Errorf("%w: result does not identify the prepared candidate and topology", ErrArchitectureReview)
	}
	if strings.TrimSpace(result.Rationale) == "" || result.Findings == nil {
		return fmt.Errorf("%w: rationale and an explicit findings array are required", ErrArchitectureReview)
	}
	return validateArchitectureDecisionFindings(result)
}

func validateArchitectureDecisionFindings(result ArchitectureReviewResult) error {
	if result.Decision != "accept" && result.Decision != "findings" || (result.Decision == "accept") != (len(result.Findings) == 0) {
		return fmt.Errorf("%w: decision must agree with the findings", ErrArchitectureReview)
	}
	return nil
}

func architectureCitationDocument(packet architecturePacket) (map[string]any, error) {
	data, err := architectureArtifactData(packet, maximumArchitecturePacket)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return document, nil
}

func validateArchitectureCitations(packet map[string]any, citations []ArchitectureCitation) error {
	if len(citations) == 0 || len(citations) > 256 {
		return fmt.Errorf("%w: between one and 256 concrete evidence citations are required", ErrArchitectureReview)
	}
	seen := map[string]bool{}
	for _, citation := range citations {
		if err := validateArchitectureCitation(packet, citation); err != nil {
			return err
		}
		key := citation.Pointer + "\x00" + citation.Quote
		if seen[key] {
			return fmt.Errorf("%w: evidence citations must be distinct", ErrArchitectureReview)
		}
		seen[key] = true
	}
	return nil
}

func validateArchitectureCitation(packet map[string]any, citation ArchitectureCitation) error {
	if !validArchitectureCitationFields(citation) {
		return fmt.Errorf("%w: evidence requires an exact packet pointer, bounded quote, and rationale", ErrArchitectureReview)
	}
	value, err := architecturePointerValue(packet, citation.Pointer)
	if err != nil {
		return err
	}
	if actual, ok := value.(string); ok {
		if actual == citation.Quote || architectureContentPointer(citation.Pointer) && strings.Contains(actual, citation.Quote) {
			return nil
		}
	} else {
		actual, err := json.Marshal(value)
		if err == nil && string(actual) == citation.Quote {
			return nil
		}
	}
	return fmt.Errorf("%w: evidence quote does not match %s", ErrArchitectureReview, citation.Pointer)
}

func validArchitectureCitationFields(citation ArchitectureCitation) bool {
	return architectureEvidencePointer(citation.Pointer) && strings.TrimSpace(citation.Quote) != "" && len(citation.Quote) <= 8192 && strings.TrimSpace(citation.Rationale) != ""
}

func architectureEvidencePointer(pointer string) bool {
	if pointer == "/patch" {
		return true
	}
	for _, prefix := range []string{"/graph/nodes/", "/graph/edges/", "/graph/externalCompositions/", "/topology/modules/", "/topology/ownership/", "/topology/testPaths/", "/topology/externalCompositions/", "/sources/", "/designDocuments/", "/summary/"} {
		if strings.HasPrefix(pointer, prefix) {
			index, _, _ := strings.Cut(strings.TrimPrefix(pointer, prefix), "/")
			if value, err := strconv.Atoi(index); err == nil && value >= 0 && strconv.Itoa(value) == index {
				return true
			}
		}
	}
	return false
}

func architectureContentPointer(pointer string) bool {
	return pointer == "/patch" || (strings.HasPrefix(pointer, "/designDocuments/") || strings.HasPrefix(pointer, "/sources/")) && strings.HasSuffix(pointer, "/content")
}

func architecturePointerValue(document map[string]any, pointer string) (any, error) {
	var value any = document
	for _, part := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		switch container := value.(type) {
		case map[string]any:
			var exists bool
			value, exists = container[part]
			if !exists {
				return nil, fmt.Errorf("%w: unknown evidence pointer %s", ErrArchitectureReview, pointer)
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(container) || strconv.Itoa(index) != part {
				return nil, fmt.Errorf("%w: invalid evidence index in %s", ErrArchitectureReview, pointer)
			}
			value = container[index]
		default:
			return nil, fmt.Errorf("%w: evidence pointer %s does not resolve", ErrArchitectureReview, pointer)
		}
	}
	return value, nil
}

func validateArchitectureCorrection(correction ArchitectureCorrection) error {
	if strings.TrimSpace(correction.Rationale) == "" || len(correction.Modules) == 0 || correction.Ownership == nil {
		return fmt.Errorf("%w: each finding requires a corrected module graph, ownership map, and rationale", ErrArchitectureReview)
	}
	for _, module := range correction.Modules {
		if !validIdentifier(module.Name) || len(module.Paths) == 0 {
			return fmt.Errorf("%w: proposed modules require names and paths", ErrArchitectureReview)
		}
	}
	if err := policy.ValidateModuleGraph(correction.Modules); err != nil {
		return fmt.Errorf("%w: proposed graph: %v", ErrArchitectureReview, err)
	}
	return nil
}

func architectureArtifactData(value any, limit int) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(data)+1 > limit {
		return nil, fmt.Errorf("%w: artifact exceeds %d bytes", ErrArchitectureReview, limit)
	}
	return append(data, '\n'), nil
}

func decodeArchitectureArtifact(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := architectureUniqueJSON(decoder, 0); err != nil {
		return fmt.Errorf("%w: %v", ErrArchitectureReview, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("%w: trailing JSON content", ErrArchitectureReview)
	}
	if err := validateArchitectureJSONShape(data, reflect.TypeOf(value)); err != nil {
		return fmt.Errorf("%w: %v", ErrArchitectureReview, err)
	}
	if err := decodeStrict(data, value); err != nil {
		return fmt.Errorf("%w: %v", ErrArchitectureReview, err)
	}
	return nil
}

func architectureUniqueJSON(decoder *json.Decoder, depth int) error {
	if depth > 64 {
		return fmt.Errorf("JSON nesting exceeds 64 levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate or invalid JSON object key")
			}
			seen[name] = true
		}
		if err := architectureUniqueJSON(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
