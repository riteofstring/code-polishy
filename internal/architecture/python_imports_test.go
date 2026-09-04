package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPythonSourceFactsUseASTBindingsAndFailClosed(t *testing.T) {
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	source := []byte("import importlib as loader\nfrom package import item\nloader.import_module(module_name)\n")
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Sources: []pythonfacts.Input{{Path: "source.py", Source: string(source)}}})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := pythonSourceFactFromAdapter(response.Sources[0], source)
	if err != nil {
		t.Fatal(err)
	}
	if len(fact.Imports) != 2 || fact.Imports[0].Module != "importlib" || fact.Imports[1].Module != "package" ||
		len(fact.ComputedImports) != 1 || fact.ComputedImports[0].Callee != "importlib.import_module" ||
		fact.ComputedImports[0].Callable != "<module>" || fact.ComputedImports[0].Line != 3 || fact.ComputedImports[0].Argument != "module_name" {
		t.Fatalf("fact = %+v", fact)
	}
	response.Sources[0].SHA256 = strings.Repeat("0", 64)
	if _, err := pythonSourceFactFromAdapter(response.Sources[0], source); err == nil {
		t.Fatal("mismatched source digest was accepted")
	}
}

func TestPythonComputedImportCoverageRequiresCurrentExactEvidence(t *testing.T) {
	t.Parallel()
	source := []byte("import importlib\nimportlib.import_module((\"app.plugins.first\",)[index])\n")
	fact := pythonComputedTestFact(t, "src/app/loader.py", source)
	project := repository.PythonProject{
		Manifest: "pyproject.toml", Root: ".", SourceRoots: []string{".", "src"},
		Files: []string{"src/app/loader.py", "src/app/plugins/first.py"},
		PackageCandidates: []repository.PythonPackageCandidate{
			{Name: "app", Path: "src/app"},
			{Name: "app.plugins", Path: "src/app/plugins"},
		},
	}
	computed := fact.ComputedImports[0]
	declaration := policy.PythonComputedImport{
		Project: "pyproject.toml", Importer: "src/app/loader.py", Module: "app.loader", ModuleScope: true,
		Callee: computed.Callee, Line: computed.Line, Column: computed.Column, Shape: computed.Shape,
		Argument: computed.Argument, SourceSHA256: fact.SHA256, Namespace: "app.plugins", Targets: []string{"app.plugins.first"},
	}
	repo := repository.Repository{Root: t.TempDir(), Config: policy.Config{Scope: policy.Scope{PythonComputedImports: []policy.PythonComputedImport{declaration}}}}
	dependencies := map[string]bool{}
	if message := pythonComputedImportSourceCoverage(repo, project, newPythonModuleIndex(project), "src/app/loader.py", dependencies, fact, []policy.PythonComputedImport{declaration}); message != "" {
		t.Fatalf("message = %q", message)
	}
	if !dependencies["src/app/plugins/first.py"] {
		t.Fatalf("dependencies = %+v", dependencies)
	}
	stale := declaration
	stale.SourceSHA256 = strings.Repeat("0", 64)
	if message := pythonComputedImportSourceCoverage(repo, project, newPythonModuleIndex(project), "src/app/loader.py", map[string]bool{}, fact, []policy.PythonComputedImport{stale}); !strings.Contains(message, "stale") {
		t.Fatalf("message = %q", message)
	}
	if message := pythonComputedImportSourceCoverage(repo, project, newPythonModuleIndex(project), "src/app/loader.py", map[string]bool{}, fact, nil); !strings.Contains(message, "no exact") {
		t.Fatalf("message = %q", message)
	}
	if message := pythonComputedImportSourceCoverage(repo, project, newPythonModuleIndex(project), "src/app/loader.py", map[string]bool{}, pythonSourceFact{SHA256: fact.SHA256}, []policy.PythonComputedImport{declaration}); !strings.Contains(message, "stale") {
		t.Fatalf("message = %q", message)
	}
}

func TestPythonComputedImportConfigurationIsDigestBoundAndNamespaced(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	data := []byte(`{"enabled":["app.plugins.first","app.plugins.second"]}`)
	writeArchitectureFile(t, root, "plugins.json", string(data))
	digest := sha256.Sum256(data)
	repo := repository.Repository{Root: root}
	input := policy.PythonComputedImportInput{Path: "plugins.json", JSONPointer: "/enabled", SHA256: hex.EncodeToString(digest[:])}
	targets, message := pythonComputedTargets(repo, repository.PythonProject{}, policy.PythonComputedImport{Namespace: "app.plugins", Configuration: []policy.PythonComputedImportInput{input}})
	if message != "" || strings.Join(targets, ",") != "app.plugins.first,app.plugins.second" {
		t.Fatalf("targets = %+v, message = %q", targets, message)
	}
	input.SHA256 = strings.Repeat("0", 64)
	if _, message := pythonComputedTargets(repo, repository.PythonProject{}, policy.PythonComputedImport{Namespace: "app.plugins", Configuration: []policy.PythonComputedImportInput{input}}); !strings.Contains(message, "changed") {
		t.Fatalf("message = %q", message)
	}
	escapeData := []byte(`{"enabled":"outside.module"}`)
	writeArchitectureFile(t, root, "escape.json", string(escapeData))
	escapeDigest := sha256.Sum256(escapeData)
	input = policy.PythonComputedImportInput{Path: "escape.json", JSONPointer: "/enabled", SHA256: hex.EncodeToString(escapeDigest[:])}
	if _, message := pythonComputedTargets(repo, repository.PythonProject{}, policy.PythonComputedImport{Namespace: "app.plugins", Configuration: []policy.PythonComputedImportInput{input}}); !strings.Contains(message, "escapes namespace") {
		t.Fatalf("message = %q", message)
	}
}

func pythonComputedTestFact(t *testing.T, path string, source []byte) pythonSourceFact {
	t.Helper()
	python, err := pythonfacts.DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	response, err := pythonfacts.Analyze(python, pythonfacts.Request{Sources: []pythonfacts.Input{{Path: path, Source: string(source)}}})
	if err != nil {
		t.Fatal(err)
	}
	fact, err := pythonSourceFactFromAdapter(response.Sources[0], source)
	if err != nil {
		t.Fatal(err)
	}
	return fact
}

func TestPythonRelativeImportResolutionStaysWithinTheProject(t *testing.T) {
	t.Parallel()
	index := pythonModuleIndex{packageOf: map[string]string{
		"root.py":                             "",
		"src/nested/consumer.py":              "nested",
		"src/nested/deep/consumer.py":         "nested.deep",
		"src/nested/deep/package/__init__.py": "nested.deep.package",
	}}
	cases := []struct {
		name   string
		source string
		module string
		want   string
		valid  bool
	}{
		{name: "absolute import", source: "src/nested/consumer.py", module: "outside", want: "outside", valid: true},
		{name: "same package", source: "src/nested/consumer.py", module: ".sibling", want: "nested.sibling", valid: true},
		{name: "parent package", source: "src/nested/deep/consumer.py", module: "..sibling", want: "nested.sibling", valid: true},
		{name: "project root", source: "src/nested/deep/consumer.py", module: "...sibling", want: "sibling", valid: true},
		{name: "root package", source: "root.py", module: ".", want: "", valid: true},
		{name: "escaping root", source: "root.py", module: "..", valid: false},
		{name: "escaping nested package", source: "src/nested/deep/consumer.py", module: "....sibling", valid: false},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			module, valid := pythonResolvedImportModule(index, testCase.source, testCase.module)
			if module != testCase.want || valid != testCase.valid {
				t.Fatalf("pythonResolvedImportModule(%q, %q) = (%q, %t), want (%q, %t)", testCase.source, testCase.module, module, valid, testCase.want, testCase.valid)
			}
		})
	}
}

func TestPythonImportCoverageRequiresExactLocalGraphEvidence(t *testing.T) {
	t.Parallel()
	index := newPythonModuleIndex(repository.PythonProject{
		Root: ".", SourceRoots: []string{".", "src"},
		Files: []string{"shared.py", "src/shared.py", "src/domain/__init__.py", "src/domain/model.py", "src/domain/model.pyi", "src/namespace/value.py", "src/nested/consumer.py", "src/web/consumer.py", "src/web/helper.py"},
	})
	cases := []struct {
		name         string
		source       string
		reference    pythonImportReference
		dependencies map[string]bool
		want         string
	}{
		{name: "relative import escapes", source: "src/web/consumer.py", reference: pythonImportReference{Module: "...domain", Line: 4, Literal: true}, want: "line 4 relative Python import escapes its project"},
		{name: "third party import", source: "src/web/consumer.py", reference: pythonImportReference{Module: "requests", Line: 5, Literal: true}},
		{name: "local import lacks evidence", source: "src/web/consumer.py", reference: pythonImportReference{Module: "domain.model", Line: 6, Literal: true}, want: "line 6 Ruff did not resolve local-looking Python import \"domain.model\""},
		{name: "local import exact evidence", source: "src/web/consumer.py", reference: pythonImportReference{Module: "domain.model", Line: 7, Literal: true}, dependencies: map[string]bool{"src/domain/model.py": true}},
		{name: "named import unresolved", source: "src/web/consumer.py", reference: pythonImportReference{Module: "namespace", Names: []string{"missing"}, Line: 8, Literal: true}, want: "line 8 local-looking Python import \"namespace.missing\" cannot be resolved"},
		{name: "relative exact edge", source: "src/web/consumer.py", reference: pythonImportReference{Module: ".helper", Line: 11, Literal: true}, dependencies: map[string]bool{"src/web/helper.py": true}},
		{name: "conflicting roots", source: "src/web/consumer.py", reference: pythonImportReference{Module: "shared", Line: 13, Literal: true}, want: "line 13 local-looking Python import \"shared\" resolves through conflicting source roots"},
		{name: "namespace package", source: "src/web/consumer.py", reference: pythonImportReference{Module: "namespace", Line: 16, Literal: true}},
		{name: "stub shares identity", source: "src/web/consumer.py", reference: pythonImportReference{Module: "domain.model", Line: 19, Literal: true}, dependencies: map[string]bool{"src/domain/model.pyi": true}},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if message := pythonUnprovenImport(index, testCase.source, testCase.reference, testCase.dependencies); message != testCase.want {
				t.Fatalf("pythonUnprovenImport() = %q, want %q", message, testCase.want)
			}
		})
	}
}
