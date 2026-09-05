package pythonfacts

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestRuntimeChecksRejectStaleDeclarations(t *testing.T) {
	modules, input := runtimeTestInput(t, runtimeTestLoaderImports+"def load(name):\n"+runtimeTestGuardBody, runtimeTestProtocolSource, "isinstance", "contracts.Contract")
	for name, value := range map[string]any{
		"importer": "other.py", "module": "other", "callable": "other",
		"sourceSha256": strings.Repeat("a", 64), "site": SourceLocation{Line: 3, Column: 1},
		"callee": "unknown.resolve", "argument": "other", "shape": "unknown/v1",
	} {
		t.Run(name, func(t *testing.T) {
			changed := input
			var consumer map[string]any
			if err := json.Unmarshal(input.Consumer, &consumer); err != nil {
				t.Fatal(err)
			}
			consumer[name] = value
			encoded, err := json.Marshal(consumer)
			if err != nil {
				t.Fatal(err)
			}
			changed.Consumer = encoded
			assertRuntimeCheckRejected(t, modules, changed)
		})
	}
	for name, change := range map[string]RuntimeCheck{
		"check moved":      {Kind: "isinstance", Protocol: "contracts.Contract", Site: SourceLocation{Line: 5, Column: 1}},
		"protocol changed": {Kind: "isinstance", Protocol: "contracts.Other", Site: input.Check.Site},
		"shape changed":    {Kind: "issubclass", Protocol: "contracts.Contract", Site: input.Check.Site},
	} {
		t.Run(name, func(t *testing.T) {
			changed := input
			changed.Check = change
			assertRuntimeCheckRejected(t, modules, changed)
		})
	}
	duplicate := input
	duplicate.ID = "second-declaration"
	result, err := ResolveRuntimeChecks(t.Context(), typeTestInterpreter(t), modules, []RuntimeCheckInput{input, duplicate})
	if err != nil || len(result.Evidence) != 0 || len(result.Problems) != 2 {
		t.Fatalf("duplicate declarations acquired runtime evidence: %+v, %v", result, err)
	}
}

func TestRuntimeCheckOutputRetainsExactSourceAndValueCustody(t *testing.T) {
	body := "    loaded = resolve(name)\n    plugin = loaded\n    if not isinstance(plugin, Contract):\n        raise TypeError\n    return plugin\n"
	modules, input := runtimeTestInput(t, runtimeTestLoaderImports+"def load(name):\n"+body, runtimeTestProtocolSource, "isinstance", "contracts.Contract")
	result, err := ResolveRuntimeChecks(t.Context(), typeTestInterpreter(t), modules, []RuntimeCheckInput{input})
	if err != nil || len(result.Evidence) != 1 {
		t.Fatalf("runtime evidence: %+v, %v", result, err)
	}
	covered := []typeCoverage{}
	for _, module := range modules {
		covered = append(covered, typeCoverage{Path: module.Path, SourceSHA256: module.SourceSHA256})
	}
	encoded, err := json.Marshal(map[string]any{"protocol": runtimeCheckProtocol, "covered": covered, "evidence": result.Evidence, "problems": result.Problems})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeRuntimeCheckProject(encoded, modules, []RuntimeCheckInput{input}); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"omitted source": func(value map[string]any) { value["covered"] = value["covered"].([]any)[1:] },
		"source digest changed": func(value map[string]any) {
			value["covered"].([]any)[0].(map[string]any)["sourceSha256"] = strings.Repeat("a", 64)
		},
		"source order changed": func(value map[string]any) { slices.Reverse(value["covered"].([]any)) },
		"missing evidence":     func(value map[string]any) { value["evidence"] = []any{} },
		"duplicate evidence": func(value map[string]any) {
			evidence := value["evidence"].([]any)
			value["evidence"] = append(evidence, evidence[0])
		},
		"unknown consumer": func(value map[string]any) { runtimeTestEvidence(value)["id"] = "missing" },
		"contract changed": func(value map[string]any) { runtimeTestEvidence(value)["contract"] = "contracts.Other" },
		"loader span changed": func(value map[string]any) {
			runtimeTestEvidence(value)["loaderEndSite"].(map[string]any)["column"] = 100
		},
		"check span changed": func(value map[string]any) {
			runtimeTestEvidence(value)["checkEndSite"].(map[string]any)["column"] = 100
		},
		"invalid identity": func(value map[string]any) { runtimeTestEvidence(value)["identity"] = strings.Repeat("z", 64) },
		"missing bindings": func(value map[string]any) { delete(runtimeTestEvidence(value), "bindings") },
		"dropped bindings": func(value map[string]any) { runtimeTestEvidence(value)["bindings"] = []any{} },
		"wrong binding": func(value map[string]any) {
			runtimeTestEvidence(value)["bindings"].([]any)[0].(map[string]any)["name"] = "unrelated"
		},
		"foreign binding": func(value map[string]any) {
			runtimeTestEvidence(value)["bindings"].([]any)[0].(map[string]any)["path"] = "contracts.py"
		},
		"reordered bindings": func(value map[string]any) { slices.Reverse(runtimeTestEvidence(value)["bindings"].([]any)) },
		"dropped alias": func(value map[string]any) {
			runtimeTestEvidence(value)["bindings"] = runtimeTestEvidence(value)["bindings"].([]any)[:1]
		},
		"conflicting outcome": func(value map[string]any) {
			value["problems"] = []any{map[string]any{"id": input.ID, "message": "stale"}}
		},
		"unknown problem": func(value map[string]any) {
			value["problems"] = []any{map[string]any{"id": "unknown", "message": "stale"}}
		},
		"unknown field": func(value map[string]any) { value["trusted"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(encoded, &value); err != nil {
				t.Fatal(err)
			}
			mutate(value)
			changed, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeRuntimeCheckProject(changed, modules, []RuntimeCheckInput{input}); err == nil {
				t.Fatal("altered runtime evidence was admitted")
			}
		})
	}
	if _, err := decodeRuntimeCheckProject(encoded, modules, []RuntimeCheckInput{input, input}); err == nil {
		t.Fatal("duplicate input identity was admitted")
	}
}

func runtimeTestEvidence(value map[string]any) map[string]any {
	return value["evidence"].([]any)[0].(map[string]any)
}
