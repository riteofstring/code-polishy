package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func rustGraphInputs(repo repository.Repository, part sourceGraphPart, plan rustfacts.SourcePlan, sources rustfacts.Sources, discovery rustfacts.Discovery) ([]sourcegraph.FactInput, error) {
	result := []sourcegraph.FactInput{}
	for _, nodes := range providerNodeGroups(part.nodes) {
		input := sourcegraph.FactInput{Analyzer: "rust-facts", Protocol: rustfacts.SourceProtocol, Root: nodes[0].Root}
		input.Project = filepath.ToSlash(filepath.Join(input.Root, "Cargo.toml"))
		selected := map[string]bool{}
		for _, node := range nodes {
			input.Paths = append(input.Paths, node.Path)
			selected[node.Path] = true
		}
		facts := rustGraphProjectFacts(repo, plan, sources, discovery, input.Project, selected)
		edges := []sourcegraph.Edge{}
		for _, edge := range part.edges {
			if selected[edge.Source] {
				edges = append(edges, edge)
			}
		}
		for _, evidence := range []struct {
			destination *string
			value       any
		}{
			{&input.FactsSHA256, facts}, {&input.PartitionsSHA256, nodes}, {&input.ResolutionSHA256, edges},
		} {
			data, err := json.Marshal(evidence.value)
			if err != nil {
				return nil, err
			}
			digest := sha256.Sum256(data)
			*evidence.destination = hex.EncodeToString(digest[:])
		}
		result = append(result, input)
	}
	return result, nil
}

type rustProjectGraphFacts struct {
	Python  string
	Rust    string
	Sources []rustfacts.Source
	Inputs  []rustfacts.SourceInput
	Package rustfacts.Package
}

func rustGraphProjectFacts(repo repository.Repository, plan rustfacts.SourcePlan, sources rustfacts.Sources, discovery rustfacts.Discovery, manifest string, selected map[string]bool) rustProjectGraphFacts {
	facts := rustProjectGraphFacts{Python: repo.ToolPin("python"), Rust: repo.ToolPin("rust")}
	for _, source := range sources.Files {
		if selected[source.Path] {
			facts.Sources = append(facts.Sources, source)
		}
	}
	for _, input := range plan.Inputs {
		if selected[input.Path] {
			facts.Inputs = append(facts.Inputs, input)
		}
	}
	for _, workspace := range discovery.Workspaces {
		for _, pkg := range workspace.Packages {
			if pkg.Manifest == manifest {
				facts.Package = pkg
			}
		}
	}
	return facts
}
