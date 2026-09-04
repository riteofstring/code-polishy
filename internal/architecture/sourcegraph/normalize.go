package sourcegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"sort"
	"strings"
	"unicode/utf8"
)

func New(nodes []Node, edges []Edge) (Graph, error) {
	if len(nodes) > MaximumNodes {
		return Graph{}, fmt.Errorf("source dependency graph exceeds the %d node limit", MaximumNodes)
	}
	if len(edges) > MaximumEdges {
		return Graph{}, fmt.Errorf("source dependency graph exceeds the %d edge limit", MaximumEdges)
	}
	normalizedNodes, nodeByPath, resolutions, err := normalizeNodes(nodes)
	if err != nil {
		return Graph{}, err
	}
	normalizedEdges, err := normalizeEdges(edges, nodeByPath, resolutions)
	if err != nil {
		return Graph{}, err
	}
	canonical := struct {
		Protocol string `json:"protocol"`
		Nodes    []Node `json:"nodes"`
		Edges    []Edge `json:"edges"`
	}{Protocol: Protocol, Nodes: normalizedNodes, Edges: normalizedEdges}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return Graph{}, fmt.Errorf("encode source dependency graph: %w", err)
	}
	if len(encoded) > MaximumEncodedBytes {
		return Graph{}, fmt.Errorf("source dependency graph exceeds the %d byte limit", MaximumEncodedBytes)
	}
	digest := sha256.Sum256(encoded)
	return Graph{
		Protocol: Protocol, Identity: hex.EncodeToString(digest[:]),
		Nodes: normalizedNodes, Edges: normalizedEdges,
	}, nil
}

func normalizeNodes(nodes []Node) ([]Node, map[string]Node, map[string]bool, error) {
	normalized := append([]Node{}, nodes...)
	byPath := make(map[string]Node, len(nodes))
	resolutions := map[string]bool{}
	resolutionShapes := map[string]string{}
	productionOwners := map[string]string{}
	for index := range normalized {
		node, err := normalizeNode(normalized[index])
		if err != nil {
			return nil, nil, nil, err
		}
		if _, duplicate := byPath[node.Path]; duplicate {
			return nil, nil, nil, fmt.Errorf("source dependency graph repeats node %q", node.Path)
		}
		normalized[index] = node
		byPath[node.Path] = node
		shape := strings.Join([]string{node.Language, node.Root}, "\x00")
		if previous, found := resolutionShapes[node.Resolution]; found && previous != shape {
			return nil, nil, nil, fmt.Errorf("source dependency graph resolution unit %q crosses language or project roots", node.Resolution)
		}
		resolutionShapes[node.Resolution] = shape
		if !node.Test {
			if previous, found := productionOwners[node.Resolution]; found && previous != node.Module {
				return nil, nil, nil, fmt.Errorf("source dependency graph production unit %q crosses module ownership", node.Resolution)
			}
			productionOwners[node.Resolution] = node.Module
		}
		resolutions[node.Resolution] = true
	}
	sort.Slice(normalized, func(left, right int) bool { return normalized[left].Path < normalized[right].Path })
	return normalized, byPath, resolutions, nil
}

func normalizeNode(node Node) (Node, error) {
	path, err := normalizePath(node.Path, false)
	if err != nil {
		return Node{}, fmt.Errorf("source dependency graph node path: %w", err)
	}
	root, err := normalizePath(node.Root, true)
	if err != nil {
		return Node{}, fmt.Errorf("source dependency graph node root: %w", err)
	}
	if !validLanguage(node.Language) || !boundedAtom(node.Module) || !boundedIdentity(node.Resolution) {
		return Node{}, fmt.Errorf("source dependency graph node %q has invalid identity fields", path)
	}
	if root != "." && path != root && !strings.HasPrefix(path, root+"/") {
		return Node{}, fmt.Errorf("source dependency graph node %q is outside root %q", path, root)
	}
	node.Path = path
	node.Root = root
	if node.Language != "go" {
		node.Resolution = strings.ReplaceAll(node.Resolution, "\\", "/")
		if node.Resolution != "file:"+path {
			return Node{}, fmt.Errorf("source dependency graph node %q has a mismatched file resolution unit", path)
		}
	}
	return node, nil
}

func normalizeEdges(edges []Edge, nodes map[string]Node, resolutions map[string]bool) ([]Edge, error) {
	normalized := append([]Edge{}, edges...)
	seen := map[string]bool{}
	for index := range normalized {
		edge, err := normalizeEdge(normalized[index], nodes, resolutions)
		if err != nil {
			return nil, err
		}
		identity := edgeIdentity(edge)
		if seen[identity] {
			return nil, fmt.Errorf("source dependency graph repeats an edge from %q at line %d", edge.Source, edge.Line)
		}
		seen[identity] = true
		normalized[index] = edge
	}
	sort.Slice(normalized, func(left, right int) bool { return edgeIdentity(normalized[left]) < edgeIdentity(normalized[right]) })
	return normalized, nil
}

func normalizeEdge(edge Edge, nodes map[string]Node, resolutions map[string]bool) (Edge, error) {
	source, err := normalizePath(edge.Source, false)
	if err != nil {
		return Edge{}, fmt.Errorf("source dependency graph edge source: %w", err)
	}
	node, found := nodes[source]
	if !found {
		return Edge{}, fmt.Errorf("source dependency graph edge names absent source %q", source)
	}
	edge.Source = source
	if node.Language != "go" {
		edge.SourceResolution = strings.ReplaceAll(edge.SourceResolution, "\\", "/")
		edge.TargetResolution = strings.ReplaceAll(edge.TargetResolution, "\\", "/")
	}
	if err := validateEdgeEvidence(edge, node, resolutions); err != nil {
		return Edge{}, err
	}
	return normalizeEdgeTarget(edge, node, nodes)
}

func validateEdgeEvidence(edge Edge, node Node, resolutions map[string]bool) error {
	if !boundedIdentity(edge.Target) || !boundedIdentity(edge.SourceResolution) || !boundedIdentity(edge.TargetResolution) {
		return fmt.Errorf("source dependency graph edge from %q has invalid target identity", edge.Source)
	}
	if edge.SourceResolution != node.Resolution {
		return fmt.Errorf("source dependency graph edge from %q has a mismatched source unit", edge.Source)
	}
	if !resolutions[edge.TargetResolution] {
		return fmt.Errorf("source dependency graph edge from %q names absent target unit %q", edge.Source, edge.TargetResolution)
	}
	if edge.Line <= 0 || edge.Column <= 0 || !validEcosystem(edge.Ecosystem) || !validKind(edge.Kind) {
		return fmt.Errorf("source dependency graph edge from %q has invalid evidence", edge.Source)
	}
	return nil
}

func normalizeEdgeTarget(edge Edge, node Node, nodes map[string]Node) (Edge, error) {
	expectedLanguage := edge.Ecosystem
	if edge.Ecosystem == "javascript" {
		expectedLanguage = "typescript"
	}
	if node.Language != expectedLanguage {
		return Edge{}, fmt.Errorf("source dependency graph edge from %q has a mismatched ecosystem", edge.Source)
	}
	if node.Language != "go" {
		target, targetErr := normalizePath(edge.Target, false)
		if targetErr != nil {
			return Edge{}, fmt.Errorf("source dependency graph edge target: %w", targetErr)
		}
		targetNode, exists := nodes[target]
		if !exists || targetNode.Resolution != edge.TargetResolution || targetNode.Language != node.Language {
			return Edge{}, fmt.Errorf("source dependency graph edge from %q has a mismatched target file", edge.Source)
		}
		edge.Target = target
	}
	return edge, nil
}

func normalizePath(value string, root bool) (string, error) {
	if value == "." && root {
		return value, nil
	}
	if !boundedAtom(value) {
		return "", fmt.Errorf("path is empty or exceeds its lexical boundary")
	}
	value = strings.ReplaceAll(value, "\\", "/")
	cleaned := pathpkg.Clean(value)
	if cleaned == "." || strings.Contains(cleaned, ":") || strings.HasPrefix(cleaned, "/") || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path %q is not repository-contained", value)
	}
	return cleaned, nil
}

func boundedAtom(value string) bool {
	return value != "" && len(value) <= MaximumPathBytes && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func boundedIdentity(value string) bool {
	return boundedAtom(value)
}

func validEcosystem(value string) bool {
	return value == "go" || value == "javascript" || value == "python"
}

func validLanguage(value string) bool {
	return value == "go" || value == "python" || value == "typescript"
}

func validKind(value EdgeKind) bool {
	return value == EdgeRuntime || value == EdgeTypeOnly || value == EdgeReExport || value == EdgeProvenDynamic
}

func edgeIdentity(edge Edge) string {
	return strings.Join([]string{
		edge.SourceResolution, edge.TargetResolution, edge.Source,
		fmt.Sprintf("%010d", edge.Line), fmt.Sprintf("%010d", edge.Column),
		edge.Ecosystem, string(edge.Kind), edge.Target,
	}, "\x00")
}
