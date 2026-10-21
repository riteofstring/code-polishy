package pythonfacts

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestReachabilityResolvesRelativeReexportsAndAliases(t *testing.T) {
	modules, input := reachabilityTestInput(t, false)
	result, err := resolveReachabilityForTest(t, modules, []ReachabilityInput{input})
	if err != nil || len(result.Problems) != 0 || len(result.Evidence) != 1 {
		t.Fatalf("resolve consumer: %+v, %v", result, err)
	}
	target := result.Evidence[0].Targets[0]
	if target.Module != "pkg.api" || target.Symbol != "run" || !slices.ContainsFunc(target.Definitions, func(definition ReachabilityDefinition) bool {
		return definition.Path == "src/pkg/implementation.py" && definition.Name == "execute" && definition.Line == 1
	}) {
		t.Fatalf("relative re-export lost its actual implementation: %+v", target)
	}
}

func TestReachabilityRejectsDuplicateConsumers(t *testing.T) {
	modules, input := reachabilityTestInput(t, false)
	duplicate := input
	duplicate.ID = "competing-consumer"
	result, err := resolveReachabilityForTest(t, modules, []ReachabilityInput{input, duplicate})
	if err != nil || len(result.Evidence) != 0 || len(result.Problems) != 2 {
		t.Fatalf("duplicate declarations acquired reachability: %+v, %v", result, err)
	}
}

func TestReachabilityRejectsStaleRegistryEvidence(t *testing.T) {
	modules, input := reachabilityTestInput(t, true)
	result, err := resolveReachabilityForTest(t, modules, []ReachabilityInput{input})
	if err != nil || len(result.Problems) != 0 || len(result.Evidence) != 1 {
		t.Fatalf("resolve registry: %+v, %v", result, err)
	}
	files := []string{}
	for _, module := range modules {
		files = append(files, module.Path)
	}
	if err := ValidateReachabilityEvidence(files, []ReachabilityInput{input}, []string{input.ID}, result.Evidence); err != nil {
		t.Fatal(err)
	}
	input.Registry += "\n"
	if err := ValidateReachabilityEvidence(files, []ReachabilityInput{input}, []string{input.ID}, result.Evidence); err == nil {
		t.Fatal("registry evidence survived an input-byte change")
	}
}

func TestReachabilityRejectsAlteredTargetEvidence(t *testing.T) {
	modules, input := reachabilityTestInput(t, false)
	result, err := resolveReachabilityForTest(t, modules, []ReachabilityInput{input})
	if err != nil || len(result.Evidence) != 1 || len(result.Problems) != 0 {
		t.Fatalf("resolve original evidence: %+v, %v", result, err)
	}
	files := []string{}
	for _, module := range modules {
		files = append(files, module.Path)
	}
	encoded, err := json.Marshal(result.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		mutate   func([]any) []any
		resolved []string
	}{
		"missing evidence":   {func(evidence []any) []any { return []any{} }, []string{input.ID}},
		"duplicate evidence": {func(evidence []any) []any { return append(evidence, evidence[0]) }, []string{input.ID}},
		"substituted target": {func(evidence []any) []any {
			reachabilityTestTarget(evidence)["symbol"] = "unrelated"
			return evidence
		}, []string{input.ID}},
		"foreign definition": {func(evidence []any) []any {
			reachabilityTestTarget(evidence)["definitions"].([]any)[0].(map[string]any)["path"] = "other/secret.py"
			return evidence
		}, []string{input.ID}},
		"unresolved evidence": {func(evidence []any) []any { return evidence }, nil},
	} {
		t.Run(name, func(t *testing.T) {
			var value []any
			if err := json.Unmarshal(encoded, &value); err != nil {
				t.Fatal(err)
			}
			changed, err := json.Marshal(test.mutate(value))
			if err != nil {
				t.Fatal(err)
			}
			var evidence []ReachabilityEvidence
			if err := json.Unmarshal(changed, &evidence); err != nil {
				t.Fatal(err)
			}
			if err := ValidateReachabilityEvidence(files, []ReachabilityInput{input}, test.resolved, evidence); err == nil {
				t.Fatal("altered evidence acquired reachability")
			}
		})
	}
	if err := ValidateReachabilityEvidence(files, []ReachabilityInput{input, input}, []string{input.ID}, result.Evidence); err == nil {
		t.Fatal("duplicate input identities acquired reachability")
	}
}

func reachabilityTestTarget(evidence []any) map[string]any {
	return evidence[0].(map[string]any)["targets"].([]any)[0].(map[string]any)
}

func reachabilityTestInput(t *testing.T, registry bool) ([]TypeModule, ReachabilityInput) {
	t.Helper()
	argument, imports, parameters, line := "'pkg.api:run'", "from pkgutil import resolve_name as load_object\n", "", 3
	declaration := map[string]any{"kind": "target", "project": "pyproject.toml", "target": map[string]any{"module": "pkg.api", "symbol": "run"}}
	input := ReachabilityInput{ID: "loader-consumer"}
	if registry {
		argument = "json.loads(Path('src/registry.json').read_text(encoding='utf-8'))['plugins'][name]"
		imports += "import json\nfrom pathlib import Path\n"
		parameters, line = "name", 5
		declaration["kind"] = "registry"
		delete(declaration, "target")
		declaration["registry"] = map[string]any{"path": "src/registry.json", "jsonPointer": "/plugins"}
		input.Registry = `{"plugins":{"default":"pkg.api:run"}}`
	}
	project, err := AnalyzeProjectSources(t.Context(), typeTestInterpreter(t), []Input{
		{Path: "src/app.py", Source: imports + "def load(" + parameters + "):\n    return load_object(" + argument + ")\n"},
		{Path: "src/pkg/__init__.py", Source: ""},
		{Path: "src/pkg/api.py", Source: "from .implementation import execute as implementation\nrun = implementation\n"},
		{Path: "src/pkg/implementation.py", Source: "def execute():\n    return 1\n"},
	})
	if err != nil {
		t.Fatal(err)
	}
	declaration["consumer"] = map[string]any{"kind": "callsite", "importer": "src/app.py", "module": "app", "callable": "load", "site": map[string]any{"line": line, "column": 12}, "callee": "pkgutil.resolve_name", "shape": "module-object-call/v1", "argument": argument, "sourceSha256": project.Sources[0].SHA256}
	input.Declaration, err = json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	return typeTestModules(project.Sources), input
}
