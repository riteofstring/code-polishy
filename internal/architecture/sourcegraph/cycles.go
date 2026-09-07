package sourcegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
)

func CyclicComponents(graph Graph) ([]Component, error) {
	graph.Edges = slices.DeleteFunc(slices.Clone(graph.Edges), func(edge Edge) bool {
		return edge.Kind == EdgeTypeOnly
	})
	units, forward, reverse := graphTopology(graph)
	order, err := finishingOrder(units, forward)
	if err != nil {
		return nil, err
	}
	groups, err := reverseComponents(order, reverse)
	if err != nil {
		return nil, err
	}
	parts := partitionComponents(groups, graph)
	components := []Component{}
	for index, group := range groups {
		if !cyclicGroup(group, parts[index].Edges) {
			continue
		}
		component, componentErr := makeComponent(group, parts[index])
		if componentErr != nil {
			return nil, componentErr
		}
		components = append(components, component)
	}
	sort.Slice(components, func(left, right int) bool { return components[left].Identity < components[right].Identity })
	return components, nil
}

func graphTopology(graph Graph) ([]string, map[string][]string, map[string][]string) {
	unitSet := map[string]bool{}
	for _, node := range graph.Nodes {
		unitSet[node.Resolution] = true
	}
	units := make([]string, 0, len(unitSet))
	for unit := range unitSet {
		units = append(units, unit)
	}
	sort.Strings(units)
	forwardSets := map[string]map[string]bool{}
	reverseSets := map[string]map[string]bool{}
	for _, edge := range graph.Edges {
		if forwardSets[edge.SourceResolution] == nil {
			forwardSets[edge.SourceResolution] = map[string]bool{}
		}
		if reverseSets[edge.TargetResolution] == nil {
			reverseSets[edge.TargetResolution] = map[string]bool{}
		}
		forwardSets[edge.SourceResolution][edge.TargetResolution] = true
		reverseSets[edge.TargetResolution][edge.SourceResolution] = true
	}
	return units, sortedTopology(units, forwardSets), sortedTopology(units, reverseSets)
}

func sortedTopology(units []string, sets map[string]map[string]bool) map[string][]string {
	result := make(map[string][]string, len(units))
	for _, unit := range units {
		for neighbor := range sets[unit] {
			result[unit] = append(result[unit], neighbor)
		}
		sort.Strings(result[unit])
	}
	return result
}

type traversalFrame struct {
	unit string
	next int
}

func finishingOrder(units []string, edges map[string][]string) ([]string, error) {
	visited := map[string]bool{}
	order := make([]string, 0, len(units))
	for _, start := range units {
		if visited[start] {
			continue
		}
		visited[start] = true
		stack := []traversalFrame{{unit: start}}
		for len(stack) > 0 {
			if len(stack) > MaximumTraversalDepth {
				return nil, fmt.Errorf("source dependency graph exceeds the %d traversal depth limit", MaximumTraversalDepth)
			}
			frame := &stack[len(stack)-1]
			neighbors := edges[frame.unit]
			if frame.next < len(neighbors) {
				neighbor := neighbors[frame.next]
				frame.next++
				if !visited[neighbor] {
					visited[neighbor] = true
					stack = append(stack, traversalFrame{unit: neighbor})
				}
				continue
			}
			order = append(order, frame.unit)
			stack = stack[:len(stack)-1]
		}
	}
	return order, nil
}

func reverseComponents(order []string, reverse map[string][]string) ([][]string, error) {
	visited := map[string]bool{}
	groups := [][]string{}
	for index := len(order) - 1; index >= 0; index-- {
		start := order[index]
		if visited[start] {
			continue
		}
		visited[start] = true
		stack := []string{start}
		group := []string{}
		for len(stack) > 0 {
			if len(stack) > MaximumTraversalDepth {
				return nil, fmt.Errorf("source dependency graph exceeds the %d traversal depth limit", MaximumTraversalDepth)
			}
			last := len(stack) - 1
			unit := stack[last]
			stack = stack[:last]
			group = append(group, unit)
			neighbors := reverse[unit]
			for neighborIndex := len(neighbors) - 1; neighborIndex >= 0; neighborIndex-- {
				neighbor := neighbors[neighborIndex]
				if !visited[neighbor] {
					visited[neighbor] = true
					stack = append(stack, neighbor)
				}
			}
		}
		sort.Strings(group)
		groups = append(groups, group)
	}
	return groups, nil
}

func cyclicGroup(units []string, edges []Edge) bool {
	if len(units) > 1 {
		return true
	}
	for _, edge := range edges {
		if edge.SourceResolution == units[0] && edge.TargetResolution == units[0] {
			return true
		}
	}
	return false
}

func partitionComponents(groups [][]string, graph Graph) []Component {
	groupByUnit := make(map[string]int, len(graph.Nodes))
	parts := make([]Component, len(groups))
	for index, group := range groups {
		for _, unit := range group {
			groupByUnit[unit] = index
		}
	}
	for _, node := range graph.Nodes {
		index := groupByUnit[node.Resolution]
		parts[index].Members = append(parts[index].Members, node)
	}
	for _, edge := range graph.Edges {
		index := groupByUnit[edge.SourceResolution]
		if index == groupByUnit[edge.TargetResolution] {
			parts[index].Edges = append(parts[index].Edges, edge)
		}
	}
	return parts
}

func makeComponent(units []string, component Component) (Component, error) {
	classification, witnessEdges, err := classifyComponent(units, component.Members, component.Edges)
	if err != nil {
		return Component{}, err
	}
	component.Classification = classification
	witness, err := canonicalWitness(units, witnessEdges)
	if err != nil {
		return Component{}, err
	}
	component.Witness = witness
	component.Identity, err = componentIdentity(component)
	if err != nil {
		return Component{}, err
	}
	return component, nil
}

func classifyComponent(units []string, members []Node, edges []Edge) (string, []Edge, error) {
	nodes := make(map[string]Node, len(members))
	for _, node := range members {
		nodes[node.Path] = node
	}
	production := make([]Edge, 0, len(edges))
	handwritten := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		node, found := nodes[edge.Source]
		if !found {
			return "", nil, fmt.Errorf("cyclic source dependency component has no source node for %q", edge.Source)
		}
		if node.Test {
			continue
		}
		production = append(production, edge)
		if !node.Generated {
			handwritten = append(handwritten, edge)
		}
	}
	cyclic, err := componentEdgesAreCyclic(units, production)
	if err != nil {
		return "", nil, err
	}
	if !cyclic {
		return "test-only", edges, nil
	}
	cyclic, err = componentEdgesAreCyclic(units, handwritten)
	if err != nil {
		return "", nil, err
	}
	if !cyclic {
		return "generated", production, nil
	}
	return "production", handwritten, nil
}

func componentEdgesAreCyclic(units []string, edges []Edge) (bool, error) {
	forwardSets := map[string]map[string]bool{}
	reverseSets := map[string]map[string]bool{}
	for _, edge := range edges {
		if forwardSets[edge.SourceResolution] == nil {
			forwardSets[edge.SourceResolution] = map[string]bool{}
		}
		if reverseSets[edge.TargetResolution] == nil {
			reverseSets[edge.TargetResolution] = map[string]bool{}
		}
		forwardSets[edge.SourceResolution][edge.TargetResolution] = true
		reverseSets[edge.TargetResolution][edge.SourceResolution] = true
	}
	forward := sortedTopology(units, forwardSets)
	reverse := sortedTopology(units, reverseSets)
	order, err := finishingOrder(units, forward)
	if err != nil {
		return false, err
	}
	groups, err := reverseComponents(order, reverse)
	if err != nil {
		return false, err
	}
	selfLoops := map[string]bool{}
	for _, edge := range edges {
		if edge.SourceResolution == edge.TargetResolution {
			selfLoops[edge.SourceResolution] = true
		}
	}
	for _, group := range groups {
		if len(group) > 1 || selfLoops[group[0]] {
			return true, nil
		}
	}
	return false, nil
}

func componentIdentity(component Component) (string, error) {
	type semanticEdge struct {
		Source     string   `json:"source"`
		Target     string   `json:"target"`
		SourceUnit string   `json:"sourceUnit"`
		TargetUnit string   `json:"targetUnit"`
		Ecosystem  string   `json:"ecosystem"`
		Kind       EdgeKind `json:"kind"`
	}
	edges := make([]semanticEdge, 0, len(component.Edges))
	for _, edge := range component.Edges {
		edges = append(edges, semanticEdge{
			Source: edge.Source, Target: edge.Target, SourceUnit: edge.SourceResolution,
			TargetUnit: edge.TargetResolution, Ecosystem: edge.Ecosystem, Kind: edge.Kind,
		})
	}
	sort.Slice(edges, func(left, right int) bool {
		leftData, _ := json.Marshal(edges[left])
		rightData, _ := json.Marshal(edges[right])
		return string(leftData) < string(rightData)
	})
	edges = slices.Compact(edges)
	data, err := json.Marshal(struct {
		Classification string         `json:"classification"`
		Members        []Node         `json:"members"`
		Edges          []semanticEdge `json:"edges"`
	}{Classification: component.Classification, Members: component.Members, Edges: edges})
	if err != nil {
		return "", fmt.Errorf("encode source dependency component: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func canonicalWitness(units []string, edges []Edge) ([]Edge, error) {
	outgoing := componentOutgoing(edges)
	start, err := canonicalWitnessStart(units, edges, outgoing)
	if err != nil {
		return nil, err
	}
	for _, first := range outgoing[start] {
		if first.TargetResolution == start {
			return []Edge{first}, nil
		}
		path, found, err := shortestWitnessPath(first.TargetResolution, start, outgoing)
		if err != nil {
			return nil, err
		}
		if found {
			return append([]Edge{first}, path...), nil
		}
	}
	return nil, fmt.Errorf("cyclic source dependency component has no canonical witness")
}

func canonicalWitnessStart(units []string, edges []Edge, outgoing map[string][]Edge) (string, error) {
	forward := make(map[string][]string, len(units))
	reverse := make(map[string][]string, len(units))
	for _, edge := range edges {
		forward[edge.SourceResolution] = append(forward[edge.SourceResolution], edge.TargetResolution)
		reverse[edge.TargetResolution] = append(reverse[edge.TargetResolution], edge.SourceResolution)
	}
	order, err := finishingOrder(units, forward)
	if err != nil {
		return "", err
	}
	groups, err := reverseComponents(order, reverse)
	if err != nil {
		return "", err
	}
	start := ""
	for _, group := range groups {
		if cyclicGroup(group, outgoing[group[0]]) && (start == "" || group[0] < start) {
			start = group[0]
		}
	}
	return start, nil
}

func componentOutgoing(edges []Edge) map[string][]Edge {
	result := map[string][]Edge{}
	for _, edge := range edges {
		result[edge.SourceResolution] = append(result[edge.SourceResolution], edge)
	}
	for unit := range result {
		sort.Slice(result[unit], func(left, right int) bool {
			return edgeIdentity(result[unit][left]) < edgeIdentity(result[unit][right])
		})
	}
	return result
}

type witnessPredecessor struct {
	unit string
	edge Edge
}

func shortestWitnessPath(start, goal string, outgoing map[string][]Edge) ([]Edge, bool, error) {
	queue := []string{start}
	depth := map[string]int{start: 0}
	previous := map[string]witnessPredecessor{}
	for len(queue) > 0 {
		unit := queue[0]
		queue = queue[1:]
		for _, edge := range outgoing[unit] {
			next := edge.TargetResolution
			if next == goal {
				return reconstructWitness(start, unit, edge, previous), true, nil
			}
			if _, seen := depth[next]; seen {
				continue
			}
			depth[next] = depth[unit] + 1
			if depth[next] > MaximumTraversalDepth {
				return nil, false, fmt.Errorf("source dependency graph exceeds the %d traversal depth limit", MaximumTraversalDepth)
			}
			previous[next] = witnessPredecessor{unit: unit, edge: edge}
			queue = append(queue, next)
		}
	}
	return nil, false, nil
}

func reconstructWitness(start, beforeGoal string, final Edge, previous map[string]witnessPredecessor) []Edge {
	reversed := []Edge{final}
	for current := beforeGoal; current != start; {
		predecessor := previous[current]
		reversed = append(reversed, predecessor.edge)
		current = predecessor.unit
	}
	result := make([]Edge, len(reversed))
	for index := range reversed {
		result[len(reversed)-1-index] = reversed[index]
	}
	return result
}
