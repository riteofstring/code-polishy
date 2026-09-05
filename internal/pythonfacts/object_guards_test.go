package pythonfacts

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const objectGuardPredicateSource = `import keyword
import unicodedata

def permitted(value):
    return (
        type(value) is str
        and unicodedata.normalize('NFKC', value) == value
        and value.count(':') == 1
        and value.startswith(('third_party.plugins:', 'third_party.plugins.'))
        and all(part.isidentifier() and not keyword.iskeyword(part)
                for part in value.replace(':', '.').split('.'))
    )
`

const objectGuardLoaderSource = "from pkgutil import resolve_name\nfrom validator import permitted\n\ndef load(name):\n    if not permitted(name):\n        raise ValueError('invalid plugin name')\n    return resolve_name(name)\n"

func TestObjectImportGuardBindsPredicateSourceAcrossReexports(t *testing.T) {
	sources := []Input{
		{Path: "app.py", Source: strings.Replace(objectGuardLoaderSource, "from validator", "from bridge", 1)},
		{Path: "bridge.py", Source: "from validator import permitted\n"},
		{Path: "validator.py", Source: objectGuardPredicateSource + "\nraise RuntimeError('target source must not execute')\n"},
	}
	modules, result := objectGuardTestImports(t, sources)
	if len(result.Imports) != 1 || result.Imports[0].Error != "" || result.Imports[0].Guard == nil {
		t.Fatalf("runtime input has no independent guard evidence: %+v", result)
	}
	guard := result.Imports[0].Guard
	if guard.Namespace != "third_party.plugins" || guard.Predicate.Path != "validator.py" || guard.Predicate.Name != "permitted" || guard.Site.Line != 5 || guard.StandardLibrary || len(result.Imports[0].Targets) != 0 {
		t.Fatalf("guard substituted an enumerated target or lost its source: %+v", result)
	}
	slices.Reverse(modules)
	reordered, err := ResolveObjectImports(t.Context(), typeTestInterpreter(t), ".", modules, nil)
	if err != nil || result.Identity != reordered.Identity {
		t.Fatalf("module order changed guard evidence: %+v, %v", reordered, err)
	}
	sources[2].Source += "\n"
	_, updated := objectGuardTestImports(t, sources)
	if updated.Imports[0].Guard == nil || updated.Imports[0].Guard.PredicateSHA256 == guard.PredicateSHA256 || updated.Identity == result.Identity {
		t.Fatalf("predicate change retained stale input evidence: %+v", updated)
	}
}

func TestObjectImportGuardRejectsWeakenedPredicatesAndShadowedOperations(t *testing.T) {
	for name, predicate := range map[string]string{
		"missing type":       strings.Replace(objectGuardPredicateSource, "type(value) is str", "True", 1),
		"wrong type":         strings.Replace(objectGuardPredicateSource, "is str", "is bytes", 1),
		"normalization":      strings.Replace(objectGuardPredicateSource, "'NFKC'", "'NFC'", 1),
		"repeated colon":     strings.Replace(objectGuardPredicateSource, "== 1", ">= 1", 1),
		"namespace prefix":   strings.Replace(objectGuardPredicateSource, "'third_party.plugins.'", "'third_party.plugins'", 1),
		"empty component":    strings.Replace(objectGuardPredicateSource, "part.isidentifier()", "part.isascii()", 1),
		"keyword":            strings.Replace(objectGuardPredicateSource, "not keyword.iskeyword(part)", "True", 1),
		"different value":    strings.Replace(objectGuardPredicateSource, "value.count(':')", "other.count(':')", 1),
		"ignored result":     strings.Replace(objectGuardPredicateSource, "    return (", "    print(", 1),
		"async predicate":    strings.Replace(objectGuardPredicateSource, "def permitted", "async def permitted", 1),
		"early return":       strings.Replace(objectGuardPredicateSource, "    return (", "    return True\n    return (", 1),
		"decorated":          strings.Replace(objectGuardPredicateSource, "def permitted", "@wrap\ndef permitted", 1),
		"shadowed type":      objectGuardPredicateSource + "type = lambda value: str\n",
		"shadowed all":       objectGuardPredicateSource + "all = lambda values: True\n",
		"shadowed keyword":   strings.Replace(objectGuardPredicateSource, "import keyword", "import custom as keyword", 1),
		"shadowed normalize": strings.Replace(objectGuardPredicateSource, "import unicodedata", "import custom as unicodedata", 1),
		"generator shadow":   strings.ReplaceAll(objectGuardPredicateSource, "part", "keyword"),
	} {
		t.Run(name, func(t *testing.T) {
			_, result := objectGuardTestImports(t, []Input{{Path: "app.py", Source: objectGuardLoaderSource}, {Path: "validator.py", Source: predicate}})
			assertObjectGuardRejected(t, result)
		})
	}
}

func TestObjectImportGuardRejectsMissingOrDisconnectedRejection(t *testing.T) {
	for name, loader := range map[string]string{
		"ignored boolean": strings.Replace(objectGuardLoaderSource, "    if not permitted(name):\n        raise ValueError('invalid plugin name')", "    permitted(name)", 1),
		"different value": strings.Replace(objectGuardLoaderSource, "permitted(name)", "permitted(other)", 1),
		"rebound input":   strings.Replace(objectGuardLoaderSource, "    return resolve_name", "    name = other\n    return resolve_name", 1),
		"nonlocal write":  strings.Replace(objectGuardLoaderSource, "    return resolve_name", "    def mutate():\n        nonlocal name\n        name = other\n    return resolve_name", 1),
		"after loading":   strings.Replace(objectGuardLoaderSource, "    if not", "    plugin = resolve_name(name)\n    if not", 1),
		"conditional":     strings.Replace(objectGuardLoaderSource, "    if not permitted(name):\n        raise ValueError('invalid plugin name')", "    if enabled:\n        if not permitted(name):\n            raise ValueError", 1),
		"caught failure":  strings.Replace(objectGuardLoaderSource, "    if not permitted(name):\n        raise ValueError('invalid plugin name')", "    try:\n        if not permitted(name):\n            raise ValueError\n    except ValueError:\n        pass", 1),
		"suppressed":      strings.Replace(objectGuardLoaderSource, "    if not permitted(name):\n        raise ValueError('invalid plugin name')", "    with suppress(ValueError):\n        if not permitted(name):\n            raise ValueError", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, result := objectGuardTestImports(t, []Input{{Path: "app.py", Source: loader}, {Path: "validator.py", Source: objectGuardPredicateSource}})
			if !slices.ContainsFunc(result.Imports, func(value ObjectImport) bool { return value.Error != "" && value.Guard == nil }) {
				t.Fatalf("disconnected guard admitted every input: %+v", result)
			}
		})
	}
}

func TestObjectImportGuardOutputCannotSubstituteItsProof(t *testing.T) {
	modules, result := objectGuardTestImports(t, []Input{{Path: "app.py", Source: objectGuardLoaderSource}, {Path: "validator.py", Source: objectGuardPredicateSource}})
	if len(result.Imports) != 1 || result.Imports[0].Guard == nil {
		t.Fatalf("fixture has no guard: %+v", result)
	}
	for name, change := range map[string]func(*ObjectInputGuard){
		"site":      func(value *ObjectInputGuard) { value.Site.Line++ },
		"span":      func(value *ObjectInputGuard) { value.EndSite.Column++ },
		"namespace": func(value *ObjectInputGuard) { value.Namespace = "other.plugins" },
		"path":      func(value *ObjectInputGuard) { value.Predicate.Path = "app.py" },
		"name":      func(value *ObjectInputGuard) { value.Predicate.Name = "other" },
		"digest":    func(value *ObjectInputGuard) { value.PredicateSHA256 = strings.Repeat("a", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			imported := result.Imports[0]
			guard := *imported.Guard
			change(&guard)
			imported.Guard = &guard
			if _, err := decodeObjectImportProject(objectImportTestWire(t, modules, []ObjectImport{imported}), modules, nil); err == nil {
				t.Fatal("substituted guard acquired input evidence")
			}
		})
	}
}

func TestObjectImportGuardResolvesAliasedOperationsAcrossSourcePartitions(t *testing.T) {
	predicate := strings.Replace(objectGuardPredicateSource, "import keyword\nimport unicodedata", "from keyword import iskeyword as reserved\nfrom unicodedata import normalize as normalized\nfrom builtins import type as kind, str as text, all as every", 1)
	for _, change := range [][2]string{{"type(value)", "kind(value)"}, {"is str", "is text"}, {"unicodedata.normalize", "normalized"}, {"all(", "every("}, {"keyword.iskeyword", "reserved"}} {
		predicate = strings.ReplaceAll(predicate, change[0], change[1])
	}
	inputs := []Input{
		{Path: "app.py", Source: objectGuardLoaderSource},
		{Path: "validator.py", Source: predicate},
	}
	python := typeTestInterpreter(t)
	limit := projectPartitionTestLimit(t, inputs[1])
	partitioned, err := analyzeProjectSources(t.Context(), python, inputs, limit, analyze)
	if err != nil || len(partitioned.Partitions) != 2 {
		t.Fatalf("predicate fixture was not partitioned: %+v, %v", partitioned.Partitions, err)
	}
	result, err := ResolveObjectImports(t.Context(), python, ".", typeTestModules(partitioned.Sources), nil)
	if err != nil || len(result.Imports) != 1 || result.Imports[0].Guard == nil || result.Imports[0].Error != "" {
		t.Fatalf("partitioned operation aliases lost their guard: %+v, %v", result, err)
	}
}

func TestAdmittedObjectPredicateRejectsInvalidValuesBeforeTheLoader(t *testing.T) {
	cases := []struct {
		value any
		valid bool
	}{
		{"third_party.plugins:Plugin", true}, {"third_party.plugins.extra:Factory.Plugin", true},
		{"third_party.plugins.λ:Δ", true}, {"third_party.plugins:False", false},
		{"third_party.plugins.class:Plugin", false}, {"third_party.plugins:Plugin()", false},
		{"third_party.plugins:Plugin:Other", false}, {"third_party.plugins..extra:Plugin", false},
		{".third_party.plugins:Plugin", false}, {"third_party.plugins:Plugin.", false},
		{"third_party.plugins:K", false}, {"third_party.plugins:*", false}, {"third_party.plugins:\nPlugin", false},
		{"third_party.plugins_other:Plugin", false}, {"other.plugins:Plugin", false},
		{"third_party.plugins:", false}, {"", false}, {nil, false}, {12, false},
	}
	values, expected := []any{}, []bool{}
	for _, test := range cases {
		values, expected = append(values, test.value), append(expected, test.valid)
	}
	input, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	loader := strings.Replace(objectGuardLoaderSource, "from pkgutil import resolve_name\nfrom validator import permitted\n", "", 1)
	program := objectGuardPredicateSource + loader + `
import json, sys
attempts = []
def resolve_name(value):
    attempts.append(value)
    return value
results = []
for value in json.load(sys.stdin):
    before = len(attempts)
    try:
        load(value)
    except ValueError:
        pass
    results.append(len(attempts) > before)
print(json.dumps(results))
`
	output, err := runFactProject(t.Context(), typeTestInterpreter(t), bytes.NewReader(input), program)
	if err != nil {
		t.Fatal(err)
	}
	var actual []bool
	if err := json.Unmarshal(output, &actual); err != nil || !slices.Equal(actual, expected) {
		t.Fatalf("predicate admitted malformed or foreign input: %s, %v", output, err)
	}
}

func objectGuardTestImports(t *testing.T, inputs []Input) ([]TypeModule, ObjectImportProject) {
	t.Helper()
	project, err := AnalyzeProjectSources(t.Context(), typeTestInterpreter(t), inputs)
	if err != nil {
		t.Fatal(err)
	}
	modules := typeTestModules(project.Sources)
	result, err := ResolveObjectImports(t.Context(), typeTestInterpreter(t), ".", modules, nil)
	if err != nil {
		t.Fatal(err)
	}
	return modules, result
}

func assertObjectGuardRejected(t *testing.T, result ObjectImportProject) {
	t.Helper()
	if len(result.Imports) != 1 || result.Imports[0].Error == "" || result.Imports[0].Guard != nil || len(result.Imports[0].Targets) != 0 {
		t.Fatalf("invalid predicate acquired input evidence: %+v", result)
	}
}
