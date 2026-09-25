package rustfacts

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestResolvedCargoMetadataRetainsSourceAndRenamedIdentity(t *testing.T) {
	t.Parallel()
	repo, wire, graph := resolvedMetadataFixture(t)
	resolution, err := ParseResolution(repo, "Cargo.toml", resolvedMetadataJSON(t, wire, graph))
	if err != nil {
		t.Fatal(err)
	}
	dependency := resolution.Packages[1]
	edge := resolution.Graph.Nodes[0].Deps[0]
	if dependency.ID != "opaque::dependency!id" || dependency.Source == nil || *dependency.Source != repository.CargoCratesIOSource || edge.Package != dependency.ID || edge.Name != "renamed" {
		t.Fatalf("resolved identity = %+v, %+v", dependency, edge)
	}
	lock := repository.CargoLock{Path: "Cargo.lock", Packages: []repository.CargoLockedPackage{
		{Name: "app", Version: "1.0.0"}, {Name: "dep", Version: "2.0.0", Source: repository.CargoCratesIOSource},
	}}
	if err := validateResolutionLock(resolution, lock); err != nil {
		t.Fatal(err)
	}
	lock.Packages[1].Source = "git+https://example.test/dep#" + strings.Repeat("a", 40)
	if err := validateResolutionLock(resolution, lock); err == nil {
		t.Fatal("a same-version package from a different source satisfied locked identity")
	}
}

func TestResolvedCargoMetadataRejectsIncompleteGraphEvidence(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*workspaceWire, *ResolvedGraph){
		"no nodes":                        func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes = nil },
		"unknown node":                    func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes[0].ID = "foreign" },
		"duplicate node":                  func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes[1] = graph.Nodes[0] },
		"unknown edge":                    func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes[0].Deps[0].Package = "foreign" },
		"missing renamed edges":           func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes[0].Deps = nil },
		"disagreeing edges":               func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes[0].Dependencies = []string{} },
		"no dependency kinds":             func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes[0].Deps[0].Kinds = nil },
		"unsupported kind":                func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes[0].Deps[0].Kinds[0].Kind = "unknown" },
		"unknown feature":                 func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes[0].Features = []string{"unknown"} },
		"missing features":                func(_ *workspaceWire, graph *ResolvedGraph) { graph.Nodes[0].Features = nil },
		"incorrect virtual root":          func(_ *workspaceWire, graph *ResolvedGraph) { graph.Root = nil },
		"duplicate package":               func(wire *workspaceWire, _ *ResolvedGraph) { wire.Packages = append(wire.Packages, wire.Packages[1]) },
		"registry pretending to be local": func(wire *workspaceWire, _ *ResolvedGraph) { wire.Packages[1].Manifest = wire.Packages[0].Manifest },
		"registry target in checkout": func(wire *workspaceWire, _ *ResolvedGraph) {
			wire.Packages[1].Targets[0].Path = wire.Packages[0].Targets[0].Path
		},
		"empty source": func(wire *workspaceWire, _ *ResolvedGraph) { *wire.Packages[1].Source = "" },
		"empty root":   func(_ *workspaceWire, graph *ResolvedGraph) { *graph.Root = "" },
	} {
		t.Run(name, func(t *testing.T) {
			repo, wire, graph := resolvedMetadataFixture(t)
			mutate(&wire, &graph)
			if _, err := ParseResolution(repo, "Cargo.toml", resolvedMetadataJSON(t, wire, graph)); err == nil {
				t.Fatal("incomplete resolution was accepted")
			}
		})
	}
}

func TestResolvedCargoMetadataRequiresExplicitResolution(t *testing.T) {
	t.Parallel()
	for _, graph := range []string{"null", `{}`, `{"nodes":[]}`, `{"root":"","nodes":[]}`} {
		repo, wire, _ := resolvedMetadataFixture(t)
		wire.Resolve = json.RawMessage(graph)
		data, err := json.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseResolution(repo, "Cargo.toml", data); err == nil {
			t.Fatalf("missing resolution accepted: %s", graph)
		}
	}
}

func resolvedMetadataFixture(t *testing.T) (repository.Repository, workspaceWire, ResolvedGraph) {
	t.Helper()
	repo, wire := metadataFixture(t)
	directory := ".tools/cargo-home/registry/src/source-id/dep-2.0.0"
	write(t, repo.Root, directory+"/Cargo.toml", "[package]\nname='dep'\nversion='2.0.0'\n")
	write(t, repo.Root, directory+"/src/lib.rs", "pub fn value() {}\n")
	source := repository.CargoCratesIOSource
	dependency := Package{ID: "opaque::dependency!id", Name: "dep", Version: "2.0.0", Source: &source,
		Manifest: filepath.Join(repo.Root, directory, "Cargo.toml"), Edition: "2021", Dependencies: []Dependency{}, Features: map[string][]string{},
		Targets: []Target{{Name: "dep", Kinds: []string{"lib"}, CrateTypes: []string{"lib"}, Path: filepath.Join(repo.Root, directory, "src/lib.rs"), Edition: "2021"}},
	}
	wire.Packages = append(wire.Packages, dependency)
	id := wire.Packages[0].ID
	graph := ResolvedGraph{Root: &id, Nodes: []ResolvedNode{
		{ID: id, Dependencies: []string{dependency.ID}, Features: []string{}, Deps: []ResolvedDependency{{Name: "renamed", Package: dependency.ID, Kinds: []ResolvedDependencyKind{{}}}}},
		{ID: dependency.ID, Dependencies: []string{}, Features: []string{}, Deps: []ResolvedDependency{}},
	}}
	return repo, wire, graph
}

func resolvedMetadataJSON(t *testing.T, wire workspaceWire, graph ResolvedGraph) []byte {
	t.Helper()
	data, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	wire.Resolve = data
	data, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
