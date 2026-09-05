package pythonfacts

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestTypedDictReadsResolveAcrossBoundedProjectPartitions(t *testing.T) {
	python := typeTestInterpreter(t)
	inputs := make([]Input, 1001)
	for index := range inputs {
		inputs[index] = Input{Path: fmt.Sprintf("src/project/module_%04d.py", index), Source: "# " + strings.Repeat("x", 8500) + "\n"}
	}
	inputs[0].Source += "from .module_0999 import Alias\ndef read(payload: Alias):\n    return payload[\"name\"]\n"
	inputs[999].Source += "from .module_1000 import Child as Alias\n"
	inputs[1000].Source += "from typing import TypedDict\nclass Base(TypedDict):\n    name: str\n    unused: str\nclass Child(Base):\n    child_only: int\n"
	project, err := AnalyzeProjectSources(t.Context(), python, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Partitions) < 2 || project.Partitions[0].LastPath >= inputs[999].Path {
		t.Fatalf("fixture does not cross the request boundary: %+v", project.Partitions)
	}
	modules := typeTestModules(project.Sources)
	first, err := ResolveTypeProject(t.Context(), python, modules)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Reads) != 1 || first.Reads[0].Path != inputs[0].Path || first.Reads[0].Field.Path != inputs[1000].Path ||
		first.Reads[0].Field.TypeName != "Base" || first.Reads[0].Field.Key != "name" || first.Reads[0].Field.Line != 4 {
		t.Fatalf("cross-partition inherited field = %+v", first.Reads)
	}
	slices.Reverse(inputs)
	secondProject, err := AnalyzeProjectSources(t.Context(), python, inputs)
	if err != nil {
		t.Fatal(err)
	}
	secondModules := typeTestModules(secondProject.Sources)
	slices.Reverse(secondModules)
	second, err := ResolveTypeProject(t.Context(), python, secondModules)
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity != second.Identity || !reflect.DeepEqual(first.Reads, second.Reads) || project.Identity != secondProject.Identity || !reflect.DeepEqual(project.Partitions, secondProject.Partitions) {
		t.Fatal("partition or source ordering changed resolved TypedDict evidence")
	}
}

func TestTypedDictReadsKeepOnlyExactDeclaredKeysAndReceiverTypes(t *testing.T) {
	source := `import typing as t
from typing_extensions import TypedDict as TD
class First(t.TypedDict):
    name: str
    unused: str
class Other(TD):
    name: str
Functional = TD("Functional", {"code": int, "unused": str})
Alias = First
def read(payload: Alias, unknown, union: First | Other, any_value: t.Any):
    alias = payload
    created = Functional(code=1)
    return (payload["name"], alias["name"], created["code"], First(name="n")["name"],
            unknown["unused"], union["name"], any_value["unused"], payload[key], payload.get("unused"))
`
	result, err := resolveTypeTestSources(t, map[string]string{"src/project/example.py": source})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Reads) != 4 {
		t.Fatalf("literal-key reads = %+v", result.Reads)
	}
	for _, read := range result.Reads {
		if read.Field.TypeName != "First" && read.Field.TypeName != "Functional" || read.Field.Key == "unused" {
			t.Fatalf("unrelated field acquired reachability: %+v", read)
		}
	}
}

func TestTypedDictReadsRejectInvalidDefinitionEvidence(t *testing.T) {
	cases := map[string]string{
		"duplicate class key":     "from typing import TypedDict\nclass Data(TypedDict):\n    key: str\n    key: int\n",
		"duplicate factory key":   "from typing import TypedDict\nData = TypedDict(\"Data\", {\"key\": str, \"key\": int})\n",
		"duplicate definition":    "from typing import TypedDict\nclass Data(TypedDict):\n    key: str\nclass Data(TypedDict):\n    another: str\n",
		"unsupported field type":  "from typing import TypedDict\nclass Data(TypedDict):\n    key: create_type()\n",
		"conditional definition":  "from typing import TypedDict\nif condition:\n    class Data(TypedDict):\n        key: str\n",
		"duplicate inherited key": "from typing import TypedDict\nclass Base(TypedDict):\n    key: str\nclass Data(Base):\n    key: str\n",
		"unknown base":            "from typing import TypedDict\nclass Data(TypedDict, Unknown):\n    key: str\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := resolveTypeTestSources(t, map[string]string{"src/project/example.py": source}); err == nil || !strings.Contains(err.Error(), "TypedDict") {
				t.Fatalf("invalid definition result = %v", err)
			}
		})
	}
}

func TestTypedDictReadsDoNotInferLookalikesWildcardsOrReboundReceivers(t *testing.T) {
	cases := map[string]string{
		"lookalike":               "class TypedDict: pass\nclass Data(TypedDict):\n    key: str\ndef read(payload: Data):\n    return payload[\"key\"]\n",
		"wildcard":                "from typing import *\nclass Data(TypedDict):\n    key: str\ndef read(payload: Data):\n    return payload[\"key\"]\n",
		"rebound":                 "from typing import TypedDict\nclass Data(TypedDict):\n    key: str\ndef read(payload: Data):\n    payload = other\n    return payload[\"key\"]\n",
		"variadic parameter":      "from typing import TypedDict\nclass Data(TypedDict):\n    key: str\ndef read(**payload: Data):\n    return payload[\"key\"]\n",
		"type object":             "from typing import TypedDict\nclass Data(TypedDict):\n    key: str\npayload = Data\nvalue = payload[\"key\"]\n",
		"comprehension rebinding": "from typing import TypedDict\nclass Data(TypedDict):\n    key: str\ndef read(payload: Data):\n    values = [(payload := other) for other in items]\n    return payload[\"key\"]\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := resolveTypeTestSources(t, map[string]string{"src/project/example.py": source})
			if err != nil || len(result.Reads) != 0 {
				t.Fatalf("unproven reads = %+v, %v", result, err)
			}
		})
	}
}

func TestTypedDictProjectRejectsMissingAndMalformedFacts(t *testing.T) {
	python := typeTestInterpreter(t)
	project, err := AnalyzeProjectSources(t.Context(), python, []Input{{Path: "src/project/data.py", Source: "value = 1\n"}})
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func([]TypeModule) []TypeModule{
		"missing facts": func(modules []TypeModule) []TypeModule { modules[0].Facts = nil; return modules },
		"missing bindings": func(modules []TypeModule) []TypeModule {
			modules[0].Facts = json.RawMessage(`{"scopes":[{"id":"module","parent":"","kind":"module"}],"reads":[]}`)
			return modules
		},
		"duplicate source": func(modules []TypeModule) []TypeModule { return append(modules, modules[0]) },
		"escaping path":    func(modules []TypeModule) []TypeModule { modules[0].Path = "../data.py"; return modules },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveTypeProject(t.Context(), python, change(typeTestModules(project.Sources))); err == nil {
				t.Fatal("invalid compact type facts were accepted")
			}
		})
	}
}

func resolveTypeTestSources(t *testing.T, sources map[string]string) (TypeProject, error) {
	t.Helper()
	python := typeTestInterpreter(t)
	inputs := make([]Input, 0, len(sources))
	for path, source := range sources {
		inputs = append(inputs, Input{Path: path, Source: source})
	}
	project, err := AnalyzeProjectSources(t.Context(), python, inputs)
	if err != nil {
		return TypeProject{}, err
	}
	return ResolveTypeProject(t.Context(), python, typeTestModules(project.Sources))
}

func typeTestModules(sources []Source) []TypeModule {
	modules := make([]TypeModule, 0, len(sources))
	for _, source := range sources {
		module := strings.TrimSuffix(strings.TrimPrefix(source.Path, "src/"), ".py")
		module = strings.ReplaceAll(module, "/", ".")
		module = strings.TrimSuffix(module, ".__init__")
		packageName := ""
		if strings.HasSuffix(source.Path, "/__init__.py") {
			packageName = module
		} else if index := strings.LastIndex(module, "."); index >= 0 {
			packageName = module[:index]
		}
		modules = append(modules, TypeModule{Path: source.Path, Module: module, Package: packageName, SourceSHA256: source.SHA256, Facts: source.TypeFacts})
	}
	return modules
}

func typeTestInterpreter(t *testing.T) string {
	t.Helper()
	python, err := DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	return python
}
