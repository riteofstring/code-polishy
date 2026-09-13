package sourcegraph

import (
	"strings"
	"testing"
)

func TestProviderGraphBindsNewLanguagesAndPreservesCrossLanguageCycles(t *testing.T) {
	nodes := []Node{{Path: "src/view.custom", Language: "custom", Module: "view", Root: ".", Resolution: "file:src/view.custom"}, {Path: "src/native.ts", Language: "typescript", Module: "native", Root: ".", Resolution: "file:src/native.ts"}}
	edges := []Edge{{Source: nodes[0].Path, Target: nodes[1].Path, SourceResolution: nodes[0].Resolution, TargetResolution: nodes[1].Resolution, Line: 1, Column: 1, Ecosystem: "custom", Kind: EdgeRuntime}, {Source: nodes[1].Path, Target: nodes[0].Path, SourceResolution: nodes[1].Resolution, TargetResolution: nodes[0].Resolution, Line: 1, Column: 1, Ecosystem: "javascript", Kind: EdgeRuntime}}
	digest := strings.Repeat("a", 64)
	input := FactInput{Analyzer: "pack", Protocol: "code-polishy-pack/v3", Project: "pack/custom/analyze", Root: ".", Paths: []string{nodes[0].Path}, FactsSHA256: digest, PartitionsSHA256: digest, ResolutionSHA256: digest, Provider: &ProviderInput{Name: "custom", Version: "1.0.0", Digest: digest, Languages: []string{"custom"}, InputsSHA256: digest, PolicySHA256: digest, RuntimeSHA256: digest}}
	graph, err := New(nodes, edges, []FactInput{input}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cycles, err := CyclicComponents(graph)
	if err != nil || len(cycles) != 1 {
		t.Fatalf("cross-language cycle disappeared: %+v %v", cycles, err)
	}
	for _, mutate := range []func(*Graph){
		func(g *Graph) { g.Inputs = nil },
		func(g *Graph) { g.Inputs[0].Provider.Languages = []string{"another"} },
		func(g *Graph) { g.Inputs[0].Provider.Digest = "" },
		func(g *Graph) { g.Inputs[0].Paths = []string{nodes[1].Path} },
		func(g *Graph) { g.Inputs[0].Provider.PolicySHA256 = "invalid" },
	} {
		altered := Clone(&graph)
		mutate(altered)
		if _, err := New(altered.Nodes, altered.Edges, altered.Inputs, nil); err == nil {
			t.Fatal("mismatched provider evidence was accepted")
		}
	}
	altered := Clone(&graph)
	altered.Inputs[0].Provider.Digest = strings.Repeat("b", 64)
	if Validate(*altered) == nil {
		t.Fatal("changed provider retained a trusted graph identity")
	}
	updated, err := New(altered.Nodes, altered.Edges, altered.Inputs, nil)
	if err != nil || updated.Identity == graph.Identity {
		t.Fatalf("provider change did not invalidate graph identity: %v", err)
	}
}
