package rustfacts

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type Resolution struct {
	Manifest  string
	Workspace Workspace
	Packages  []Package
	Graph     ResolvedGraph
	Lock      repository.CargoLock
}

type ResolvedGraph struct {
	Root  *string        `json:"root"`
	Nodes []ResolvedNode `json:"nodes"`
}

type ResolvedNode struct {
	ID           string               `json:"id"`
	Dependencies []string             `json:"dependencies"`
	Deps         []ResolvedDependency `json:"deps"`
	Features     []string             `json:"features"`
}

type ResolvedDependency struct {
	Name    string                   `json:"name"`
	Package string                   `json:"pkg"`
	Kinds   []ResolvedDependencyKind `json:"dep_kinds"`
}

type ResolvedDependencyKind struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

func ParseResolution(repo repository.Repository, manifest string, data []byte) (Resolution, error) {
	wire, err := decodeMetadata(data)
	if err != nil {
		return Resolution{}, err
	}
	workspaceWire := wire
	workspaceWire.Packages = slices.DeleteFunc(slices.Clone(wire.Packages), func(pkg Package) bool { return !slices.Contains(wire.Members, pkg.ID) })
	workspace, err := workspaceFromWire(repo, manifest, workspaceWire)
	if err != nil {
		return Resolution{}, err
	}
	packages, err := resolvedPackages(repo, wire.Packages, workspace)
	if err != nil {
		return Resolution{}, err
	}
	graph, err := parseResolvedGraph(wire.Resolve, packages, manifest)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Manifest: manifest, Workspace: workspace, Packages: packages, Graph: graph}, nil
}

func resolvedPackages(repo repository.Repository, packages []Package, workspace Workspace) ([]Package, error) {
	members := map[string]Package{}
	for _, pkg := range workspace.Packages {
		members[pkg.ID] = pkg
	}
	seen := map[string]bool{}
	for index := range packages {
		pkg := &packages[index]
		if seen[pkg.ID] || !completePackage(*pkg) {
			return nil, fmt.Errorf("resolved Cargo package %q has incomplete or duplicate identity", pkg.ID)
		}
		seen[pkg.ID] = true
		if member, ok := members[pkg.ID]; ok {
			*pkg = member
			continue
		}
		if err := validatePackage(repo, pkg); err != nil {
			return nil, fmt.Errorf("resolved Cargo package %q: %w", pkg.ID, err)
		}
		if err := resolvedSourcePaths(*pkg); err != nil {
			return nil, err
		}
	}
	return packages, nil
}

func resolvedSourcePaths(pkg Package) error {
	if pkg.Source == nil {
		return nil
	}
	if *pkg.Source == "" || !strings.HasPrefix(pkg.Manifest, ".tools/cargo-home/") {
		return fmt.Errorf("resolved remote Cargo package %q is outside the governed Cargo home or has no source identity", pkg.ID)
	}
	for _, target := range pkg.Targets {
		if !strings.HasPrefix(target.Path, ".tools/cargo-home/") {
			return fmt.Errorf("resolved remote Cargo target %q is outside the governed Cargo home", target.Path)
		}
	}
	return nil
}

func validateResolutionLock(resolution Resolution, lock repository.CargoLock) error {
	identities := map[[3]string]bool{}
	for _, pkg := range lock.Packages {
		identities[[3]string{pkg.Name, pkg.Version, pkg.Source}] = true
	}
	for _, pkg := range resolution.Packages {
		source := ""
		if pkg.Source != nil {
			source = *pkg.Source
		}
		if !identities[[3]string{pkg.Name, pkg.Version, source}] {
			return fmt.Errorf("resolved Cargo package %q is absent from the reviewed lock inventory", pkg.ID)
		}
	}
	expected := filepath.ToSlash(filepath.Join(resolution.Workspace.Root, "Cargo.lock"))
	if lock.Path != expected {
		return errors.New("cargo resolution does not describe the reviewed workspace lock")
	}
	return nil
}

func parseResolvedGraph(data []byte, packages []Package, manifest string) (ResolvedGraph, error) {
	var wire struct {
		Root  json.RawMessage `json:"root"`
		Nodes []ResolvedNode  `json:"nodes"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return ResolvedGraph{}, fmt.Errorf("invalid Cargo resolve graph: %w", err)
	}
	if len(wire.Root) == 0 || len(wire.Nodes) != len(packages) {
		return ResolvedGraph{}, errors.New("cargo resolve graph must describe every resolved package and the current root")
	}
	graph := ResolvedGraph{Nodes: wire.Nodes}
	if err := json.Unmarshal(wire.Root, &graph.Root); err != nil {
		return ResolvedGraph{}, fmt.Errorf("invalid Cargo resolve root: %w", err)
	}
	if err := validateResolvedGraph(graph, packages, manifest); err != nil {
		return ResolvedGraph{}, err
	}
	return graph, nil
}
