package pythonfacts

import (
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
	if source.Error != "" || len(source.Imports) != 1 {
		t.Fatalf("source = %+v", source)
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
