package architecture

import (
	"encoding/json"
	"reflect"
	"testing"
)

func FuzzRuffGraphFactsBoundary(f *testing.F) {
	f.Add([]byte(`{"src/app.py":["src/model.py"]}`))
	f.Add([]byte(`{"src\\app.py":["src\\model.py"]}`))
	f.Add([]byte(`{"src/app.py":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > pythonGraphMaximumBytes+1 {
			return
		}
		facts, err := adaptPythonGraph(data)
		if err != nil {
			return
		}
		if facts.Protocol != pythonGraphProtocol || facts.RuffVersion != pythonGraphRuffVersion {
			t.Fatalf("facts = %+v", facts)
		}
		encoded, err := json.Marshal(facts.Graph)
		if err != nil {
			t.Fatal(err)
		}
		reparsed, err := adaptPythonGraph(encoded)
		if err != nil || !reflect.DeepEqual(reparsed.Graph, facts.Graph) {
			t.Fatalf("reparsed = %+v, %v", reparsed, err)
		}
	})
}
