package pythonfacts

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestModuleImportsResolveReexportedLoadersAndSupportingCalls(t *testing.T) {
	sources := map[string]string{
		"src/exports.py": "from importlib import import_module as load\nfrom json import loads as decode\nfrom pathlib import Path as Document\nfrom importlib.metadata import entry_points as entries\nfrom builtins import next as first\nfrom typing import TYPE_CHECKING as CHECK\nlegacy = __import__\n",
		"src/bridge.py":  "from exports import load, decode, Document, entries, first, CHECK, legacy\n",
		"src/app.py": `from bridge import load, decode, Document, entries, first, CHECK, legacy
load(("app.plugins.first", "app.plugins.second")[choice])
legacy("app.plugins.direct")
load(decode(Document("plugins.json").read_text(encoding="utf-8"))["enabled"][choice])
load(first(entries(group="app.plugins")).module)
if CHECK:
    load("app.plugins.types")
    load("app.plugins.first" if choice else "app.plugins.second")
`,
	}
	result, err := resolveTypeTestSources(t, sources)
	if err != nil || len(result.Imports) != 6 {
		t.Fatalf("cross-file imports = %+v, error = %v", result.Imports, err)
	}
	for _, imported := range result.Imports {
		if imported.Path != "src/app.py" || imported.EvidenceError != "" || imported.Shape == "" {
			t.Fatalf("cross-file source evidence = %+v", imported)
		}
	}
	if !slices.Equal(result.Imports[0].Targets, []string{"app.plugins.first", "app.plugins.second"}) || !result.Imports[1].Literal || result.Imports[1].Callee != "builtins.__import__" {
		t.Fatalf("bounded and literal targets = %+v", result.Imports[:2])
	}
	if !reflect.DeepEqual(result.Imports[2].Configuration, []ComputedConfiguration{{Path: "plugins.json", JSONPointer: "/enabled"}}) || result.Imports[3].EntryPointGroup != "app.plugins" {
		t.Fatalf("cross-file configuration and entry-point proof = %+v", result.Imports[2:4])
	}
	if result.Imports[4].Kind != "type-only" || result.Imports[5].Kind != "type-only" || !result.Imports[4].Literal || result.Imports[5].Literal {
		t.Fatalf("type-only imports = %+v", result.Imports[4:])
	}
}

func TestModuleImportsKeepConditionalAndShadowedLoaderEvidenceExact(t *testing.T) {
	for name, source := range map[string]string{
		"parameter":         "from exports import load\ndef run(load):\n    return load(name)\n",
		"local":             "from exports import load\ndef run():\n    load = lambda name: name\n    return load(name)\n",
		"module":            "from exports import load\nload = lambda name: name\nload(name)\n",
		"loop":              "from exports import load\nfor load in functions:\n    load(name)\n",
		"comprehension":     "from exports import load\nvalues = [load(name) for load in functions]\n",
		"builtin parameter": "def run(__import__):\n    return __import__(name)\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := resolveTypeTestSources(t, map[string]string{"src/exports.py": "from importlib import import_module as load\n", "src/app.py": source})
			if err != nil || len(result.Imports) != 0 {
				t.Fatalf("shadowed loader = %+v, error = %v", result.Imports, err)
			}
		})
	}
	for name, source := range map[string]string{
		"wildcard":        "from exports import *\nload('app.plugins.first')\n",
		"conditional":     "from exports import load\nif condition:\n    load = other\nload('app.plugins.first')\n",
		"global mutation": "from exports import load\ndef change():\n    global load\n    load = other\nload('app.plugins.first')\n",
		"extra argument":  "from exports import load\nload('app.plugins.first', 'app')\n",
		"keyword":         "from exports import load\nload(name='app.plugins.first')\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := resolveTypeTestSources(t, map[string]string{"src/exports.py": "from importlib import import_module as load\n", "src/app.py": source})
			if err != nil || len(result.Imports) != 1 || result.Imports[0].EvidenceError == "" || result.Imports[0].Literal || len(result.Imports[0].Targets) != 0 {
				t.Fatalf("unproven loader was lost or admitted: %+v, error = %v", result.Imports, err)
			}
		})
	}
}

func TestModuleImportsRequireExactConfigurationAndEntryPointBindings(t *testing.T) {
	for name, source := range map[string]string{
		"JSON alias rebound":       "decode = other\nload(decode(Document('plugins.json').read_text())['enabled'][choice])\n",
		"path alias rebound":       "Document = other\nload(decode(Document('plugins.json').read_text())['enabled'][choice])\n",
		"entrypoint alias rebound": "entries = other\nload(entries(group='app.plugins')[choice].module)\n",
		"next rebound":             "next = other\nload(next(entries(group='app.plugins')).module)\n",
		"encoding":                 "load(decode(Document('plugins.json').read_text(encoding='latin-1'))['enabled'][choice])\n",
		"dynamic prefix":           "load(decode(Document('plugins.json').read_text())[choice]['enabled'])\n",
		"unbounded collection":     "load(('app.plugins.first', unknown)[choice])\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := resolveTypeTestSources(t, map[string]string{
				"src/exports.py": "from importlib import import_module as load\nfrom json import loads as decode\nfrom pathlib import Path as Document\nfrom importlib.metadata import entry_points as entries\n",
				"src/app.py":     "from exports import load, decode, Document, entries\n" + source,
			})
			if err != nil || len(result.Imports) != 1 || result.Imports[0].EvidenceError == "" {
				t.Fatalf("unproven bounded input = %+v, error = %v", result.Imports, err)
			}
		})
	}
}

func TestModuleImportsResolveBindingsWhenDefinitionsAndLoopInputsAreEvaluated(t *testing.T) {
	for name, source := range map[string]string{
		"function default": "def load(value=load('app.plugins.first')):\n    return value\n",
		"class base":       "class load(load('app.plugins.first')):\n    pass\n",
		"class body":       "class load:\n    value = load('app.plugins.first')\n",
		"loop input":       "for load in load('app.plugins.first'):\n    load(other)\n",
		"assignment input": "load = load('app.plugins.first')\n",
		"multiple targets": "load = other = load('app.plugins.first')\n",
		"named expression": "value = (load := load('app.plugins.first'))\n",
		"augmented input":  "load += load('app.plugins.first')\n",
		"annotation input": "load: load('app.plugins.first')\n",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := resolveTypeTestSources(t, map[string]string{"src/app.py": "from importlib import import_module as load\n" + source})
			if err != nil || len(result.Imports) != 1 || !result.Imports[0].Literal || !slices.Equal(result.Imports[0].Targets, []string{"app.plugins.first"}) {
				t.Fatalf("definition shadowed its own input: %+v, error = %v", result.Imports, err)
			}
		})
	}
}

func TestModuleImportsRepeatedImportsRetainTheSameLoaderBinding(t *testing.T) {
	result, err := resolveTypeTestSources(t, map[string]string{"src/app.py": "import importlib\nimport importlib.metadata\ndef load():\n    return importlib.import_module(importlib.metadata.entry_points(group='app.plugins')[choice].module)\n"})
	if err != nil || len(result.Imports) != 1 || result.Imports[0].EvidenceError != "" || result.Imports[0].EntryPointGroup != "app.plugins" {
		t.Fatalf("repeated imports lost the same loader identity: %+v, error = %v", result.Imports, err)
	}
}

func TestModuleImportEvidenceCannotSubstituteCurrentSourceCalls(t *testing.T) {
	python := typeTestInterpreter(t)
	project, err := AnalyzeProjectSources(t.Context(), python, []Input{{Path: "src/app.py", Source: "import importlib\nimportlib.import_module('app.plugins.first')\n"}})
	if err != nil {
		t.Fatal(err)
	}
	modules := typeTestModules(project.Sources)
	resolved, err := ResolveTypeProject(t.Context(), python, modules)
	if err != nil || len(resolved.Imports) != 1 {
		t.Fatalf("literal baseline = %+v, error = %v", resolved.Imports, err)
	}
	for name, change := range map[string]func(*typeProjectResponse){
		"missing collection": func(value *typeProjectResponse) { value.Imports = nil },
		"duplicate":          func(value *typeProjectResponse) { value.Imports = append(value.Imports, value.Imports[0]) },
		"source":             func(value *typeProjectResponse) { value.Imports[0].Path = "src/other.py" },
		"span":               func(value *typeProjectResponse) { value.Imports[0].EndColumn++ },
		"shape":              func(value *typeProjectResponse) { value.Imports[0].Shape = "Call()" },
		"argument":           func(value *typeProjectResponse) { value.Imports[0].Argument = "'app.plugins.other'" },
		"target":             func(value *typeProjectResponse) { value.Imports[0].Targets = []string{"app.plugins.other"} },
		"kind":               func(value *typeProjectResponse) { value.Imports[0].Kind = "runtime" },
	} {
		t.Run(name, func(t *testing.T) {
			response := typeProjectResponse{Protocol: typeProjectProtocol, Covered: []typeCoverage{{Path: modules[0].Path, SourceSHA256: modules[0].SourceSHA256}}, Reads: []TypedDictRead{}, Pydantic: []ReachabilityDefinition{}, Imports: slices.Clone(resolved.Imports)}
			change(&response)
			encoded, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeTypeProject(encoded, modules); err == nil {
				t.Fatal("substituted module-import evidence was accepted")
			}
		})
	}
}

func TestModuleImportLimitsDoNotRejectUnrelatedLargeCalls(t *testing.T) {
	result, err := resolveTypeTestSources(t, map[string]string{"src/app.py": "print('" + strings.Repeat("x", 20000) + "')\n"})
	if err != nil || len(result.Imports) != 0 {
		t.Fatalf("unrelated call inherited module-import bounds: %+v, error = %v", result.Imports, err)
	}
	_, err = resolveTypeTestSources(t, map[string]string{"src/app.py": "import importlib\nimportlib.import_module('" + strings.Repeat("x", 20000) + "')\n"})
	if err == nil || !strings.Contains(err.Error(), "bounded call evidence") {
		t.Fatalf("oversized module import error = %v", err)
	}
}
