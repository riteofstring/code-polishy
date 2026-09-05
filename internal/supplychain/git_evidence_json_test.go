package supplychain

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	policyschema "github.com/riteofstring/code-polishy/schema"
)

func TestGitEvidenceSchemaRejectsInvalidNestedShapes(t *testing.T) {
	t.Parallel()
	fixture := newGitEvidenceFixture(t, true)
	payload, err := json.Marshal(fixture.statement)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(string) string{
		"missing nested field": func(value string) string { return strings.Replace(value, `"kind":"first-observed",`, "", 1) },
		"misplaced known field": func(value string) string {
			return strings.Replace(value, `"kind":"first-observed"`, `"kind":"first-observed","scope":"uv.lock"`, 1)
		},
		"null array": func(value string) string {
			return strings.Replace(value, `"vulnerabilities":[]`, `"vulnerabilities":null`, 1)
		},
		"null scalar": func(value string) string {
			return strings.Replace(value, `"coverage":"complete"`, `"coverage":null`, 1)
		},
		"wrong primitive": func(value string) string {
			return strings.Replace(value, `"coverage":"complete"`, `"coverage":true`, 1)
		},
		"wrong case": func(value string) string { return strings.Replace(value, `"kind":`, `"Kind":`, 1) },
		"duplicate nested field": func(value string) string {
			return strings.Replace(value, `"kind":`, `"kind":"first-observed","kind":`, 1)
		},
		"escaped duplicate": func(value string) string {
			return strings.Replace(value, `"kind":`, `"k\u0069nd":"first-observed","kind":`, 1)
		},
		"trailing document": func(value string) string { return value + " {}" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := []byte(change(string(payload)))
			if bytes.Equal(changed, payload) {
				t.Fatal("fixture mutation did not alter the signed input")
			}
			_, err := verifySignedGitEvidence(fixture.sign(t, changed), fixture.repo.Config.SupplyChain.GitEvidence.Providers[0])
			if failure, ok := err.(*gitEvidenceError); !ok || failure.category != "invalid" {
				t.Fatalf("signed malformed payload was not rejected as invalid: %v", err)
			}
		})
	}
}

func TestGitEvidenceJSONRetainsByteEncodingAndNestingLimits(t *testing.T) {
	t.Parallel()
	valid := []byte(`{"payloadType":"example","payload":"","signatures":[]}`)
	for name, data := range map[string][]byte{
		"oversized":    append(valid, bytes.Repeat([]byte(" "), maximumGitEvidenceBytes)...),
		"invalid utf8": append(valid, 0xff),
		"deep nesting": []byte(strings.Repeat("[", 18) + "0" + strings.Repeat("]", 18)),
		"truncated":    valid[:len(valid)-1],
	} {
		t.Run(name, func(t *testing.T) {
			var envelope gitEvidenceEnvelope
			if err := decodeGitEvidenceJSON(data, &envelope); err == nil {
				t.Fatal("unbounded or malformed evidence was accepted")
			}
		})
	}
	for _, depth := range []int{16, 17} {
		data := []byte(strings.Repeat("[", depth) + "0" + strings.Repeat("]", depth))
		if err := policyschema.ValidateUniqueJSON(data, 16); (err == nil) != (depth == 16) {
			t.Fatalf("nesting depth %d: %v", depth, err)
		}
	}
}
