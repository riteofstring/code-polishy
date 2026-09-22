package main

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestParseVultureResponseReturnsValidatedFacts(t *testing.T) {
	t.Parallel()
	fact := deadCodeFact{Analyzer: "vulture", Path: "src/app.py", Line: 2, EndLine: 2, Name: "unused", Kind: "variable", Confidence: 60, Message: "unused variable 'unused'"}
	data, err := json.Marshal(map[string]any{
		"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/app.py"},
		"deadCode": []deadCodeFact{fact}, "failure": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseVultureResponse(data, []string{"src/app.py"})
	if err != nil || !slices.Equal(got, []deadCodeFact{fact}) {
		t.Fatalf("facts = %+v, error = %v", got, err)
	}
}

func TestParseVultureResponseRejectsInvalidEvidence(t *testing.T) {
	t.Parallel()
	validFact := deadCodeFact{Analyzer: "vulture", Path: "src/app.py", Line: 2, EndLine: 2, Name: "unused", Kind: "variable", Confidence: 60, Message: "unused variable 'unused'"}
	tests := map[string]map[string]any{
		"wrong coverage": {
			"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/other.py"},
			"deadCode": []deadCodeFact{}, "failure": "",
		},
		"invalid fact": {
			"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/app.py"},
			"deadCode": []deadCodeFact{{Analyzer: "vulture", Path: "src/app.py", Line: 2, EndLine: 1, Name: "unused", Kind: "variable", Confidence: 60, Message: "unused"}}, "failure": "",
		},
		"duplicate fact": {
			"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/app.py"},
			"deadCode": []deadCodeFact{validFact, validFact}, "failure": "",
		},
		"wrong version": {
			"protocol": vultureProtocol, "toolVersion": "2.15", "covered": []string{"src/app.py"},
			"deadCode": []deadCodeFact{}, "failure": "",
		},
	}
	for name, value := range tests {
		name, value := name, value
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseVultureResponse(data, []string{"src/app.py"}); err == nil {
				t.Fatal("invalid Vulture evidence passed")
			}
		})
	}
	unknown := []byte(`{"protocol":"code-polishy-python-vulture/v1","toolVersion":"2.16","covered":["src/app.py"],"deadCode":[],"failure":"","extra":true}`)
	if _, err := parseVultureResponse(unknown, []string{"src/app.py"}); err == nil {
		t.Fatal("unknown Vulture response field passed")
	}
}
