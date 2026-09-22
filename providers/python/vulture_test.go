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
		"deadCode": []deadCodeFact{fact}, "problems": []vultureProblem{}, "failure": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseVultureResponse(data, []string{"src/app.py"})
	if err != nil || !slices.Equal(got.Facts, []deadCodeFact{fact}) || len(got.Problems) != 0 {
		t.Fatalf("facts = %+v, error = %v", got, err)
	}
}

func TestParseVultureResponseRejectsInvalidEvidence(t *testing.T) {
	t.Parallel()
	validFact := deadCodeFact{Analyzer: "vulture", Path: "src/app.py", Line: 2, EndLine: 2, Name: "unused", Kind: "variable", Confidence: 60, Message: "unused variable 'unused'"}
	tests := map[string]map[string]any{
		"wrong coverage": {
			"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/other.py"},
			"deadCode": []deadCodeFact{}, "problems": []vultureProblem{}, "failure": "",
		},
		"invalid fact": {
			"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/app.py"},
			"deadCode": []deadCodeFact{{Analyzer: "vulture", Path: "src/app.py", Line: 2, EndLine: 1, Name: "unused", Kind: "variable", Confidence: 60, Message: "unused"}}, "problems": []vultureProblem{}, "failure": "",
		},
		"duplicate fact": {
			"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/app.py"},
			"deadCode": []deadCodeFact{validFact, validFact}, "problems": []vultureProblem{}, "failure": "",
		},
		"wrong version": {
			"protocol": vultureProtocol, "toolVersion": "2.15", "covered": []string{"src/app.py"},
			"deadCode": []deadCodeFact{}, "problems": []vultureProblem{}, "failure": "",
		},
		"facts with problems": {
			"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/app.py"},
			"deadCode": []deadCodeFact{validFact}, "problems": []vultureProblem{{ID: "manifest:missing", Message: "symbol is stale"}}, "failure": "",
		},
		"unsorted problems": {
			"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/app.py"},
			"deadCode": []deadCodeFact{}, "problems": []vultureProblem{{ID: "manifest:second", Message: "missing"}, {ID: "manifest:first", Message: "missing"}}, "failure": "",
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
	unknown := []byte(`{"protocol":"code-polishy-python-vulture/v1","toolVersion":"2.16","covered":["src/app.py"],"deadCode":[],"problems":[],"failure":"","extra":true}`)
	if _, err := parseVultureResponse(unknown, []string{"src/app.py"}); err == nil {
		t.Fatal("unknown Vulture response field passed")
	}
}

func TestParseVultureResponseReturnsReachabilityProblems(t *testing.T) {
	t.Parallel()
	problem := vultureProblem{ID: "manifest:missing", Message: "symbol is stale or ambiguous"}
	data, err := json.Marshal(map[string]any{
		"protocol": vultureProtocol, "toolVersion": vultureVersion, "covered": []string{"src/app.py"},
		"deadCode": []deadCodeFact{}, "problems": []vultureProblem{problem}, "failure": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseVultureResponse(data, []string{"src/app.py"})
	if err != nil || len(got.Facts) != 0 || !slices.Equal(got.Problems, []vultureProblem{problem}) {
		t.Fatalf("result = %+v, error = %v", got, err)
	}
}

func TestNewVultureRequestCarriesManifestReachability(t *testing.T) {
	t.Parallel()
	scope := analysisScope{
		Members: []string{"backend.py", "src/sample/__init__.py"},
		Data:    json.RawMessage(`{"manifest":"pyproject.toml","requiresPython":"==3.12.*","targetVersion":"py312","sourceRoots":[".","src"],"backendPaths":["."],"buildBackend":{"module":"backend","object":"Builder"},"entryPoints":[{"group":"console_scripts","name":"sample","module":"sample","symbol":"main"}],"problems":[]}`),
	}
	request, err := newVultureRequest(scope, scope.Members)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []vultureFile{{Path: "backend.py", Module: "backend"}, {Path: "src/sample/__init__.py", Module: "sample", Package: "sample"}}
	wantReferences := []vultureReference{{ID: "manifest:pyproject.toml:console_scripts:sample:sample:main", Module: "sample", Symbol: "main"}}
	wantBackends := []vultureBackend{{ID: "manifest:pyproject.toml:build-system.build-backend:backend:Builder", Module: "backend", Object: "Builder"}}
	if !slices.Equal(request.Files, wantFiles) || !slices.Equal(request.References, wantReferences) || !slices.Equal(request.Backends, wantBackends) {
		t.Fatalf("request = %+v", request)
	}
}
