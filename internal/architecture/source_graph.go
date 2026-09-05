package architecture

import (
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type sourceGraphPart struct {
	nodes       []sourcegraph.Node
	edges       []sourcegraph.Edge
	inputs      []sourcegraph.FactInput
	external    []sourcegraph.ExternalComposition
	findings    []policy.Finding
	incomplete  bool
	testImports map[string][]string
}

func mergeSourceGraphParts(parts ...sourceGraphPart) sourceGraphPart {
	merged := sourceGraphPart{}
	for _, part := range parts {
		if part.testImports == nil && !part.incomplete {
			part.testImports = resolvedTestImports(part.nodes, part.edges)
		}
		if merged.testImports == nil {
			merged.testImports = map[string][]string{}
		}
		for path, imports := range part.testImports {
			merged.testImports[path] = imports
		}
		merged.nodes = append(merged.nodes, part.nodes...)
		merged.edges = append(merged.edges, part.edges...)
		merged.inputs = append(merged.inputs, part.inputs...)
		merged.external = append(merged.external, part.external...)
		merged.findings = append(merged.findings, part.findings...)
		merged.incomplete = merged.incomplete || part.incomplete
	}
	return merged
}

func completeSourceGraph(part sourceGraphPart, selected []string) (*sourcegraph.Graph, []policy.Finding) {
	if part.incomplete {
		return nil, part.findings
	}
	unowned := []policy.Finding{}
	for _, node := range part.nodes {
		if node.Module == "" {
			unowned = append(unowned, policy.Finding{Check: "policy.testOwnership", Path: node.Path, Subject: "unmapped", Message: "governed executable test has no explicit tests.ownership entry"})
		}
	}
	if len(unowned) > 0 {
		return nil, append(part.findings, unowned...)
	}
	graph, err := sourcegraph.New(part.nodes, part.edges, part.inputs, part.external)
	if err != nil {
		return nil, append(part.findings, sourceGraphCoverageFinding(err.Error()))
	}
	components, err := sourcegraph.CyclicComponents(graph)
	if err != nil {
		return nil, append(part.findings, sourceGraphCoverageFinding(err.Error()))
	}
	findings := append([]policy.Finding{}, part.findings...)
	for _, component := range components {
		findings = append(findings, sourceCycleFinding(component, selected))
	}
	return &graph, findings
}

func sourceModuleOwner(repo repository.Repository, path string) (string, error) {
	owners := repo.OwnerModuleNames(path)
	if len(owners) == 1 {
		return owners[0], nil
	}
	if len(owners) == 0 && repo.IsTest(path) {
		return "", nil
	}
	return "", fmt.Errorf("source must belong to exactly one repository module")
}

func resolvedTestImports(nodes []sourcegraph.Node, edges []sourcegraph.Edge) map[string][]string {
	tests := map[string]bool{}
	production := map[string][]string{}
	for _, node := range nodes {
		if node.Test {
			tests[node.Path] = true
		} else {
			production[node.Resolution] = append(production[node.Resolution], node.Path)
		}
	}
	result := map[string][]string{}
	for _, edge := range edges {
		if tests[edge.Source] {
			result[edge.Source] = append(result[edge.Source], production[edge.TargetResolution]...)
		}
	}
	for path, imports := range result {
		slices.Sort(imports)
		result[path] = slices.Compact(imports)
	}
	return result
}

func sourceGraphCoverageFinding(message string) policy.Finding {
	return policy.Finding{
		Check: "architecture.sourceGraphCoverage", Path: "repository", Subject: "source-dependency-graph/v1",
		Message: "the canonical source dependency graph is unavailable; cycle findings were withheld: " + message,
	}
}

func sourceCycleFinding(component sourcegraph.Component, selected []string) policy.Finding {
	witness := component.Witness[0]
	rule := "architecture.fileCycle"
	if component.Classification == "test-only" {
		rule = "testing.fileCycle"
	}
	finding := policy.Finding{
		Check: rule, Path: witness.Source, Line: witness.Line, Column: witness.Column,
		Subject: component.Classification, Message: sourceCycleMessage(component),
		Scope:               policy.FindingScope{Kind: "graph-component", Value: component.Identity},
		SemanticIdentity:    []string{component.Identity},
		DependencyComponent: sourceGraphComponentEvidence(component),
		Remediation: policy.FindingRemediation{
			Summary:     "Break at least one dependency edge in this source component without suppressing the cycle.",
			NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "architecture", "--files", witness.Source}, Cwd: "."},
		},
	}
	selectedSet := make(map[string]bool, len(selected))
	for _, path := range selected {
		selectedSet[path] = true
	}
	for _, member := range component.Members {
		if member.Path != finding.Path {
			finding.Related = append(finding.Related, policy.FindingLocation{Path: member.Path, Message: "cycle member"})
		}
		if selectedSet[member.Path] && !selectedSet[finding.Path] {
			finding.SelectionEvidence = append(finding.SelectionEvidence, policy.FindingSelectionEvidence{
				Selected: policy.FindingLocation{Path: member.Path},
				Related:  policy.FindingLocation{Path: finding.Path, Line: finding.Line, Column: finding.Column},
				Kind:     "source-dependency-cycle",
			})
		}
	}
	return finding
}

func sourceCycleMessage(component sourcegraph.Component) string {
	const maximumWitnessEdges = 20
	shown := component.Witness
	omitted := 0
	if len(shown) > maximumWitnessEdges {
		omitted = len(shown) - maximumWitnessEdges
		shown = shown[:maximumWitnessEdges]
	}
	parts := make([]string, 0, len(shown))
	for _, edge := range shown {
		parts = append(parts, fmt.Sprintf("%s:%d -> %s [%s]", edge.Source, edge.Line, edge.Target, edge.Kind))
	}
	message := fmt.Sprintf("%s source dependency cycle: %s", component.Classification, strings.Join(parts, "; "))
	if omitted > 0 {
		message += fmt.Sprintf("; ... %d witness edges omitted from human output", omitted)
	}
	return message
}

func sourceGraphComponentEvidence(component sourcegraph.Component) *policy.DependencyComponent {
	evidence := &policy.DependencyComponent{
		Protocol: "source-dependency-component/v1", Classification: component.Classification, Identity: component.Identity,
	}
	for _, node := range component.Members {
		evidence.Members = append(evidence.Members, policy.DependencyNode{
			Path: node.Path, Language: node.Language, Generated: node.Generated, Test: node.Test,
			Root: node.Root, Module: node.Module, Resolution: node.Resolution,
		})
	}
	for _, edge := range component.Edges {
		evidence.Edges = append(evidence.Edges, sourceGraphEdgeEvidence(edge))
	}
	for _, edge := range component.Witness {
		evidence.Witness = append(evidence.Witness, sourceGraphEdgeEvidence(edge))
	}
	evidence.Members = slices.Clip(evidence.Members)
	evidence.Edges = slices.Clip(evidence.Edges)
	evidence.Witness = slices.Clip(evidence.Witness)
	return evidence
}

func sourceGraphEdgeEvidence(edge sourcegraph.Edge) policy.DependencyEdge {
	return policy.DependencyEdge{
		Source: edge.Source, Target: edge.Target, SourceResolution: edge.SourceResolution,
		TargetResolution: edge.TargetResolution, Line: edge.Line, Column: edge.Column,
		Ecosystem: edge.Ecosystem, Kind: string(edge.Kind),
	}
}
