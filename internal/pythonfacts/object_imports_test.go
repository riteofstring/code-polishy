package pythonfacts

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestObjectImportOutputCannotSubstituteSourceCalls(t *testing.T) {
	modules, _ := reachabilityTestInput(t, false)
	result, err := ResolveObjectImports(t.Context(), typeTestInterpreter(t), ".", modules, nil)
	if err != nil || len(result.Imports) != 1 || result.Imports[0].Error != "" {
		t.Fatalf("original object import: %+v, %v", result, err)
	}
	for name, mutate := range map[string]func(*ObjectImport){
		"moved call":         func(value *ObjectImport) { value.Site.Line++ },
		"changed span":       func(value *ObjectImport) { value.EndSite.Column++ },
		"changed argument":   func(value *ObjectImport) { value.Argument = "'pkg.api:other'" },
		"substituted symbol": func(value *ObjectImport) { value.Targets[0].Symbol = "other" },
		"substituted module": func(value *ObjectImport) { value.Targets[0].Module = "unrelated" },
	} {
		t.Run(name, func(t *testing.T) {
			imported := result.Imports[0]
			imported.Targets = slices.Clone(imported.Targets)
			mutate(&imported)
			encoded := objectImportTestWire(t, modules, []ObjectImport{imported})
			if _, err := decodeObjectImportProject(encoded, modules, nil); err == nil {
				t.Fatal("altered object import acquired source evidence")
			}
		})
	}
}

func TestObjectImportOutputCannotDropOrSubstituteRegistryEvidence(t *testing.T) {
	modules, reference := reachabilityTestInput(t, true)
	inputs := []ObjectRegistryInput{{Path: "src/registry.json", Content: reference.Registry}}
	result, err := ResolveObjectImports(t.Context(), typeTestInterpreter(t), ".", modules, inputs)
	if err != nil || len(result.Imports) != 1 || result.Imports[0].Error != "" {
		t.Fatalf("original registry import: %+v, %v", result, err)
	}
	encoded := objectImportTestWire(t, modules, result.Imports)
	inputs[0].Content += "\n"
	if _, err := decodeObjectImportProject(encoded, modules, inputs); err == nil {
		t.Fatal("registry input changed without invalidating object import evidence")
	}
	inputs[0].Content = reference.Registry
	result.Imports[0].Registry = nil
	encoded = objectImportTestWire(t, modules, result.Imports)
	if _, err := decodeObjectImportProject(encoded, modules, inputs); err == nil {
		t.Fatal("computed loader masqueraded as a literal import")
	}
}

func objectImportTestWire(t *testing.T, modules []TypeModule, imports []ObjectImport) []byte {
	t.Helper()
	covered := []typeCoverage{}
	for _, module := range modules {
		covered = append(covered, typeCoverage{Path: module.Path, SourceSHA256: module.SourceSHA256})
	}
	encoded, err := json.Marshal(map[string]any{"protocol": "python-object-import-project/v1", "covered": covered, "imports": imports})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestObjectImportFactsResolveLoaderBindingsAcrossModules(t *testing.T) {
	python := typeTestInterpreter(t)
	for name, source := range map[string]string{
		"module alias":      "import pkgutil as utilities\ndef load():\n    return utilities.resolve_name('plugin:Handler')\n",
		"local alias":       "from pkgutil import resolve_name\nload_object = resolve_name\ndef load():\n    return load_object('plugin:Handler')\n",
		"relative reexport": "from pkg.bridge import load_object\ndef load():\n    return load_object('plugin:Handler')\n",
	} {
		t.Run(name, func(t *testing.T) {
			project, err := AnalyzeProjectSources(t.Context(), python, []Input{
				{Path: "src/app.py", Source: source},
				{Path: "src/pkg/bridge.py", Source: "from .implementation import loader as load_object\n"},
				{Path: "src/pkg/implementation.py", Source: "from pkgutil import resolve_name as loader\n"},
				{Path: "src/plugin.py", Source: "raise RuntimeError('must not execute target')\nclass Handler:\n    pass\n"},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := ResolveObjectImports(t.Context(), python, ".", typeTestModules(project.Sources), nil)
			if err != nil || len(result.Imports) != 1 || result.Imports[0].Error != "" || !slices.Equal(result.Imports[0].Targets, []ObjectImportTarget{{Module: "plugin", Symbol: "Handler"}}) {
				t.Fatalf("object loader lost its exact target: %+v, %v", result, err)
			}
		})
	}
}

func TestObjectImportFactsRejectAmbiguousAndMalformedLoads(t *testing.T) {
	python := typeTestInterpreter(t)
	for name, source := range map[string]string{
		"wildcard":          "from pkgutil import *\ndef load():\n    return resolve_name('plugin:Handler')\n",
		"conditional":       "if enabled:\n    from pkgutil import resolve_name\ndef load():\n    return resolve_name('plugin:Handler')\n",
		"rebound":           "from pkgutil import resolve_name\nresolve_name = other\ndef load():\n    return resolve_name('plugin:Handler')\n",
		"unknown input":     "from pkgutil import resolve_name\ndef load(name):\n    return resolve_name(name)\n",
		"relative module":   "from pkgutil import resolve_name\ndef load():\n    return resolve_name('.plugin:Handler')\n",
		"import expression": "from pkgutil import resolve_name\ndef load():\n    return resolve_name('plugin:Handler()')\n",
		"extra argument":    "from pkgutil import resolve_name\ndef load():\n    return resolve_name('plugin:Handler', other)\n",
	} {
		t.Run(name, func(t *testing.T) {
			project, err := AnalyzeProjectSources(t.Context(), python, []Input{{Path: "src/app.py", Source: source}})
			if err != nil {
				t.Fatal(err)
			}
			result, err := ResolveObjectImports(t.Context(), python, ".", typeTestModules(project.Sources), nil)
			if err != nil || len(result.Imports) != 1 || result.Imports[0].Error == "" || len(result.Imports[0].Targets) != 0 {
				t.Fatalf("unproven object import acquired targets: %+v, %v", result, err)
			}
		})
	}
}

func TestObjectImportFactsIgnoreLocallyShadowedLoaders(t *testing.T) {
	python := typeTestInterpreter(t)
	project, err := AnalyzeProjectSources(t.Context(), python, []Input{{Path: "src/app.py", Source: "from pkgutil import resolve_name\ndef load(resolve_name):\n    return resolve_name('plugin:Handler')\n"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := ResolveObjectImports(t.Context(), python, ".", typeTestModules(project.Sources), nil)
	if err != nil || len(result.Imports) != 0 {
		t.Fatalf("local parameter was treated as a dynamic import: %+v, %v", result, err)
	}
}

func TestObjectImportRegistryEvidenceBindsNestedProjectAndCurrentBytes(t *testing.T) {
	modules, reference := reachabilityTestInput(t, true)
	for index := range modules {
		modules[index].Path = "apps/api/" + modules[index].Path
	}
	inputs := []ObjectRegistryInput{{Path: "apps/api/src/registry.json", Content: reference.Registry}}
	python := typeTestInterpreter(t)
	first, err := ResolveObjectImports(t.Context(), python, "apps/api", modules, inputs)
	if err != nil || len(first.Imports) != 1 || first.Imports[0].Error != "" || first.Imports[0].Registry == nil || first.Imports[0].Registry.Path != inputs[0].Path {
		t.Fatalf("nested registry input was lost: %+v, %v", first, err)
	}
	inputs[0].Content += "\n"
	second, err := ResolveObjectImports(t.Context(), python, "apps/api", modules, inputs)
	if err != nil || first.Identity == second.Identity || first.Imports[0].Registry.SHA256 == second.Imports[0].Registry.SHA256 {
		t.Fatalf("changed registry bytes retained old evidence: %+v, %v", second, err)
	}
	for name, content := range map[string]string{
		"empty": `{"plugins":{}}`, "duplicate": `{"plugins":{"first":"pkg.api:run","first":"pkg.api:other"}}`,
		"unbounded": strings.Repeat("x", (2<<20)+1), "missing": "", "malformed": `{"plugins":["pkg.api:*"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			inputs[0].Content = content
			result, err := ResolveObjectImports(t.Context(), python, "apps/api", modules, inputs)
			if err != nil || len(result.Imports) != 1 || result.Imports[0].Error == "" || len(result.Imports[0].Targets) != 0 {
				t.Fatalf("invalid registry acquired import evidence: %+v, %v", result, err)
			}
		})
	}
}
