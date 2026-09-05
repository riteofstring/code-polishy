package pythonfacts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestContractDefinitionsResolveOwnedInterfaces(t *testing.T) {
	modules := contractDefinitionModules(t, []Input{
		{Path: "src/framework/__init__.py", Source: "from .interfaces import Contract\nfrom .hooks import decorate, register\n"},
		{Path: "src/framework/interfaces.py", Source: "from typing import Protocol\nclass Contract(Protocol):\n    def on_event(self):\n        raise NotImplementedError\n"},
		{Path: "src/framework/hooks.py", Source: "from .interfaces import Contract\ndef decorate(function):\n    return function\ndef register(plugin: type[Contract]):\n    return plugin\n"},
	})
	requests := []ContractDefinitionRequest{
		{ID: "base", Kind: "base", Qualified: "framework.Contract", Member: "on_event"},
		{ID: "protocol", Kind: "protocol", Qualified: "framework.Contract", Member: "on_event"},
		{ID: "decorator", Kind: "decorator", Qualified: "framework.decorate", Member: "on_event"},
		{ID: "registration", Kind: "entry-point", Qualified: "framework.register", Member: "on_event"},
	}
	result, err := ResolveContractDefinitions(t.Context(), typeTestInterpreter(t), modules, requests)
	if err != nil || len(result.Problems) != 0 || len(result.Evidence) != 4 {
		t.Fatalf("owned contracts = %+v, %v", result, err)
	}
	for _, evidence := range result.Evidence {
		last := evidence.Definitions[len(evidence.Definitions)-1]
		if evidence.Kind != "decorator" && (last.Name != "on_event" || last.Path != "src/framework/interfaces.py") {
			t.Fatalf("contract lost its actual interface member: %+v", evidence)
		}
	}
	slices.Reverse(modules)
	reversed, err := ResolveContractDefinitions(t.Context(), typeTestInterpreter(t), modules, requests)
	if err != nil || !reflect.DeepEqual(result, reversed) {
		t.Fatalf("module ordering changed contract evidence: %+v, %v", reversed, err)
	}
}

func TestContractDefinitionsRejectInventedOrUnownedMembers(t *testing.T) {
	for name, source := range map[string]string{
		"absent member":     "class Contract:\n    def unrelated(self):\n        return 1\n",
		"global lookalike":  "def on_event():\n    return 1\nclass Contract:\n    pass\n",
		"foreign reexport":  "from foreign_package import Contract\n",
		"duplicate binding": "class Contract:\n    def on_event(self):\n        return 1\nContract = object\n",
		"ambiguous method":  "class Contract:\n    def on_event(self):\n        return 1\n    def on_event(self):\n        return 2\n",
	} {
		t.Run(name, func(t *testing.T) {
			modules := contractDefinitionModules(t, []Input{{Path: "src/framework.py", Source: source}})
			result, err := ResolveContractDefinitions(t.Context(), typeTestInterpreter(t), modules, []ContractDefinitionRequest{{ID: "contract", Kind: "base", Qualified: "framework.Contract", Member: "on_event"}})
			if err != nil || len(result.Evidence) != 0 || len(result.Problems) != 1 {
				t.Fatalf("invented contract admitted: %+v, %v", result, err)
			}
		})
	}
}

func TestContractDefinitionsCrossSourcePartitions(t *testing.T) {
	inputs := make([]Input, 1001)
	for index := range inputs {
		inputs[index] = Input{Path: fmt.Sprintf("src/framework/module_%04d.py", index), Source: "# " + strings.Repeat("x", 8500) + "\n"}
	}
	inputs[0].Source += "from .module_0999 import Contract\n"
	inputs[999].Source += "from .module_1000 import Parent\nclass Contract(Parent):\n    pass\n"
	inputs[1000].Source += "class Parent:\n    def on_event(self):\n        return 1\n"
	project, err := AnalyzeProjectSources(t.Context(), typeTestInterpreter(t), inputs)
	if err != nil || len(project.Partitions) < 2 {
		t.Fatalf("partitioned sources = %+v, %v", project.Partitions, err)
	}
	result, err := ResolveContractDefinitions(t.Context(), typeTestInterpreter(t), typeTestModules(project.Sources), []ContractDefinitionRequest{{ID: "inherited", Kind: "base", Qualified: "framework.module_0000.Contract", Member: "on_event"}})
	if err != nil || len(result.Evidence) != 1 || len(result.Problems) != 0 {
		t.Fatalf("partitioned contract = %+v, %v", result, err)
	}
	definitions := result.Evidence[0].Definitions
	if definitions[len(definitions)-1].Path != inputs[1000].Path {
		t.Fatalf("contract lost inherited source across partitions: %+v", definitions)
	}
}

func TestContractDefinitionsRejectUnprovenProtocolAndRegistration(t *testing.T) {
	modules := contractDefinitionModules(t, []Input{{Path: "src/framework.py", Source: "class Contract:\n    def on_event(self):\n        return 1\ndef register(plugin):\n    return plugin\n"}})
	requests := []ContractDefinitionRequest{
		{ID: "protocol", Kind: "protocol", Qualified: "framework.Contract", Member: "on_event"},
		{ID: "registration", Kind: "entry-point", Qualified: "framework.register", Member: "on_event"},
	}
	result, err := ResolveContractDefinitions(t.Context(), typeTestInterpreter(t), modules, requests)
	if err != nil || len(result.Evidence) != 0 || len(result.Problems) != 2 {
		t.Fatalf("unproven interface acquired consumer evidence: %+v, %v", result, err)
	}
}

func TestContractDefinitionsUseStubsOnlyWithoutRuntimeSource(t *testing.T) {
	project, err := AnalyzeProjectSources(t.Context(), typeTestInterpreter(t), []Input{{Path: "src/framework.pyi", Source: "class Contract:\n    def on_event(self) -> None: ...\n"}})
	if err != nil {
		t.Fatal(err)
	}
	stub := project.Sources[0]
	modules := []TypeModule{{Path: stub.Path, Module: "framework", SourceSHA256: stub.SHA256, Facts: stub.TypeFacts}}
	requests := []ContractDefinitionRequest{{ID: "base", Kind: "base", Qualified: "framework.Contract", Member: "on_event"}}
	result, err := ResolveContractDefinitions(t.Context(), typeTestInterpreter(t), modules, requests)
	if err != nil || len(result.Evidence) != 1 || result.Evidence[0].Definitions[0].Path != stub.Path {
		t.Fatalf("owned stub contract was not resolved: %+v, %v", result, err)
	}
	modules = append(modules, contractDefinitionModules(t, []Input{{Path: "src/framework.py", Source: "class Contract:\n    pass\n"}})...)
	result, err = ResolveContractDefinitions(t.Context(), typeTestInterpreter(t), modules, requests)
	if err != nil || len(result.Evidence) != 0 || len(result.Problems) != 1 {
		t.Fatalf("stub invented a member absent from runtime source: %+v, %v", result, err)
	}
}

func TestContractDefinitionsRejectAlteredResponseCustody(t *testing.T) {
	modules := contractDefinitionModules(t, []Input{{Path: "src/framework.py", Source: "class Contract:\n    def on_event(self):\n        return 1\n"}})
	requests := []ContractDefinitionRequest{{ID: "base", Kind: "base", Qualified: "framework.Contract", Member: "on_event"}}
	result, err := ResolveContractDefinitions(t.Context(), typeTestInterpreter(t), modules, requests)
	if err != nil || len(result.Evidence) != 1 {
		t.Fatalf("contract query failed: %+v, %v", result, err)
	}
	wire := map[string]any{"protocol": "python-contract-definitions/v1", "covered": []typeCoverage{{Path: modules[0].Path, SourceSHA256: modules[0].SourceSHA256}}, "evidence": result.Evidence, "problems": result.Problems}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeContractDefinitions(data, modules, requests); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"stale source": func(w map[string]any) {
			w["covered"].([]any)[0].(map[string]any)["sourceSha256"] = strings.Repeat("0", 64)
		},
		"wrong member":     func(w map[string]any) { w["evidence"].([]any)[0].(map[string]any)["member"] = "other" },
		"missing evidence": func(w map[string]any) { w["evidence"] = []any{} },
		"foreign source": func(w map[string]any) {
			w["evidence"].([]any)[0].(map[string]any)["definitions"].([]any)[0].(map[string]any)["path"] = "foreign.py"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			mutate(changed)
			encoded, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeContractDefinitions(encoded, modules, requests); err == nil {
				t.Fatal("substituted contract evidence accepted")
			}
		})
	}
}

func contractDefinitionModules(t *testing.T, inputs []Input) []TypeModule {
	t.Helper()
	project, err := AnalyzeProjectSources(t.Context(), typeTestInterpreter(t), inputs)
	if err != nil {
		t.Fatal(err)
	}
	return typeTestModules(project.Sources)
}
