package sourcegraph

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

func normalizeFactInputs(inputs []FactInput, nodes map[string]Node) ([]FactInput, error) {
	if len(inputs) > len(nodes) {
		return nil, fmt.Errorf("source dependency graph has more fact projects than source nodes")
	}
	result := make([]FactInput, 0, len(inputs))
	projects := map[string]bool{}
	covered := map[string]bool{}
	for _, input := range inputs {
		normalized, err := normalizeFactInput(input)
		if err != nil {
			return nil, err
		}
		if projects[normalized.Project] {
			return nil, fmt.Errorf("source dependency graph repeats fact project %q", normalized.Project)
		}
		projects[normalized.Project] = true
		if err := coverFactInput(normalized, nodes, covered); err != nil {
			return nil, err
		}
		result = append(result, normalized)
	}
	for path, node := range nodes {
		if node.Language == "python" && !covered[path] {
			return nil, fmt.Errorf("source dependency graph has no validated Python fact input for %q", path)
		}
	}
	slices.SortFunc(result, func(left, right FactInput) int { return strings.Compare(left.Project, right.Project) })
	return result, nil
}

func normalizeFactInput(input FactInput) (FactInput, error) {
	if input.Analyzer != "python-facts" || input.Protocol != "python-facts/v3" {
		return FactInput{}, fmt.Errorf("source dependency graph has an unsupported fact analyzer or protocol")
	}
	root, err := normalizePath(input.Root, true)
	if err != nil {
		return FactInput{}, fmt.Errorf("source dependency graph fact root: %w", err)
	}
	project, err := normalizePath(input.Project, false)
	if err != nil || project != factProjectManifest(root) {
		return FactInput{}, fmt.Errorf("source dependency graph fact project does not match its root")
	}
	for _, digest := range []string{input.FactsSHA256, input.PartitionsSHA256, input.ResolutionSHA256} {
		if !factDigest(digest) {
			return FactInput{}, fmt.Errorf("source dependency graph fact project %q has an invalid evidence digest", project)
		}
	}
	if len(input.Paths) == 0 || len(input.Paths) > MaximumNodes {
		return FactInput{}, fmt.Errorf("source dependency graph fact project %q has an invalid source count", project)
	}
	input.Root, input.Project = root, project
	input.Paths, err = normalizeFactPaths(input.Paths)
	return input, err
}

func normalizeFactPaths(paths []string) ([]string, error) {
	result := slices.Clone(paths)
	for index, path := range result {
		normalized, err := normalizePath(path, false)
		if err != nil {
			return nil, fmt.Errorf("source dependency graph fact path: %w", err)
		}
		result[index] = normalized
	}
	slices.Sort(result)
	return result, nil
}

func coverFactInput(input FactInput, nodes map[string]Node, covered map[string]bool) error {
	for _, path := range input.Paths {
		node, exists := nodes[path]
		if !exists || node.Language != "python" || node.Root != input.Root || covered[path] {
			return fmt.Errorf("source dependency graph fact path %q is absent, duplicated, or outside its Python project", path)
		}
		covered[path] = true
	}
	return nil
}

func factProjectManifest(root string) string {
	if root == "." {
		return "pyproject.toml"
	}
	return root + "/pyproject.toml"
}

func factDigest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func Validate(graph Graph) error {
	canonical, err := New(graph.Nodes, graph.Edges, graph.Inputs, graph.External)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(canonical, graph) {
		return fmt.Errorf("source dependency graph is not canonical or its identity does not match")
	}
	return nil
}

func Clone(graph *Graph) *Graph {
	if graph == nil {
		return nil
	}
	result := *graph
	result.Nodes = slices.Clone(graph.Nodes)
	result.Edges = slices.Clone(graph.Edges)
	result.External = slices.Clone(graph.External)
	result.Inputs = slices.Clone(graph.Inputs)
	for index := range result.Inputs {
		result.Inputs[index].Paths = slices.Clone(result.Inputs[index].Paths)
	}
	return &result
}
