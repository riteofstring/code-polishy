package sourcegraph

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestCyclicComponentsAreCompleteClassifiedAndDeterministic(t *testing.T) {
	t.Parallel()
	nodes := []Node{
		{Path: "app/b.ts", Language: "typescript", Root: ".", Module: "app", Resolution: "file:app/b.ts"},
		{Path: "app/a.ts", Language: "typescript", Root: ".", Module: "app", Resolution: "file:app/a.ts"},
		{Path: "tests/c.ts", Language: "typescript", Test: true, Root: ".", Module: "app", Resolution: "file:tests/c.ts"},
	}
	edges := []Edge{
		{Source: "app/b.ts", Target: "app/a.ts", SourceResolution: "file:app/b.ts", TargetResolution: "file:app/a.ts", Line: 7, Column: 1, Ecosystem: "javascript", Kind: EdgeTypeOnly},
		{Source: "app/a.ts", Target: "app/b.ts", SourceResolution: "file:app/a.ts", TargetResolution: "file:app/b.ts", Line: 3, Column: 2, Ecosystem: "javascript", Kind: EdgeRuntime},
		{Source: "tests/c.ts", Target: "tests/c.ts", SourceResolution: "file:tests/c.ts", TargetResolution: "file:tests/c.ts", Line: 1, Column: 1, Ecosystem: "javascript", Kind: EdgeProvenDynamic},
	}
	graph, err := New(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	components, err := CyclicComponents(graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 2 {
		t.Fatalf("components = %+v", components)
	}
	sortComponentsByClassification(components)
	production := components[0]
	if production.Classification != "production" || len(production.Members) != 2 || len(production.Edges) != 2 || len(production.Witness) != 2 {
		t.Fatalf("production = %+v", production)
	}
	if components[1].Classification != "test-only" || len(components[1].Witness) != 1 {
		t.Fatalf("test component = %+v", components[1])
	}
	reversedNodes := slices.Clone(nodes)
	reversedEdges := slices.Clone(edges)
	slices.Reverse(reversedNodes)
	slices.Reverse(reversedEdges)
	reversedGraph, err := New(reversedNodes, reversedEdges)
	if err != nil {
		t.Fatal(err)
	}
	reversedComponents, err := CyclicComponents(reversedGraph)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Identity != reversedGraph.Identity || !slices.EqualFunc(components, sortedComponents(reversedComponents), func(left, right Component) bool {
		return left.Identity == right.Identity && slices.Equal(left.Witness, right.Witness)
	}) {
		t.Fatalf("graph or component identity changed with input order")
	}
}

func TestLargeStronglyConnectedComponentHasOneBoundedWitness(t *testing.T) {
	t.Parallel()
	const size = 2000
	nodes := make([]Node, 0, size)
	edges := make([]Edge, 0, size)
	for index := 0; index < size; index++ {
		path := fmt.Sprintf("src/%04d.ts", index)
		target := fmt.Sprintf("src/%04d.ts", (index+1)%size)
		nodes = append(nodes, Node{Path: path, Language: "typescript", Root: ".", Module: "app", Resolution: "file:" + path})
		edges = append(edges, Edge{
			Source: path, Target: target, SourceResolution: "file:" + path, TargetResolution: "file:" + target,
			Line: 1, Column: 1, Ecosystem: "javascript", Kind: EdgeRuntime,
		})
	}
	graph, err := New(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	components, err := CyclicComponents(graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || len(components[0].Members) != size || len(components[0].Edges) != size || len(components[0].Witness) != size {
		t.Fatalf("component counts = %d/%d/%d/%d", len(components), len(components[0].Members), len(components[0].Edges), len(components[0].Witness))
	}
}

func TestGoPackageUnitsKeepRealImportingFiles(t *testing.T) {
	t.Parallel()
	nodes := []Node{
		{Path: "a/a.go", Language: "go", Root: ".", Module: "core", Resolution: "go:example.test/app/a"},
		{Path: "a/other.go", Language: "go", Root: ".", Module: "core", Resolution: "go:example.test/app/a"},
		{Path: "b/b.go", Language: "go", Root: ".", Module: "core", Resolution: "go:example.test/app/b"},
	}
	edges := []Edge{
		{Source: "a/other.go", Target: "example.test/app/b", SourceResolution: "go:example.test/app/a", TargetResolution: "go:example.test/app/b", Line: 4, Column: 1, Ecosystem: "go", Kind: EdgeRuntime},
		{Source: "b/b.go", Target: "example.test/app/a", SourceResolution: "go:example.test/app/b", TargetResolution: "go:example.test/app/a", Line: 5, Column: 1, Ecosystem: "go", Kind: EdgeRuntime},
	}
	graph, err := New(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	components, err := CyclicComponents(graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(components) != 1 || len(components[0].Members) != 3 || components[0].Witness[0].Source != "a/other.go" {
		t.Fatalf("components = %+v", components)
	}
}

func TestCycleClassificationUsesTheEdgesRequiredForTheCycle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		nodes          []Node
		edges          []Edge
		classification string
	}{
		{
			name: "test overlay",
			nodes: []Node{
				{Path: "a/a.go", Language: "go", Root: ".", Module: "app", Resolution: "go:a"},
				{Path: "a/a_test.go", Language: "go", Test: true, Root: ".", Module: "app", Resolution: "go:a"},
				{Path: "b/b.go", Language: "go", Root: ".", Module: "app", Resolution: "go:b"},
			},
			edges: []Edge{
				graphTestEdge("a/a_test.go", "b/b.go", "go:a", "go:b"),
				graphTestEdge("b/b.go", "a/a.go", "go:b", "go:a"),
			},
			classification: "test-only",
		},
		{
			name: "generated edge",
			nodes: []Node{
				{Path: "a/a.go", Language: "go", Generated: true, Root: ".", Module: "app", Resolution: "go:a"},
				{Path: "b/b.go", Language: "go", Root: ".", Module: "app", Resolution: "go:b"},
			},
			edges: []Edge{
				graphTestEdge("a/a.go", "b/b.go", "go:a", "go:b"),
				graphTestEdge("b/b.go", "a/a.go", "go:b", "go:a"),
			},
			classification: "generated",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			graph, err := New(testCase.nodes, testCase.edges)
			if err != nil {
				t.Fatal(err)
			}
			components, err := CyclicComponents(graph)
			if err != nil || len(components) != 1 || components[0].Classification != testCase.classification {
				t.Fatalf("components = %+v, error = %v", components, err)
			}
		})
	}
}

func graphTestEdge(source, target, sourceUnit, targetUnit string) Edge {
	return Edge{
		Source: source, Target: target, SourceResolution: sourceUnit, TargetResolution: targetUnit,
		Line: 1, Column: 1, Ecosystem: "go", Kind: EdgeRuntime,
	}
}

func TestGraphRejectsMissingTargetsAndDuplicateEdges(t *testing.T) {
	t.Parallel()
	node := Node{Path: "a.py", Language: "python", Root: ".", Module: "app", Resolution: "file:a.py"}
	edge := Edge{Source: "a.py", Target: "b.py", SourceResolution: "file:a.py", TargetResolution: "file:b.py", Line: 1, Column: 1, Ecosystem: "python", Kind: EdgeRuntime}
	if _, err := New([]Node{node}, []Edge{edge}); err == nil {
		t.Fatal("missing target unit was accepted")
	}
	edge.Target = "a.py"
	edge.TargetResolution = "file:a.py"
	if _, err := New([]Node{node}, []Edge{edge, edge}); err == nil {
		t.Fatal("duplicate edge was accepted")
	}
}

func TestGraphRejectsInvalidNodeOwnershipAndResolutionCollisions(t *testing.T) {
	t.Parallel()
	cases := [][]Node{
		{{Path: "src/a.rs", Language: "rust", Root: "src", Module: "app", Resolution: "file:src/a.rs"}},
		{{Path: "outside/a.ts", Language: "typescript", Root: "src", Module: "app", Resolution: "file:outside/a.ts"}},
		{
			{Path: "a/a.go", Language: "go", Root: "a", Module: "a", Resolution: "shared"},
			{Path: "b/b.go", Language: "go", Root: "b", Module: "b", Resolution: "shared"},
		},
		{
			{Path: "a/a.go", Language: "go", Root: ".", Module: "a", Resolution: "shared"},
			{Path: "a/b.go", Language: "go", Root: ".", Module: "b", Resolution: "shared"},
		},
	}
	for _, nodes := range cases {
		if _, err := New(nodes, nil); err == nil {
			t.Fatalf("invalid nodes were accepted: %+v", nodes)
		}
	}
}

func TestEmptyGraphReportsAnExplicitEmptyInventory(t *testing.T) {
	t.Parallel()
	graph, err := New(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if string(document["nodes"]) != "[]" || string(document["edges"]) != "[]" {
		t.Fatalf("empty inventory document = %s", data)
	}
	components, err := CyclicComponents(graph)
	if err != nil || len(components) != 0 {
		t.Fatalf("empty components = %+v, error = %v", components, err)
	}
}

func TestGraphRejectsItsExactNodeResourceOverrun(t *testing.T) {
	t.Parallel()
	nodes := make([]Node, MaximumNodes+1)
	if _, err := New(nodes, nil); err == nil {
		t.Fatal("node overrun was accepted")
	}
}

func TestMixedComponentWitnessUsesItsProductionSubcycle(t *testing.T) {
	t.Parallel()
	nodes := []Node{
		{Path: "a.test.ts", Language: "typescript", Root: ".", Module: "app", Test: true, Resolution: "file:a.test.ts"},
		{Path: "b.ts", Language: "typescript", Root: ".", Module: "app", Resolution: "file:b.ts"},
		{Path: "c.ts", Language: "typescript", Root: ".", Module: "app", Resolution: "file:c.ts"},
	}
	edges := []Edge{}
	for _, pair := range [][2]string{{"a.test.ts", "b.ts"}, {"b.ts", "a.test.ts"}, {"b.ts", "c.ts"}, {"c.ts", "b.ts"}} {
		edges = append(edges, Edge{Source: pair[0], Target: pair[1], SourceResolution: "file:" + pair[0], TargetResolution: "file:" + pair[1], Line: 1, Column: 1, Ecosystem: "javascript", Kind: EdgeRuntime})
	}
	graph, err := New(nodes, edges)
	if err != nil {
		t.Fatal(err)
	}
	components, err := CyclicComponents(graph)
	if err != nil || len(components) != 1 {
		t.Fatalf("components = %+v, error = %v", components, err)
	}
	component := components[0]
	if component.Classification != "production" || len(component.Members) != 3 || len(component.Edges) != 4 || len(component.Witness) != 2 || component.Witness[0].Source != "b.ts" || component.Witness[1].Source != "c.ts" {
		t.Fatalf("mixed component = %+v", component)
	}
}

func TestCycleFingerprintIgnoresLocationsButBindsOwnership(t *testing.T) {
	t.Parallel()
	node := Node{Path: "app.py", Language: "python", Root: ".", Module: "app", Resolution: "file:app.py"}
	edge := Edge{Source: "app.py", Target: "app.py", SourceResolution: "file:app.py", TargetResolution: "file:app.py", Line: 1, Column: 1, Ecosystem: "python", Kind: EdgeRuntime}
	identity := func(nodes []Node, edges []Edge) string {
		graph, err := New(nodes, edges)
		if err != nil {
			t.Fatal(err)
		}
		components, err := CyclicComponents(graph)
		if err != nil || len(components) != 1 {
			t.Fatalf("components = %+v, error = %v", components, err)
		}
		return components[0].Identity
	}
	initial := identity([]Node{node}, []Edge{edge})
	moved := edge
	moved.Line = 15
	if initial != identity([]Node{node}, []Edge{moved, edge}) {
		t.Fatal("locations or repeated import statements changed semantic identity")
	}
	node.Module = "domain"
	if initial == identity([]Node{node}, []Edge{edge}) {
		t.Fatal("changed module owner retained semantic identity")
	}
}

func TestGraphNormalizesPlatformPathsAndRejectsFalseEdgeEvidence(t *testing.T) {
	t.Parallel()
	node := Node{Path: "src/a.py", Language: "python", Root: "src", Module: "app", Resolution: "file:src/a.py"}
	edge := Edge{Source: "src/a.py", Target: "src/a.py", SourceResolution: node.Resolution, TargetResolution: node.Resolution, Line: 1, Column: 1, Ecosystem: "python", Kind: EdgeRuntime}
	graph, err := New([]Node{node}, []Edge{edge})
	if err != nil {
		t.Fatal(err)
	}
	windowsNode, windowsEdge := node, edge
	windowsNode.Path = `src\a.py`
	windowsNode.Resolution = `file:src\a.py`
	windowsEdge.Source, windowsEdge.Target = windowsNode.Path, windowsNode.Path
	windowsEdge.SourceResolution, windowsEdge.TargetResolution = windowsNode.Resolution, windowsNode.Resolution
	windowsGraph, err := New([]Node{windowsNode}, []Edge{windowsEdge})
	if err != nil || !reflect.DeepEqual(graph, windowsGraph) {
		t.Fatalf("normalized graph = %+v, error = %v", windowsGraph, err)
	}
	for _, invalid := range []string{"../a.py", `C:\src\a.py`, "missing.py"} {
		changed := edge
		changed.Target = invalid
		if _, err := New([]Node{node}, []Edge{changed}); err == nil {
			t.Fatalf("target %q accepted", invalid)
		}
	}
	edge.Ecosystem = "go"
	if _, err := New([]Node{node}, []Edge{edge}); err == nil {
		t.Fatal("foreign ecosystem accepted")
	}
	node.Path = strings.ReplaceAll(`C:\src\a.py`, "\\", "/")
	if _, err := New([]Node{node}, nil); err == nil {
		t.Fatal("foreign absolute path accepted")
	}
}

func sortComponentsByClassification(components []Component) {
	if components[0].Classification > components[1].Classification {
		components[0], components[1] = components[1], components[0]
	}
}

func sortedComponents(components []Component) []Component {
	result := slices.Clone(components)
	sortComponentsByClassification(result)
	return result
}
