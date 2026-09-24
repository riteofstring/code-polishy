package rustfacts

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestCargoMetadataAcceptsOpaqueIDsAndAdditionalFields(t *testing.T) {
	t.Parallel()
	repo, metadata := metadataFixture(t)
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	var expanded map[string]any
	if err := json.Unmarshal(data, &expanded); err != nil {
		t.Fatal(err)
	}
	expanded["future_field"] = map[string]any{"version": 8}
	data, err = json.Marshal(expanded)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ParseWorkspace(repo, "Cargo.toml", data)
	if err != nil || workspace.Packages[0].ID != "opaque::member#identity" || workspace.Packages[0].Targets[0].Path != "src/lib.rs" {
		t.Fatalf("metadata = %+v, %v", workspace, err)
	}
}

func TestCargoMetadataRejectsIncompleteOrEscapingFacts(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*workspaceWire){
		"version":              func(wire *workspaceWire) { wire.Version = 2 },
		"false resolved graph": func(wire *workspaceWire) { wire.Resolve = json.RawMessage(`{"nodes":[]}`) },
		"missing package":      func(wire *workspaceWire) { wire.Packages = nil },
		"duplicate package":    func(wire *workspaceWire) { wire.Packages = append(wire.Packages, wire.Packages[0]) },
		"unknown member":       func(wire *workspaceWire) { wire.Members[0] = "unknown" },
		"default member":       func(wire *workspaceWire) { wire.DefaultMembers = []string{"unknown"} },
		"escape":               func(wire *workspaceWire) { wire.Packages[0].Manifest = filepath.Join(t.TempDir(), "Cargo.toml") },
		"relative manifest":    func(wire *workspaceWire) { wire.Packages[0].Manifest = "Cargo.toml" },
		"missing target":       func(wire *workspaceWire) { wire.Packages[0].Targets[0].Path += ".missing" },
		"edition":              func(wire *workspaceWire) { wire.Packages[0].Edition = "future" },
		"MSRV":                 func(wire *workspaceWire) { wire.Packages[0].RustVersion = "1.99" },
		"unknown output":       func(wire *workspaceWire) { wire.TargetDir = t.TempDir() },
	} {
		t.Run(name, func(t *testing.T) {
			repo, metadata := metadataFixture(t)
			mutate(&metadata)
			data, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseWorkspace(repo, "Cargo.toml", data); err == nil {
				t.Fatal("invalid metadata was accepted")
			}
		})
	}
}

func TestCargoMetadataRejectsTrailingAndOversizedOutput(t *testing.T) {
	t.Parallel()
	repo, metadata := metadataFixture(t)
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseWorkspace(repo, "Cargo.toml", append(data, []byte(" {}")...)); err == nil {
		t.Fatal("trailing metadata value was accepted")
	}
	missing := bytes.Replace(data, []byte(`,"resolve":null`), nil, 1)
	if bytes.Equal(missing, data) {
		t.Fatal("metadata fixture did not contain the resolution field")
	}
	if _, err := ParseWorkspace(repo, "Cargo.toml", missing); err == nil {
		t.Fatal("missing resolution field was accepted")
	}
	if _, err := ParseWorkspace(repo, "Cargo.toml", make([]byte, 8<<20+1)); err == nil {
		t.Fatal("oversized metadata was accepted")
	}
}

func metadataFixture(t *testing.T) (repository.Repository, workspaceWire) {
	t.Helper()
	repo := repository.Repository{Root: t.TempDir(), PolicyRoot: t.TempDir()}
	write(t, repo.PolicyRoot, "tools/rust-version.txt", "1.97.1\n")
	write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\n")
	write(t, repo.Root, "src/lib.rs", "pub fn app() {}\n")
	id := "opaque::member#identity"
	return repo, workspaceWire{
		Version: 1, Root: repo.Root, TargetDir: TargetDirectory(repo), Resolve: json.RawMessage("null"),
		Members: []string{id}, DefaultMembers: []string{id}, Packages: []Package{{
			ID: id, Name: "app", Version: "1.0.0", Manifest: filepath.Join(repo.Root, "Cargo.toml"), Edition: "2024",
			Dependencies: []Dependency{}, Features: map[string][]string{},
			Targets: []Target{{Name: "app", Kinds: []string{"lib"}, CrateTypes: []string{"lib"}, Path: filepath.Join(repo.Root, "src/lib.rs"), Edition: "2024", Test: true, Doctest: true}},
		}},
	}
}
