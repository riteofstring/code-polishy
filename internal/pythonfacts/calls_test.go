package pythonfacts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type consumerFactSite struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type consumerFactArgument struct {
	Kind    string           `json:"kind"`
	Value   string           `json:"value"`
	Text    string           `json:"text"`
	Site    consumerFactSite `json:"site"`
	EndSite consumerFactSite `json:"endSite"`
}

type consumerFactCall struct {
	Scope     string                 `json:"scope"`
	Site      consumerFactSite       `json:"site"`
	EndSite   consumerFactSite       `json:"endSite"`
	Callee    consumerFactArgument   `json:"callee"`
	Arguments []consumerFactArgument `json:"arguments"`
	Keywords  []struct {
		Name  *string              `json:"name"`
		Value consumerFactArgument `json:"value"`
	} `json:"keywords"`
	Conditional bool `json:"conditional"`
}

type consumerFactDocument struct {
	Bindings []struct {
		Scope        string            `json:"scope"`
		Name         string            `json:"name"`
		Reference    string            `json:"reference"`
		Site         consumerFactSite  `json:"site"`
		ValueSite    *consumerFactSite `json:"valueSite"`
		ValueEndSite *consumerFactSite `json:"valueEndSite"`
	} `json:"bindings"`
	Calls []consumerFactCall `json:"calls"`
}

func TestSourceFactsRetainLoaderAndCheckDataFlowWithoutExecutingPython(t *testing.T) {
	python := typeTestInterpreter(t)
	source := "from pkgutil import resolve_name as resolve\nfrom plugin_api import Contract\ndef load(name):\n    plugin = resolve(name)\n    if not isinstance(plugin, Contract):\n        raise ValueError('unsupported plugin')\n    return plugin\ndef unrelated():\n    plugin = object()\n    return plugin\nraise RuntimeError('target must never execute')\n"
	project, err := AnalyzeProjectSources(t.Context(), python, []Input{{Path: "loader.py", Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	facts := decodeConsumerTestFacts(t, project.Sources[0])
	loader := consumerTestCall(t, facts, "resolve")
	check := consumerTestCall(t, facts, "isinstance")
	unrelated := consumerTestCall(t, facts, "object")
	if loader.Scope != check.Scope || loader.Scope == unrelated.Scope || loader.Site != (consumerFactSite{Line: 4, Column: 14}) || check.Arguments[0].Value != "plugin" || check.Arguments[1].Value != "Contract" {
		t.Fatalf("loader/check lost exact lexical or argument evidence: %+v, %+v, %+v", loader, check, unrelated)
	}
	bound := false
	for _, binding := range facts.Bindings {
		if binding.Scope == loader.Scope && binding.Name == "plugin" {
			bound = binding.ValueSite != nil && binding.ValueEndSite != nil && *binding.ValueSite == loader.Site && *binding.ValueEndSite == loader.EndSite
		}
	}
	if !bound {
		t.Fatal("loaded value has no exact assignment binding")
	}
	if _, err := ResolveTypeProject(t.Context(), python, typeTestModules(project.Sources)); err != nil {
		t.Fatal(err)
	}
}

func TestConsumerCallsRetainKeywordExpansionAndUTF8ByteLocations(t *testing.T) {
	python := typeTestInterpreter(t)
	project, err := AnalyzeProjectSources(t.Context(), python, []Input{{Path: "app.py", Source: "def call(items, options):\n    café = 'é'; result = dispatch('app:Plugin', *items, mode='strict', **options)\n    return café, result\n"}})
	if err != nil {
		t.Fatal(err)
	}
	call := consumerTestCall(t, decodeConsumerTestFacts(t, project.Sources[0]), "dispatch")
	if len(call.Arguments) != 2 || call.Arguments[0].Kind != "string" || call.Arguments[0].Value != "app:Plugin" || call.Arguments[1].Kind != "starred" || len(call.Keywords) != 2 || call.Keywords[0].Name == nil || *call.Keywords[0].Name != "mode" || call.Keywords[1].Name != nil {
		t.Fatalf("call shape lost expansion or literal facts: %+v", call)
	}
	if call.Site.Column != len("    café = 'é'; result = ")+1 || call.EndSite.Line != 2 {
		t.Fatalf("call location is not a UTF-8 byte span: %+v", call)
	}
}

func TestNestedConsumerCallsRetainDistinctSpans(t *testing.T) {
	python := typeTestInterpreter(t)
	project, err := AnalyzeProjectSources(t.Context(), python, []Input{{Path: "app.py", Source: "def invoke(name):\n    result = resolve(name)()\n    return result\n"}})
	if err != nil {
		t.Fatal(err)
	}
	facts := decodeConsumerTestFacts(t, project.Sources[0])
	if len(facts.Calls) != 2 || facts.Calls[0].Site != facts.Calls[1].Site || facts.Calls[0].EndSite.Column <= facts.Calls[1].EndSite.Column {
		t.Fatalf("nested call identity lost its span: %+v", facts.Calls)
	}
	bound := false
	for _, binding := range facts.Bindings {
		if binding.Name == "result" {
			bound = binding.ValueSite != nil && binding.ValueEndSite != nil && *binding.ValueSite == facts.Calls[0].Site && *binding.ValueEndSite == facts.Calls[0].EndSite
		}
	}
	if !bound {
		t.Fatal("assignment cannot distinguish the outer call from its callee")
	}
	if _, err := ResolveTypeProject(t.Context(), python, typeTestModules(project.Sources)); err != nil {
		t.Fatal(err)
	}
}

func TestConsumerCallsSurviveDeterministicProjectPartitions(t *testing.T) {
	python := typeTestInterpreter(t)
	inputs := []Input{{Path: "a_loader.py", Source: "from z_bridge import resolve_name, Contract\ndef load():\n    plugin = resolve_name('z_plugin:Plugin')\n    if not issubclass(plugin, Contract):\n        raise TypeError\n    return plugin\n"}}
	for index := range 1000 {
		inputs = append(inputs, Input{Path: fmt.Sprintf("filler_%04d.py", index), Source: "# " + strings.Repeat("x", 8500) + "\n"})
	}
	inputs = append(inputs, Input{Path: "z_bridge.py", Source: "from pkgutil import resolve_name\nfrom z_contracts import Contract\n"}, Input{Path: "z_contracts.py", Source: runtimeTestProtocolSource}, Input{Path: "z_plugin.py", Source: "class Plugin:\n    def run(self):\n        return 1\n"})
	first, err := AnalyzeProjectSources(t.Context(), python, inputs)
	if err != nil || len(first.Partitions) < 2 {
		t.Fatalf("partitioned facts: %v", err)
	}
	call := consumerTestCall(t, decodeConsumerTestFacts(t, first.Sources[0]), "resolve_name")
	if call.Arguments[0].Value != "z_plugin:Plugin" || first.Partitions[0].LastPath >= "z_plugin.py" {
		t.Fatal("fixture did not retain a cross-partition target reference")
	}
	slices.Reverse(inputs)
	second, err := AnalyzeProjectSources(t.Context(), python, inputs)
	if err != nil || first.Identity != second.Identity || first.PartitionIdentity != second.PartitionIdentity || !reflect.DeepEqual(first.Sources, second.Sources) {
		t.Fatalf("reordered partitions changed consumer facts: %v", err)
	}
	if _, err := ResolveTypeProject(t.Context(), python, typeTestModules(first.Sources)); err != nil {
		t.Fatal(err)
	}
	declaration := fmt.Sprintf(`{"kind":"target","project":"pyproject.toml","target":{"module":"z_plugin","symbol":"Plugin"},"consumer":{"kind":"callsite","importer":"a_loader.py","module":"a_loader","callable":"load","site":{"line":3,"column":14},"callee":"pkgutil.resolve_name","shape":"module-object-call/v1","argument":"'z_plugin:Plugin'","sourceSha256":"%s"}}`, first.Sources[0].SHA256)
	requests := []ReachabilityInput{{ID: "configured-consumer", Declaration: json.RawMessage(declaration)}}
	resolved, err := ResolveReachability(t.Context(), python, typeTestModules(first.Sources), requests)
	if err != nil || len(resolved.Problems) != 0 || len(resolved.Evidence) != 1 || resolved.Evidence[0].Targets[0].Definitions[0].Path != "z_plugin.py" {
		t.Fatalf("consumer did not resolve its target across partitions: %+v, %v", resolved, err)
	}
	reordered, err := ResolveReachability(t.Context(), python, typeTestModules(second.Sources), requests)
	if err != nil || !reflect.DeepEqual(resolved, reordered) {
		t.Fatalf("consumer evidence changed with partition order: %+v, %v", reordered, err)
	}
	assertPartitionedObjectImports(t, python, first, second)
	assertPartitionedRuntimeChecks(t, python, first, second)
}

func assertPartitionedObjectImports(t *testing.T, python string, first, second SourceProject) {
	t.Helper()
	imports, err := ResolveObjectImports(t.Context(), python, ".", typeTestModules(first.Sources), nil)
	if err != nil || len(imports.Imports) != 1 || len(imports.Imports[0].Targets) != 1 || imports.Imports[0].Targets[0].Module != "z_plugin" {
		t.Fatalf("cross-partition object import was not resolved independently: %+v, %v", imports, err)
	}
	reorderedImports, err := ResolveObjectImports(t.Context(), python, ".", typeTestModules(second.Sources), nil)
	if err != nil || !reflect.DeepEqual(imports, reorderedImports) {
		t.Fatalf("object import identity changed with partition order: %+v, %v", reorderedImports, err)
	}
}

func TestTypeProjectRejectsMalformedConsumerEvidence(t *testing.T) {
	python := typeTestInterpreter(t)
	project, err := AnalyzeProjectSources(t.Context(), python, []Input{{Path: "app.py", Source: "def load(value):\n    return validate(value, mode='strict')\n"}})
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"missing calls":         func(f map[string]any) { delete(f, "calls") },
		"duplicate call":        func(f map[string]any) { calls := f["calls"].([]any); f["calls"] = append(calls, calls[0]) },
		"unknown scope":         func(f map[string]any) { firstConsumerTestCall(f)["scope"] = "missing" },
		"reversed span":         func(f map[string]any) { firstConsumerTestCall(f)["endSite"] = map[string]any{"line": 1, "column": 1} },
		"missing arguments":     func(f map[string]any) { delete(firstConsumerTestCall(f), "arguments") },
		"extra call field":      func(f map[string]any) { firstConsumerTestCall(f)["authorized"] = true },
		"unknown argument kind": func(f map[string]any) { firstConsumerTestCall(f)["callee"].(map[string]any)["kind"] = "trusted" },
		"oversized expression": func(f map[string]any) {
			firstConsumerTestCall(f)["callee"].(map[string]any)["text"] = strings.Repeat("x", 65537)
		},
		"duplicate keyword": func(f map[string]any) {
			c := firstConsumerTestCall(f)
			values := c["keywords"].([]any)
			c["keywords"] = append(values, values[0])
		},
		"argument outside call": func(f map[string]any) {
			firstConsumerTestCall(f)["callee"].(map[string]any)["site"] = map[string]any{"line": 1, "column": 1}
		},
		"same call in another scope": func(f map[string]any) {
			calls := f["calls"].([]any)
			duplicate := map[string]any{}
			for key, value := range calls[0].(map[string]any) {
				duplicate[key] = value
			}
			duplicate["scope"] = "module"
			f["calls"] = append(calls, duplicate)
		},
	} {
		t.Run(name, func(t *testing.T) {
			modules := typeTestModules(project.Sources)
			var facts map[string]any
			if err := json.Unmarshal(modules[0].Facts, &facts); err != nil {
				t.Fatal(err)
			}
			mutate(facts)
			modules[0].Facts, err = json.Marshal(facts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ResolveTypeProject(t.Context(), python, modules); err == nil {
				t.Fatal("malformed consumer evidence was accepted")
			}
		})
	}
}

func firstConsumerTestCall(facts map[string]any) map[string]any {
	return facts["calls"].([]any)[0].(map[string]any)
}

func decodeConsumerTestFacts(t *testing.T, source Source) consumerFactDocument {
	t.Helper()
	var facts consumerFactDocument
	if source.Error != "" {
		t.Fatal(source.Error)
	}
	if err := json.Unmarshal(source.TypeFacts, &facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

func consumerTestCall(t *testing.T, facts consumerFactDocument, callee string) consumerFactCall {
	t.Helper()
	calls := []consumerFactCall{}
	for _, call := range facts.Calls {
		if call.Callee.Kind == "name" && call.Callee.Value == callee {
			calls = append(calls, call)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("callee %s has %d facts", callee, len(calls))
	}
	return calls[0]
}
