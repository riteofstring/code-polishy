package pythonfacts

import (
	"slices"
	"strings"
	"testing"
)

func TestAdapterProducesVersionedManifestRequirementAndSourceFacts(t *testing.T) {
	python, err := DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	response, err := Analyze(python, Request{
		Manifests: []Input{{Path: "pyproject.toml", Source: `[project]
requires-python = "==3.12.*"
dependencies = [
  "Requests[socks] >= 2.31, < 3 ; python_version >= '3.10'",
]
`}},
		Locks:        []Input{{Path: "uv.lock", Source: "version = 1\nrevision = 3\nrequires-python = \"==3.12.*\"\n[[package]]\nname = \"Example_Name\"\nversion = \"1.2\"\nsource = { registry = \"https://pypi.org/simple\" }\n"}},
		Metadata:     []Input{{Path: "example.dist-info/METADATA", Source: "Metadata-Version: 2.4\nName: Example_Name\nVersion: 1.2\nRequires-Dist: Requests>=2; python_version >= '3.12'\n\n"}},
		Sources:      []Input{{Path: "src/app.py", Source: "import importlib as loader\nloader.import_module(module_name)\n"}},
		Requirements: []string{"Example_Name==1.2"},
		Names:        []string{"Example_Name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertAdapterIdentity(t, response)
	assertAdapterManifest(t, response.Manifests[0])
	assertAdapterRequirement(t, response.Requirements[0], response.Names[0])
	assertAdapterLock(t, response.Locks[0])
	assertAdapterMetadata(t, response.Metadata[0])
	assertAdapterSource(t, response.Sources[0])
}

func assertAdapterIdentity(t *testing.T, response Response) {
	t.Helper()
	if response.Protocol != Protocol || response.PackagingVersion != PackagingVersion || len(response.Manifests) != 1 || len(response.Locks) != 1 || len(response.Metadata) != 1 || len(response.Sources) != 1 {
		t.Fatalf("response = %+v", response)
	}
}

func assertAdapterManifest(t *testing.T, manifest Manifest) {
	t.Helper()
	if manifest.Error != "" || len(manifest.Assignments) != 2 || len(manifest.Requirements) == 0 {
		t.Fatalf("manifest = %+v", manifest)
	}
}

func assertAdapterRequirement(t *testing.T, requirement Requirement, name Name) {
	t.Helper()
	if requirement.Name != "example-name" || requirement.Specifier != "==1.2" || name.Normalized != "example-name" {
		t.Fatalf("requirement = %+v, name = %+v", requirement, name)
	}
}

func assertAdapterLock(t *testing.T, lock Lock) {
	t.Helper()
	if lock.Error != "" || lock.Version != 1 || lock.Revision != 3 || lock.RequiresPython != "==3.12.*" || len(lock.Packages) != 1 || lock.Packages[0].Name != "example-name" || len(lock.Packages[0].Source) != 1 {
		t.Fatalf("lock = %+v", lock)
	}
}

func assertAdapterMetadata(t *testing.T, metadata Distribution) {
	t.Helper()
	if metadata.Error != "" || metadata.NameInput != "Example_Name" || metadata.Name != "example-name" || metadata.Version != "1.2" || len(metadata.Requirements) != 1 || metadata.Requirements[0].Name != "requests" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func assertAdapterSource(t *testing.T, source Source) {
	t.Helper()
	if source.Error != "" || len(source.Imports) != 1 || len(source.ComputedImports) != 1 || source.ComputedImports[0].Callee != "importlib.import_module" {
		t.Fatalf("source = %+v", source)
	}
	if source.ComputedImports[0].EvidenceError == "" {
		t.Fatalf("unbounded computed import evidence = %+v", source.ComputedImports[0])
	}
}

func TestAdapterTracesOnlyBoundedComputedImportTargets(t *testing.T) {
	python, err := DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	response, err := Analyze(python, Request{Sources: []Input{{Path: "src/app.py", Source: `import importlib
import importlib.metadata
import json
from pathlib import Path
importlib.import_module(("app.plugins.first", "app.plugins.second")[choice])
importlib.import_module(json.loads(Path("plugins.json").read_text(encoding="utf-8"))["enabled"][choice])
importlib.import_module(importlib.metadata.entry_points(group="app.plugins")[choice].module)
`}}})
	if err != nil {
		t.Fatal(err)
	}
	source := response.Sources[0]
	if source.Error != "" || len(source.ComputedImports) != 3 {
		t.Fatalf("source = %+v", source)
	}
	if strings.Join(source.ComputedImports[0].Targets, ",") != "app.plugins.first,app.plugins.second" || source.ComputedImports[0].EvidenceError != "" {
		t.Fatalf("enumeration = %+v", source.ComputedImports[0])
	}
	configuration := source.ComputedImports[1]
	if len(configuration.Configuration) != 1 || configuration.Configuration[0].Path != "plugins.json" || configuration.Configuration[0].JSONPointer != "/enabled" || configuration.EvidenceError != "" {
		t.Fatalf("configuration = %+v", configuration)
	}
	if source.ComputedImports[2].EntryPointGroup != "app.plugins" || source.ComputedImports[2].EvidenceError != "" {
		t.Fatalf("entry point = %+v", source.ComputedImports[2])
	}
}

func TestAdapterFindsComputedImportsInDefinitionExpressions(t *testing.T) {
	python, err := DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	response, err := Analyze(python, Request{Sources: []Input{{Path: "definitions.py", Source: `import importlib
def load(value: importlib.import_module(annotation_name) = importlib.import_module(default_name)) -> importlib.import_module(return_name):
    return value
loader = lambda: importlib.import_module(lambda_name)
@importlib.import_module(decorator_name)
class Plugin(importlib.import_module(base_name), metaclass=importlib.import_module(metaclass_name)):
    setting = importlib.import_module(class_name)
`}}})
	if err != nil {
		t.Fatal(err)
	}
	facts := response.Sources[0]
	if facts.Error != "" || len(facts.ComputedImports) != 8 {
		t.Fatalf("source = %+v", facts)
	}
	arguments := make([]string, 0, len(facts.ComputedImports))
	for _, fact := range facts.ComputedImports {
		arguments = append(arguments, fact.Argument)
	}
	slices.Sort(arguments)
	if strings.Join(arguments, ",") != "annotation_name,base_name,class_name,decorator_name,default_name,lambda_name,metaclass_name,return_name" {
		t.Fatalf("computed imports = %+v", facts.ComputedImports)
	}
	if facts.ComputedImports[3].Callable != "<lambda>" {
		t.Fatalf("lambda computed import = %+v", facts.ComputedImports[3])
	}
}

func TestAdapterDoesNotTreatShadowedAliasesAsComputedImports(t *testing.T) {
	python, err := DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	response, err := Analyze(python, Request{Sources: []Input{{Path: "shadowed.py", Source: `import importlib
def parameter(importlib):
    return importlib.import_module(parameter_name)
def local():
    import other as importlib
    return importlib.import_module(local_name)
for importlib in values:
    importlib.import_module(loop_name)
import other as importlib
importlib.import_module(module_name)
value = lambda importlib: importlib.import_module(lambda_name)
`}}})
	if err != nil {
		t.Fatal(err)
	}
	facts := response.Sources[0]
	if facts.Error != "" || len(facts.ComputedImports) != 0 {
		t.Fatalf("source = %+v", facts)
	}
}

func TestAdapterFailsClosedForSyntaxIdentityAndResourceBoundaries(t *testing.T) {
	python, err := DefaultInterpreter()
	if err != nil {
		t.Fatal(err)
	}
	response, err := Analyze(python, Request{
		Manifests: []Input{{Path: "pyproject.toml", Source: "[project\n"}},
		Metadata:  []Input{{Path: "broken.dist-info/METADATA", Source: "Metadata-Version: 2.4\nName:\nVersion: no version spaces\n\n"}},
		Sources:   []Input{{Path: "future.py", Source: "type Alias[T = int] = list[T]\n"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Manifests[0].Error == "" || response.Metadata[0].Error == "" || response.Sources[0].Error == "" {
		t.Fatalf("response = %+v", response)
	}
	if _, err := Analyze(python, Request{Requirements: []string{strings.Repeat("x", maximumRequestSize)}}); err == nil {
		t.Fatal("oversized request was accepted")
	}
	if _, err := Analyze(python, Request{Metadata: []Input{{Path: "broken.dist-info/METADATA", Source: string([]byte{0xff})}}}); err == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
}
