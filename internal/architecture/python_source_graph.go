package architecture

import (
	"fmt"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonGraphSourcePart(
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	selectedSources []string,
	owners map[string]string,
	allFiles []string,
	graph pythonGraph,
	sourceFacts map[string]pythonSourceFact,
) sourceGraphPart {
	dependencies, coverage, err := pythonGraphDependencies(repo, project, sources, graph)
	if err != nil {
		return sourceGraphPart{findings: pythonCoverageForSources(sources, err.Error()), incomplete: true}
	}
	pythonGraphMissingCoverage(sources, selectedSources, dependencies, coverage)
	pythonGraphTargetCoverage(repo, project, owners, allFiles, dependencies, coverage)
	pythonGraphImportCoverage(repo, project, sources, dependencies, coverage, sourceFacts)
	findings := pythonCoverageFindings(coverage)
	findings = append(findings, pythonModuleDependencyFindings(repo, selectedSources, dependencies, coverage)...)
	part := sourceGraphPart{findings: pythonSortedFindings(findings), incomplete: len(pythonCoverageFindings(coverage)) > 0}
	if part.incomplete {
		return part
	}
	part.nodes = pythonGraphNodes(repo, project, sources, dependencies)
	edges, evidenceCoverage := pythonGraphEdges(repo, project, sources, dependencies, sourceFacts)
	part.edges = edges
	if len(evidenceCoverage) > 0 {
		part.findings = append(part.findings, evidenceCoverage...)
		part.findings = pythonSortedFindings(part.findings)
		part.incomplete = true
	}
	return part
}

func pythonGraphNodes(
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	dependencies map[string]map[string]bool,
) []sourcegraph.Node {
	paths := map[string]bool{}
	for _, source := range sources {
		paths[source] = true
	}
	for _, targets := range dependencies {
		for target := range targets {
			paths[target] = true
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	nodes := make([]sourcegraph.Node, 0, len(ordered))
	for _, path := range ordered {
		owner, err := sourceModuleOwner(repo, path)
		if err != nil {
			continue
		}
		nodes = append(nodes, sourcegraph.Node{
			Path: path, Language: "python", Generated: repo.IsGenerated(path), Test: repo.IsTest(path),
			Root: project.Root, Module: owner, Resolution: "file:" + path,
		})
	}
	return nodes
}

func pythonGraphEdges(
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	dependencies map[string]map[string]bool,
	facts map[string]pythonSourceFact,
) ([]sourcegraph.Edge, []policy.Finding) {
	index := newPythonModuleIndex(project)
	edges := []sourcegraph.Edge{}
	covered := map[string]map[string]bool{}
	seen := map[string]bool{}
	for _, source := range sources {
		covered[source] = map[string]bool{}
		for _, reference := range facts[source].Imports {
			for _, target := range pythonReferenceTargets(index, source, reference, dependencies[source]) {
				edge := pythonGraphEdge(source, target, reference.Line, reference.Column, reference.Kind)
				identity := pythonEdgeIdentity(edge)
				if !seen[identity] {
					seen[identity] = true
					edges = append(edges, edge)
				}
				covered[source][target] = true
			}
		}
		for _, computed := range facts[source].ComputedImports {
			for _, target := range pythonComputedGraphTargets(repo, project, index, source, computed) {
				if !dependencies[source][target] {
					continue
				}
				edge := pythonGraphEdge(source, target, computed.Line, computed.Column, computed.Kind)
				identity := pythonEdgeIdentity(edge)
				if !seen[identity] {
					seen[identity] = true
					edges = append(edges, edge)
				}
				covered[source][target] = true
			}
		}
		edges = append(edges, pythonObjectGraphEdges(index, source, facts[source].ObjectImports, dependencies[source], covered[source], seen)...)
	}
	sort.Slice(edges, func(left, right int) bool { return pythonEdgeIdentity(edges[left]) < pythonEdgeIdentity(edges[right]) })
	return edges, pythonGraphEdgeCoverage(sources, dependencies, covered)
}

func pythonGraphEdgeCoverage(sources []string, dependencies, covered map[string]map[string]bool) []policy.Finding {
	coverage := []policy.Finding{}
	for _, source := range sources {
		missing := []string{}
		for target := range dependencies[source] {
			if !covered[source][target] {
				missing = append(missing, target)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			coverage = append(coverage, pythonImportCoverage(source,
				fmt.Sprintf("resolved local dependency edges have no exact Python source evidence: %s", strings.Join(missing, ", "))))
		}
	}
	return coverage
}

func pythonReferenceTargets(
	index pythonModuleIndex,
	source string,
	reference pythonImportReference,
	dependencies map[string]bool,
) []string {
	module, valid := pythonResolvedImportModule(index, source, reference.Module)
	if !valid || !pythonLocalModule(index, module) {
		return nil
	}
	candidates := []string{}
	if len(reference.Names) == 0 {
		candidates = append(candidates, index.files[module]...)
	} else {
		for _, name := range reference.Names {
			if name != "*" {
				candidates = append(candidates, index.files[module+"."+name]...)
			}
			candidates = append(candidates, index.files[module]...)
		}
	}
	return selectedPythonTargets(candidates, dependencies)
}

func pythonComputedGraphTargets(
	repo repository.Repository,
	project repository.PythonProject,
	index pythonModuleIndex,
	source string,
	fact pythonComputedImportFact,
) []string {
	declarations := pythonComputedDeclarations(repo.Config.Scope.PythonComputedImports, project.Manifest, source)
	position, message := pythonComputedDeclarationPosition(declarations, fact)
	if message != "" {
		return nil
	}
	targets, message := pythonComputedTargets(repo, project, declarations[position])
	if message != "" {
		return nil
	}
	paths := []string{}
	for _, target := range targets {
		paths = append(paths, index.files[target]...)
	}
	return selectedPythonTargets(paths, nil)
}

func selectedPythonTargets(candidates []string, selected map[string]bool) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] || selected != nil && !selected[candidate] {
			continue
		}
		seen[candidate] = true
		result = append(result, candidate)
	}
	sort.Strings(result)
	return result
}

func pythonGraphEdge(source, target string, line, column int, kind string) sourcegraph.Edge {
	return sourcegraph.Edge{
		Source: source, Target: target, SourceResolution: "file:" + source, TargetResolution: "file:" + target,
		Line: line, Column: column, Ecosystem: "python", Kind: sourcegraph.EdgeKind(kind),
	}
}

func pythonEdgeIdentity(edge sourcegraph.Edge) string {
	return fmt.Sprintf("%s\x00%010d\x00%010d\x00%s\x00%s", edge.Source, edge.Line, edge.Column, edge.Target, edge.Kind)
}
