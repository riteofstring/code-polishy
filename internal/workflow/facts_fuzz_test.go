package workflow

import (
	"encoding/json"
	"testing"
)

func FuzzWorkflowFactsBoundary(f *testing.F) {
	f.Add([]byte("on: push\njobs:\n  test:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n"))
	f.Add([]byte("on: [push\n"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, source []byte) {
		if len(source) > MaximumInputBytes+1 {
			return
		}
		facts, err := Parse(".github/workflows/fuzz.yml", source)
		if err != nil {
			return
		}
		if facts.ProtocolVersion != ProtocolVersion || facts.ParserIdentity != ParserIdentity {
			t.Fatalf("facts = %+v", facts)
		}
		encoded, err := json.Marshal(facts)
		if err != nil || len(encoded) > maximumFactsBytes {
			t.Fatalf("encoded facts = %d bytes, %v", len(encoded), err)
		}
	})
}
