package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const ReviewTopologyProtocol = "architecture-topology/v1"

type ReviewTopology struct {
	Protocol  string                 `json:"protocol"`
	Identity  string                 `json:"identity"`
	Modules   []policy.Module        `json:"modules"`
	TestPaths []string               `json:"testPaths"`
	Ownership []policy.TestOwnership `json:"ownership"`
	Nodes     []sourcegraph.Node     `json:"nodes"`
	Edges     []ReviewEdge           `json:"edges"`
	External  []ReviewExternal       `json:"externalCompositions"`
}

type ReviewEdge struct {
	Source           string               `json:"source"`
	Target           string               `json:"target"`
	SourceResolution string               `json:"sourceResolutionUnit"`
	TargetResolution string               `json:"targetResolutionUnit"`
	Ecosystem        string               `json:"ecosystem"`
	Kind             sourcegraph.EdgeKind `json:"kind"`
}

type ReviewSignal struct {
	ID      string   `json:"id"`
	Module  string   `json:"module,omitempty"`
	Summary string   `json:"summary"`
	Paths   []string `json:"paths"`
	Units   []string `json:"units"`
}

type ReviewTopologyDiff struct {
	Previous         string           `json:"previous"`
	Current          string           `json:"current"`
	AddedNodes       []string         `json:"addedNodes"`
	RemovedNodes     []string         `json:"removedNodes"`
	ChangedNodes     []string         `json:"changedNodes"`
	AddedEdges       []ReviewEdge     `json:"addedEdges"`
	RemovedEdges     []ReviewEdge     `json:"removedEdges"`
	ChangedModules   []string         `json:"changedModules"`
	OwnershipChanged bool             `json:"ownershipChanged"`
	AddedExternal    []ReviewExternal `json:"addedExternalCompositions"`
	RemovedExternal  []ReviewExternal `json:"removedExternalCompositions"`
}

func BuildReviewTopology(repo repository.Repository, graph sourcegraph.Graph) (ReviewTopology, error) {
	canonical, err := sourcegraph.New(graph.Nodes, graph.Edges, graph.Inputs, graph.External)
	if err != nil {
		return ReviewTopology{}, err
	}
	if canonical.Protocol != graph.Protocol || canonical.Identity != graph.Identity {
		return ReviewTopology{}, fmt.Errorf("architecture review requires the complete canonical source graph")
	}
	topology := ReviewTopology{Protocol: ReviewTopologyProtocol, Modules: []policy.Module{}, TestPaths: sortedReviewStrings(repo.Config.Tests.Paths), Ownership: []policy.TestOwnership{}, Nodes: canonical.Nodes, Edges: []ReviewEdge{}}
	topology.External = reviewExternalCompositions(canonical.External)
	for _, module := range repo.Config.Modules {
		module.Paths = sortedReviewStrings(module.Paths)
		module.DependsOn = sortedReviewStrings(module.DependsOn)
		module.Capabilities = sortedReviewStrings(module.Capabilities)
		topology.Modules = append(topology.Modules, module)
	}
	sort.Slice(topology.Modules, func(i, j int) bool { return topology.Modules[i].Name < topology.Modules[j].Name })
	for _, ownership := range repo.Config.Tests.Ownership {
		ownership.Paths = sortedReviewStrings(ownership.Paths)
		topology.Ownership = append(topology.Ownership, ownership)
	}
	sort.Slice(topology.Ownership, func(i, j int) bool {
		return reviewJSONKey(topology.Ownership[i]) < reviewJSONKey(topology.Ownership[j])
	})
	for _, edge := range canonical.Edges {
		topology.Edges = append(topology.Edges, ReviewEdge{Source: edge.Source, Target: edge.Target, SourceResolution: edge.SourceResolution, TargetResolution: edge.TargetResolution, Ecosystem: edge.Ecosystem, Kind: edge.Kind})
	}
	sort.Slice(topology.Edges, func(i, j int) bool { return reviewJSONKey(topology.Edges[i]) < reviewJSONKey(topology.Edges[j]) })
	topology.Edges = slices.Compact(topology.Edges)
	data, err := json.Marshal(topology)
	if err != nil {
		return ReviewTopology{}, err
	}
	digest := sha256.Sum256(data)
	topology.Identity = hex.EncodeToString(digest[:])
	return topology, nil
}

func CompareReviewTopology(previous, current ReviewTopology) ReviewTopologyDiff {
	difference := ReviewTopologyDiff{
		Previous: previous.Identity, Current: current.Identity,
		AddedNodes: []string{}, RemovedNodes: []string{}, ChangedNodes: []string{}, ChangedModules: []string{},
		AddedEdges: []ReviewEdge{}, RemovedEdges: []ReviewEdge{},
		AddedExternal: reviewExternalDifference(current.External, previous.External), RemovedExternal: reviewExternalDifference(previous.External, current.External),
		OwnershipChanged: reviewJSONKey(previous.Ownership) != reviewJSONKey(current.Ownership) || !slices.Equal(previous.TestPaths, current.TestPaths),
	}
	before, after := map[string]sourcegraph.Node{}, map[string]sourcegraph.Node{}
	for _, node := range previous.Nodes {
		before[node.Path] = node
	}
	for _, node := range current.Nodes {
		after[node.Path] = node
		prior, exists := before[node.Path]
		if !exists {
			difference.AddedNodes = append(difference.AddedNodes, node.Path)
		} else if prior != node {
			difference.ChangedNodes = append(difference.ChangedNodes, node.Path)
		}
	}
	for path := range before {
		if _, exists := after[path]; !exists {
			difference.RemovedNodes = append(difference.RemovedNodes, path)
		}
	}
	difference.AddedEdges = reviewEdgeDifference(current.Edges, previous.Edges)
	difference.RemovedEdges = reviewEdgeDifference(previous.Edges, current.Edges)
	difference.ChangedModules = changedReviewModules(previous.Modules, current.Modules)
	slices.Sort(difference.RemovedNodes)
	return difference
}

func reviewEdgeDifference(candidates, baseline []ReviewEdge) []ReviewEdge {
	known := make(map[ReviewEdge]bool, len(baseline))
	for _, edge := range baseline {
		known[edge] = true
	}
	result := []ReviewEdge{}
	for _, edge := range candidates {
		if !known[edge] {
			result = append(result, edge)
		}
	}
	return result
}

func changedReviewModules(previous, current []policy.Module) []string {
	before, after := map[string]string{}, map[string]string{}
	for _, module := range previous {
		before[module.Name] = reviewJSONKey(module)
	}
	for _, module := range current {
		after[module.Name] = reviewJSONKey(module)
	}
	changed := []string{}
	for name, value := range before {
		if after[name] != value {
			changed = append(changed, name)
		}
	}
	for name, value := range after {
		if before[name] != value {
			changed = append(changed, name)
		}
	}
	return sortedReviewStrings(changed)
}

func sortedReviewStrings(values []string) []string {
	result := append([]string{}, values...)
	slices.Sort(result)
	return slices.Compact(result)
}

func reviewJSONKey(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
