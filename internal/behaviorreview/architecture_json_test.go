package behaviorreview

import (
	"strings"
	"testing"

	policyschema "github.com/riteofstring/code-polishy/schema"
)

func TestArchitectureSchemaRejectsNestedMalformedEvidence(t *testing.T) {
	repo, input := newArchitectureReviewRepository(t)
	_, packet := prepareArchitectureForTest(t, repo, input)
	data := string(architectureTestJSON(t, architectureAcceptance(packet)))
	for name, change := range map[string]func(string) string{
		"missing nested field": func(value string) string {
			return strings.Replace(value, `"quote":`, `"unknown":`, 1)
		},
		"misplaced known field": func(value string) string {
			return strings.Replace(value, `"quote":`, `"decision":"accept","quote":`, 1)
		},
		"null scalar": func(value string) string {
			return strings.Replace(value, `"decision":"accept"`, `"decision":null`, 1)
		},
		"escaped duplicate": func(value string) string {
			return strings.Replace(value, `"quote":`, `"qu\u006fte":"ignored","quote":`, 1)
		},
		"trailing document": func(value string) string { return value + " {}" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := change(data)
			if changed == data {
				t.Fatal("fixture mutation did not alter the review input")
			}
			if _, err := validateArchitectureResult(packet, []byte(changed)); err == nil {
				t.Fatal("malformed evidence was accepted")
			}
		})
	}
}

func TestArchitectureSchemaRequiresCompleteReceiptAndOptionalArrayShapes(t *testing.T) {
	repo, input := newArchitectureReviewRepository(t)
	_, packet := prepareArchitectureForTest(t, repo, input)
	packetData := architectureTestJSON(t, packet)
	marker := bindArchitecturePacket(packet, packetData)
	var receipt architectureReceipt
	if err := decodeArchitectureArtifact(architectureTestJSON(t, marker), &receipt); err == nil {
		t.Fatal("a preparation marker was accepted as a completed review receipt")
	}
	data := strings.Replace(string(packetData), `"name":"app"`, `"name":"app","capabilities":null`, 1)
	if data == string(packetData) {
		t.Fatal("fixture contains no app module")
	}
	var decoded architecturePacket
	if err := decodeArchitectureArtifact([]byte(data), &decoded); err == nil {
		t.Fatal("null optional capabilities were accepted")
	}
}

func TestArchitectureJSONRetainsItsNestingLimit(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{64, 65} {
		data := []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
		if err := policyschema.ValidateUniqueJSON(data, 64); (err == nil) != (depth == 64) {
			t.Fatalf("nesting depth %d: %v", depth, err)
		}
	}
}
