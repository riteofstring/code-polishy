package pythonfacts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTypedDictFieldsRejectInvalidDefinitionEvidence(t *testing.T) {
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

func TestTypedDictFieldsDoNotInferLookalikesOrWildcards(t *testing.T) {
	cases := map[string]string{
		"lookalike": "class TypedDict: pass\nclass Data(TypedDict):\n    key: str\n",
		"wildcard":  "from typing import *\nclass Data(TypedDict):\n    key: str\n",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			fields, err := resolveTypeTestSources(t, map[string]string{"src/project/example.py": source})
			if err != nil || len(fields) != 0 {
				t.Fatalf("unproven fields = %+v, %v", fields, err)
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
			modules[0].Facts = json.RawMessage(`{"scopes":[{"id":"module","parent":"","kind":"module"}],"calls":[]}`)
			return modules
		},
		"duplicate source": func(modules []TypeModule) []TypeModule { return append(modules, modules[0]) },
		"escaping path":    func(modules []TypeModule) []TypeModule { modules[0].Path = "../data.py"; return modules },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := resolveTypedDictFieldsForTest(t, change(typeTestModules(project.Sources))); err == nil {
				t.Fatal("invalid compact type facts were accepted")
			}
		})
	}
}

func resolveTypeTestSources(t *testing.T, sources map[string]string) ([]typedDictTestField, error) {
	t.Helper()
	python := typeTestInterpreter(t)
	inputs := make([]Input, 0, len(sources))
	for path, source := range sources {
		inputs = append(inputs, Input{Path: path, Source: source})
	}
	project, err := AnalyzeProjectSources(t.Context(), python, inputs)
	if err != nil {
		return nil, err
	}
	return resolveTypedDictFieldsForTest(t, typeTestModules(project.Sources))
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
