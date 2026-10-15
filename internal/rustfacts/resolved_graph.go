package rustfacts

import (
	"errors"
	"fmt"
	"slices"
)

func validateResolvedGraph(graph ResolvedGraph, packages []Package, manifest string) error {
	ids := map[string]Package{}
	root := ""
	for _, pkg := range packages {
		ids[pkg.ID] = pkg
		if pkg.Manifest == manifest {
			root = pkg.ID
		}
	}
	if !resolvedRootMatches(graph.Root, root) {
		return errors.New("cargo resolve root does not identify the requested manifest")
	}
	seen := map[string]bool{}
	for _, node := range graph.Nodes {
		pkg, ok := ids[node.ID]
		if !ok || seen[node.ID] {
			return fmt.Errorf("cargo resolve graph has an unknown or duplicate node %q", node.ID)
		}
		seen[node.ID] = true
		if err := validateResolvedNode(node, pkg, ids); err != nil {
			return fmt.Errorf("cargo resolve node %q: %w", node.ID, err)
		}
	}
	return nil
}

func resolvedRootMatches(root *string, expected string) bool {
	if root == nil {
		return expected == ""
	}
	return *root != "" && *root == expected
}

func validateResolvedNode(node ResolvedNode, pkg Package, ids map[string]Package) error {
	if node.Dependencies == nil || node.Deps == nil || node.Features == nil {
		return errors.New("dependency identities, renamed edges and enabled features are required")
	}
	features := map[string]bool{}
	for _, feature := range node.Features {
		if _, ok := pkg.Features[feature]; !ok || features[feature] {
			return fmt.Errorf("enabled feature %q is unknown or repeated", feature)
		}
		features[feature] = true
	}
	dependencies, err := validateResolvedEdges(node.Deps, ids)
	if err != nil {
		return err
	}
	declared := slices.Clone(node.Dependencies)
	slices.Sort(declared)
	if !slices.Equal(declared, dependencies) {
		return errors.New("dependency identities and renamed edges disagree")
	}
	return nil
}

func validateResolvedEdges(edges []ResolvedDependency, ids map[string]Package) ([]string, error) {
	seen := map[[2]string]bool{}
	dependencies := map[string]bool{}
	for _, edge := range edges {
		key := [2]string{edge.Name, edge.Package}
		if _, ok := ids[edge.Package]; !ok || edge.Name == "" || seen[key] {
			return nil, errors.New("resolved dependency edge has an unknown, empty or repeated identity")
		}
		seen[key] = true
		dependencies[edge.Package] = true
		if err := validateResolvedKinds(edge.Kinds); err != nil {
			return nil, err
		}
	}
	result := make([]string, 0, len(dependencies))
	for id := range dependencies {
		result = append(result, id)
	}
	slices.Sort(result)
	return result, nil
}

func validateResolvedKinds(kinds []ResolvedDependencyKind) error {
	if len(kinds) == 0 {
		return errors.New("resolved dependency edge has no dependency kind")
	}
	seen := map[ResolvedDependencyKind]bool{}
	for _, kind := range kinds {
		if !slices.Contains([]string{"", "dev", "build"}, kind.Kind) || seen[kind] {
			return errors.New("resolved dependency edge has an unsupported or repeated kind")
		}
		seen[kind] = true
	}
	return nil
}
