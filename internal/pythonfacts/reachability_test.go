package pythonfacts

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestReachabilityResolvesRelativeReexportsAndAliases(t *testing.T) {
	modules, input := reachabilityTestInput(t, false)
	result, err := ResolveReachability(t.Context(), typeTestInterpreter(t), modules, []ReachabilityInput{input})
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
	result, err := ResolveReachability(t.Context(), typeTestInterpreter(t), modules, []ReachabilityInput{input, duplicate})
	if err != nil || len(result.Evidence) != 0 || len(result.Problems) != 2 {
		t.Fatalf("duplicate declarations acquired reachability: %+v, %v", result, err)
	}
}

func TestReachabilityRejectsStaleRegistryEvidence(t *testing.T) {
	modules, input := reachabilityTestInput(t, true)
	result, err := ResolveReachability(t.Context(), typeTestInterpreter(t), modules, []ReachabilityInput{input})
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

func TestReachabilityRejectsAlteredSourceAndTargetEvidence(t *testing.T) {
	modules, input := reachabilityTestInput(t, false)
	result, err := ResolveReachability(t.Context(), typeTestInterpreter(t), modules, []ReachabilityInput{input})
	if err != nil || len(result.Evidence) != 1 || len(result.Problems) != 0 {
		t.Fatalf("resolve original evidence: %+v, %v", result, err)
	}
	covered := []typeCoverage{}
	for _, module := range modules {
		covered = append(covered, typeCoverage{Path: module.Path, SourceSHA256: module.SourceSHA256})
	}
	encoded, err := json.Marshal(map[string]any{"protocol": "python-reachability-project/v1", "covered": covered, "evidence": result.Evidence, "problems": result.Problems})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeReachabilityProject(encoded, modules, []ReachabilityInput{input}); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"omitted source": func(value map[string]any) { value["covered"] = value["covered"].([]any)[1:] },
		"stale source": func(value map[string]any) {
			value["covered"].([]any)[0].(map[string]any)["sourceSha256"] = strings.Repeat("a", 64)
		},
		"reordered sources": func(value map[string]any) { slices.Reverse(value["covered"].([]any)) },
		"missing evidence":  func(value map[string]any) { value["evidence"] = []any{} },
		"duplicate evidence": func(value map[string]any) {
			evidence := value["evidence"].([]any)
			value["evidence"] = append(evidence, evidence[0])
		},
		"substituted target": func(value map[string]any) {
			reachabilityTestTarget(value)["symbol"] = "unrelated"
		},
		"foreign definition": func(value map[string]any) {
			reachabilityTestTarget(value)["definitions"].([]any)[0].(map[string]any)["path"] = "other/secret.py"
		},
		"unresolved evidence": func(value map[string]any) {
			value["problems"] = []any{map[string]any{"id": input.ID, "message": "consumer is stale"}}
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
			if _, err := decodeReachabilityProject(changed, modules, []ReachabilityInput{input}); err == nil {
				t.Fatal("altered evidence acquired reachability")
			}
		})
	}
	if _, err := decodeReachabilityProject(encoded, modules, []ReachabilityInput{input, input}); err == nil {
		t.Fatal("duplicate input identities acquired reachability")
	}
}

func reachabilityTestTarget(value map[string]any) map[string]any {
	return value["evidence"].([]any)[0].(map[string]any)["targets"].([]any)[0].(map[string]any)
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
