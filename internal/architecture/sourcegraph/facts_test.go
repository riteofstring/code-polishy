package sourcegraph

import (
	"reflect"
	"strings"
	"testing"
)

func TestGraphFactInputsRequireExactPythonProjectCoverage(t *testing.T) {
	t.Parallel()
	nodes := []Node{
		{Path: "src/a.py", Language: "python", Root: "src", Module: "app", Resolution: "file:src/a.py"},
		{Path: "src/b.py", Language: "python", Root: "src", Module: "app", Resolution: "file:src/b.py"},
	}
	input := graphTestFactInput("src", "src/b.py", "src/a.py")
	graph, err := New(nodes, nil, []FactInput{input}, nil)
	if err != nil || Validate(graph) != nil {
		t.Fatalf("complete fact inputs: %+v, %v", graph, err)
	}
	input.Paths[0] = "mutated.py"
	if !reflect.DeepEqual(graph.Inputs[0].Paths, []string{"src/a.py", "src/b.py"}) {
		t.Fatalf("caller mutation changed canonical paths: %+v", graph.Inputs)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*Graph)
	}{
		{"missing project", func(g *Graph) { g.Inputs = nil }},
		{"missing source", func(g *Graph) { g.Inputs[0].Paths = g.Inputs[0].Paths[:1] }},
		{"duplicate project", func(g *Graph) { g.Inputs = append(g.Inputs, g.Inputs[0]) }},
		{"duplicate source", func(g *Graph) { g.Inputs[0].Paths[1] = g.Inputs[0].Paths[0] }},
		{"foreign source", func(g *Graph) { g.Inputs[0].Paths[0] = "other/a.py" }},
		{"foreign language", func(g *Graph) { g.Nodes[0].Language = "typescript" }},
		{"foreign root", func(g *Graph) { g.Inputs[0].Root = "."; g.Inputs[0].Project = "pyproject.toml" }},
		{"foreign manifest", func(g *Graph) { g.Inputs[0].Project = "other/pyproject.toml" }},
		{"escaped source", func(g *Graph) { g.Inputs[0].Paths[0] = "../a.py" }},
		{"unknown analyzer", func(g *Graph) { g.Inputs[0].Analyzer = "other" }},
		{"old protocol", func(g *Graph) { g.Inputs[0].Protocol = "python-facts/v1" }},
		{"missing facts", func(g *Graph) { g.Inputs[0].FactsSHA256 = "" }},
		{"invalid partitions", func(g *Graph) { g.Inputs[0].PartitionsSHA256 = strings.Repeat("z", 64) }},
		{"uppercase resolution", func(g *Graph) { g.Inputs[0].ResolutionSHA256 = strings.Repeat("A", 64) }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			changed := Clone(&graph)
			testCase.mutate(changed)
			if _, err := New(changed.Nodes, changed.Edges, changed.Inputs, changed.External); err == nil {
				t.Fatal("accepted incomplete or mismatched fact evidence")
			}
		})
	}
}

func TestGraphFactEvidenceChangesIdentityWithoutChangingCycles(t *testing.T) {
	t.Parallel()
	node := Node{Path: "a.py", Language: "python", Root: ".", Module: "app", Resolution: "file:a.py"}
	edge := Edge{Source: node.Path, Target: node.Path, SourceResolution: node.Resolution, TargetResolution: node.Resolution, Line: 1, Column: 1, Ecosystem: "python", Kind: EdgeRuntime}
	graph, err := New([]Node{node}, []Edge{edge}, []FactInput{graphTestFactInput(".", node.Path)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	components, err := CyclicComponents(graph)
	if err != nil || len(components) != 1 {
		t.Fatalf("components: %+v, %v", components, err)
	}
	for _, mutate := range []func(*FactInput){
		func(input *FactInput) { input.FactsSHA256 = strings.Repeat("f", 64) },
		func(input *FactInput) { input.PartitionsSHA256 = strings.Repeat("f", 64) },
		func(input *FactInput) { input.ResolutionSHA256 = strings.Repeat("f", 64) },
	} {
		changed := Clone(&graph)
		mutate(&changed.Inputs[0])
		if Validate(*changed) == nil {
			t.Fatal("stale graph identity accepted changed evidence")
		}
		updated, err := New(changed.Nodes, changed.Edges, changed.Inputs, changed.External)
		if err != nil || updated.Identity == graph.Identity {
			t.Fatalf("unchanged graph evidence identity: %v", err)
		}
		updatedComponents, err := CyclicComponents(updated)
		if err != nil || !reflect.DeepEqual(components, updatedComponents) {
			t.Fatalf("fact evidence changed semantic cycles: %+v, %v", updatedComponents, err)
		}
	}
}

func graphTestFactInput(root string, paths ...string) FactInput {
	return FactInput{Analyzer: "python-facts", Protocol: "python-facts/v3", Project: factProjectManifest(root), Root: root, Paths: paths,
		FactsSHA256: strings.Repeat("a", 64), PartitionsSHA256: strings.Repeat("b", 64), ResolutionSHA256: strings.Repeat("c", 64)}
}
