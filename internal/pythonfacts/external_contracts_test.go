package pythonfacts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExternalReachabilityContractsBindLocalMembers(t *testing.T) {
	for _, kind := range []string{"base", "protocol", "decorator", "entry-point"} {
		t.Run(kind, func(t *testing.T) {
			modules, input := externalContractInput(t, kind)
			result, err := ResolveReachability(t.Context(), typeTestInterpreter(t), modules, []ReachabilityInput{input})
			if err != nil || len(result.Problems) != 0 || len(result.Evidence) != 1 || result.Evidence[0].Targets[0].Symbol != "Plugin.on_event" {
				t.Fatalf("external contract = %+v, %v", result, err)
			}
			input.Dependency.Identity = strings.Repeat("b", 64)
			if err := ValidateReachabilityEvidence([]string{modules[0].Path}, []ReachabilityInput{input}, []string{input.ID}, result.Evidence); err == nil {
				t.Fatal("changed dependency retained previously accepted evidence")
			}
			changed, err := ResolveReachability(t.Context(), typeTestInterpreter(t), modules, []ReachabilityInput{input})
			if err != nil || len(changed.Evidence) != 1 || changed.Evidence[0].Identity == result.Evidence[0].Identity {
				t.Fatalf("dependency change did not invalidate identity: %+v, %v", changed, err)
			}
		})
	}
}

func TestExternalReachabilityRequiresDependencyDefinitionProof(t *testing.T) {
	for _, kind := range []string{"base", "protocol", "decorator", "entry-point"} {
		t.Run(kind, func(t *testing.T) {
			modules, input := externalContractInput(t, kind)
			input.Dependency.Contract = nil
			result, err := ResolveReachability(t.Context(), typeTestInterpreter(t), modules, []ReachabilityInput{input})
			if err != nil || len(result.Evidence) != 0 || len(result.Problems) != 1 {
				t.Fatalf("unproven dependency preserved a local member: %+v, %v", result, err)
			}
		})
	}
}

func TestExternalReachabilityRejectsDisconnectedContracts(t *testing.T) {
	for name, change := range map[string]func(map[string]any){
		"wrong type":           func(c map[string]any) { c["qualified"] = "framework.Other" },
		"wrong member":         func(c map[string]any) { c["member"] = "unused" },
		"wrong implementation": func(c map[string]any) { c["implementation"] = "Other" },
		"stale source":         func(c map[string]any) { c["sourceSha256"] = strings.Repeat("f", 64) },
		"stale binding":        func(c map[string]any) { c["site"] = map[string]int{"line": 3, "column": 1} },
		"wrong dependency":     func(c map[string]any) { c["distribution"] = "unadmitted" },
	} {
		t.Run(name, func(t *testing.T) {
			modules, input := externalContractInput(t, "base")
			var declaration map[string]any
			if err := json.Unmarshal(input.Declaration, &declaration); err != nil {
				t.Fatal(err)
			}
			change(declaration["consumer"].(map[string]any))
			var err error
			input.Declaration, err = json.Marshal(declaration)
			if err != nil {
				t.Fatal(err)
			}
			result, err := ResolveReachability(t.Context(), typeTestInterpreter(t), modules, []ReachabilityInput{input})
			if err != nil || len(result.Evidence) != 0 || len(result.Problems) != 1 {
				t.Fatalf("disconnected contract accepted: %+v, %v", result, err)
			}
		})
	}
}

func externalContractInput(t *testing.T, kind string) ([]TypeModule, ReachabilityInput) {
	t.Helper()
	source := "from framework import Contract\nclass Plugin(Contract):\n    def on_event(self):\n        return 1\n    def unused(self):\n        return 2\n"
	line, column := 2, 1
	if kind == "decorator" {
		source = "from framework import Contract\nclass Plugin:\n    @Contract\n    def on_event(self):\n        return 1\n"
		line, column = 4, 5
	} else if kind == "entry-point" {
		source = "from framework import Contract\nclass Plugin:\n    def on_event(self):\n        return 1\nContract(Plugin)\n"
		line = 5
	}
	project, err := AnalyzeProjectSources(t.Context(), typeTestInterpreter(t), []Input{{Path: "src/app.py", Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := json.Marshal(map[string]any{
		"kind": "target", "project": "pyproject.toml", "target": map[string]string{"module": "app", "symbol": "Plugin.on_event"},
		"consumer": map[string]any{"kind": kind, "module": "app", "importer": "src/app.py", "implementation": "Plugin", "member": "on_event", "qualified": "framework.Contract", "distribution": "framework", "site": map[string]int{"line": line, "column": column}, "sourceSha256": project.Sources[0].SHA256},
	})
	if err != nil {
		t.Fatal(err)
	}
	contractSource := "class Contract:\n    def on_event(self):\n        raise NotImplementedError\n"
	switch kind {
	case "protocol":
		contractSource = "from typing import Protocol\nclass Contract(Protocol):\n    def on_event(self):\n        raise NotImplementedError\n"
	case "decorator":
		contractSource = "def Contract(function):\n    return function\n"
	case "entry-point":
		contractSource = "class Interface:\n    def on_event(self):\n        raise NotImplementedError\ndef Contract(plugin: type[Interface]):\n    return plugin\n"
	}
	modules := contractDefinitionModules(t, []Input{{Path: "src/framework.py", Source: contractSource}})
	proof, err := ResolveContractDefinitions(t.Context(), typeTestInterpreter(t), modules, []ContractDefinitionRequest{{ID: "contract", Kind: kind, Qualified: "framework.Contract", Member: "on_event"}})
	if err != nil || len(proof.Evidence) != 1 {
		t.Fatalf("dependency contract = %+v, %v", proof, err)
	}
	return typeTestModules(project.Sources), ReachabilityInput{ID: "external-member", Declaration: declaration, Dependency: &ReachabilityDependency{Distribution: "framework", Identity: strings.Repeat("a", 64), Contract: &proof.Evidence[0]}}
}
