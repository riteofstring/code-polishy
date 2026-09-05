package sourcegraph

import (
	"reflect"
	"strings"
	"testing"
)

func TestExternalCompositionIsSeparateFromLocalCyclesAndOwnsItsEvidence(t *testing.T) {
	node := Node{Path: "app.py", Language: "python", Root: ".", Module: "application", Resolution: "file:app.py"}
	edge := externalGraphTestEdge()
	inputs := []FactInput{graphTestFactInput(".", "app.py")}
	external := []ExternalComposition{edge}
	graph, err := New([]Node{node}, nil, inputs, external)
	if err != nil {
		t.Fatal(err)
	}
	external[0].Dependency.Namespace = "changed.namespace"
	if len(graph.Nodes) != 1 || len(graph.Edges) != 0 || len(graph.External) != 1 || graph.External[0].Dependency.Namespace != "third_party.plugins" {
		t.Fatalf("external edge changed the local graph or retained mutable caller state: %+v", graph)
	}
	components, err := CyclicComponents(graph)
	if err != nil || len(components) != 0 {
		t.Fatalf("external edge participated in local cycles: %+v, %v", components, err)
	}
	clone := Clone(&graph)
	clone.External[0].Contract.Protocol = "app.Other"
	if graph.External[0].Contract.Protocol != "app.Contract" || Validate(*clone) == nil {
		t.Fatal("changed external contract retained canonical graph evidence")
	}
	rebuilt, err := New(clone.Nodes, clone.Edges, clone.Inputs, clone.External)
	if err != nil || rebuilt.Identity == graph.Identity {
		t.Fatalf("changed external contract did not invalidate identity: %v", err)
	}
	if err := Validate(graph); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(graph.External, []ExternalComposition{edge}) {
		t.Fatal("external evidence changed during normalization")
	}
}

func TestExternalCompositionRejectsMissingOrCrossProjectEvidence(t *testing.T) {
	node := Node{Path: "app.py", Language: "python", Root: ".", Module: "application", Resolution: "file:app.py"}
	for name, change := range map[string]func(*ExternalComposition){
		"wildcard namespace":        func(value *ExternalComposition) { value.Dependency.Namespace = "third_party.*" },
		"noncanonical distribution": func(value *ExternalComposition) { value.Dependency.Distribution = "Plug_Dist" },
		"absent source":             func(value *ExternalComposition) { value.Source = "other.py" },
		"wrong resolution":          func(value *ExternalComposition) { value.SourceResolution = "file:other.py" },
		"missing location":          func(value *ExternalComposition) { value.Line = 0 },
		"foreign project":           func(value *ExternalComposition) { value.Dependency.Project = "other/pyproject.toml" },
		"foreign lock":              func(value *ExternalComposition) { value.Dependency.Lock = "other/uv.lock" },
		"local dependency":          func(value *ExternalComposition) { value.Dependency.Kind = "file" },
		"missing dependency digest": func(value *ExternalComposition) { value.Dependency.LockSHA256 = "" },
		"unsupported grammar":       func(value *ExternalComposition) { value.Contract.InputGrammar = "expression" },
		"unsupported check":         func(value *ExternalComposition) { value.Contract.CheckKind = "annotation" },
		"missing runtime digest":    func(value *ExternalComposition) { value.Contract.RuntimeSHA256 = "" },
		"missing input digest":      func(value *ExternalComposition) { value.Contract.InputSHA256 = "" },
	} {
		t.Run(name, func(t *testing.T) {
			edge := externalGraphTestEdge()
			change(&edge)
			if _, err := New([]Node{node}, nil, []FactInput{graphTestFactInput(".", "app.py")}, []ExternalComposition{edge}); err == nil {
				t.Fatal("invalid external composition acquired canonical identity")
			}
		})
	}
	edge := externalGraphTestEdge()
	duplicate := edge
	duplicate.Dependency.Distribution = "another-dist"
	if _, err := New([]Node{node}, nil, []FactInput{graphTestFactInput(".", "app.py")}, []ExternalComposition{edge, duplicate}); err == nil {
		t.Fatal("one loader acquired conflicting external compositions")
	}
}

func externalGraphTestEdge() ExternalComposition {
	return ExternalComposition{Source: "app.py", SourceResolution: "file:app.py", Line: 4, Column: 14, Dependency: ExternalDependency{Project: "pyproject.toml", Lock: "uv.lock", ManifestSHA256: strings.Repeat("a", 64), LockSHA256: strings.Repeat("b", 64), Distribution: "plug-dist", Version: "1.0", Kind: "registry", Source: "https://pypi.org/simple", Namespace: "third_party.plugins"}, Contract: ExternalContract{InputGrammar: "python-module-object/v1", CheckKind: "issubclass", Protocol: "app.Contract", RuntimeType: "app.Contract", CheckLine: 5, CheckColumn: 12, SourceSHA256: strings.Repeat("c", 64), RuntimeSHA256: strings.Repeat("d", 64), InputSHA256: strings.Repeat("e", 64)}}
}
