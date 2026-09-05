package architecture

import (
	"fmt"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
)

type reviewModuleFacts struct {
	paths      []string
	units      []string
	roots      []string
	components []string
}

func ReviewSignals(topology ReviewTopology) []ReviewSignal {
	facts := reviewProductionFacts(topology)
	signals := []ReviewSignal{}
	for _, module := range topology.Modules {
		owned := facts[module.Name]
		if len(owned.paths) == 0 {
			continue
		}
		if len(facts) == 1 {
			signals = append(signals, newReviewSignal("single-production-module", module.Name, "one production module owns all executable production source", owned.paths, owned.units))
		}
		if len(owned.units) > 1 {
			signals = append(signals, newReviewSignal("multiple-projects-or-packages", module.Name, "one module spans independently discovered projects or Go packages", owned.paths, owned.units))
		}
		if len(owned.components) > 1 {
			signals = append(signals, newReviewSignal("disconnected-responsibilities", module.Name, "one module contains disconnected production dependency components", owned.paths, owned.components))
		}
		if len(owned.roots) > 1 && moduleHasCatchAllPath(module) {
			signals = append(signals, newReviewSignal("catch-all-ownership", module.Name, "a repository-wide ownership pattern covers distinct source roots", owned.paths, owned.roots))
		}
	}
	if declaredGraphIsEmpty(topology.Modules) && reviewHasProductionEdges(topology) {
		signals = append(signals, newReviewSignal("empty-declared-graph", "", "the declared module graph has no edges despite internal production dependencies", nil, nil))
	}
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].ID+"\x00"+signals[i].Module < signals[j].ID+"\x00"+signals[j].Module
	})
	return signals
}

func newReviewSignal(id, module, summary string, paths, units []string) ReviewSignal {
	return ReviewSignal{ID: id, Module: module, Summary: summary, Paths: sortedReviewStrings(paths), Units: sortedReviewStrings(units)}
}

func reviewProductionFacts(topology ReviewTopology) map[string]reviewModuleFacts {
	nodes := map[string]sourcegraph.Node{}
	parents := map[string]string{}
	facts := map[string]reviewModuleFacts{}
	for _, node := range topology.Nodes {
		if node.Test {
			continue
		}
		nodes[node.Path] = node
		parents[node.Resolution] = node.Resolution
		owned := facts[node.Module]
		owned.paths = append(owned.paths, node.Path)
		owned.units = append(owned.units, reviewProjectUnit(node))
		root, _, nested := strings.Cut(node.Path, "/")
		if !nested {
			root = "."
		}
		owned.roots = append(owned.roots, root)
		facts[node.Module] = owned
	}
	owners := map[string]string{}
	for _, node := range nodes {
		owners[node.Resolution] = node.Module
	}
	for _, edge := range topology.Edges {
		source, exists := nodes[edge.Source]
		if exists && owners[edge.TargetResolution] == source.Module {
			reviewUnion(parents, source.Resolution, edge.TargetResolution)
		}
	}
	for _, node := range nodes {
		owned := facts[node.Module]
		owned.components = append(owned.components, reviewRoot(parents, node.Resolution))
		facts[node.Module] = owned
	}
	for module, owned := range facts {
		owned.paths, owned.units = sortedReviewStrings(owned.paths), sortedReviewStrings(owned.units)
		owned.roots, owned.components = sortedReviewStrings(owned.roots), sortedReviewStrings(owned.components)
		facts[module] = owned
	}
	return facts
}

func reviewProjectUnit(node sourcegraph.Node) string {
	if node.Language == "go" {
		return node.Resolution
	}
	return node.Language + ":" + node.Root
}

func reviewRoot(parents map[string]string, unit string) string {
	root := unit
	for parents[root] != root {
		root = parents[root]
	}
	for unit != root {
		next := parents[unit]
		parents[unit] = root
		unit = next
	}
	return root
}

func reviewUnion(parents map[string]string, left, right string) {
	left, right = reviewRoot(parents, left), reviewRoot(parents, right)
	if right < left {
		left, right = right, left
	}
	parents[right] = left
}

func moduleHasCatchAllPath(module policy.Module) bool {
	for _, pattern := range module.Paths {
		if strings.HasPrefix(pattern, "**/") || pattern == "**" {
			return true
		}
	}
	return false
}

func declaredGraphIsEmpty(modules []policy.Module) bool {
	for _, module := range modules {
		if len(module.DependsOn) > 0 {
			return false
		}
	}
	return true
}

func reviewHasProductionEdges(topology ReviewTopology) bool {
	production := map[string]bool{}
	for _, node := range topology.Nodes {
		production[node.Path] = !node.Test
	}
	for _, edge := range topology.Edges {
		if production[edge.Source] {
			return true
		}
	}
	return false
}

func ReviewSignalFindings(signals []ReviewSignal, reviewed bool, base string) []policy.Finding {
	findings := []policy.Finding{}
	for _, signal := range signals {
		finding := policy.Finding{Check: "architecture.reviewSignal", Path: policy.ConfigFilename, Module: signal.Module, Subject: signal.ID,
			Message: signal.Summary, Severity: policy.FindingInformation, SemanticIdentity: []string{signal.ID, signal.Module},
			Remediation: policy.FindingRemediation{Summary: "Only when the caller requests architecture review, inspect ownership, boundary depth, and dependency direction using its packet.", NextCommand: &policy.FindingCommand{Argv: []string{"code-polishy", "architecture-review", "prepare", "--base", base}, Cwd: "."}},
			Fields:      map[string]string{"pathCount": fmt.Sprint(len(signal.Paths)), "unitCount": fmt.Sprint(len(signal.Units))},
		}
		if reviewed {
			finding.Status = policy.FindingReviewed
			finding.Remediation.Summary = "Inspect the accepted review evidence; reevaluate if the topology or ownership changes."
			finding.Remediation.NextCommand = &policy.FindingCommand{Argv: []string{"code-polishy", "architecture-review", "status", "--base", base}, Cwd: "."}
		}
		if base == "" {
			finding.Remediation.NextCommand = &policy.FindingCommand{Argv: []string{"code-polishy", "architecture-review", "--help"}, Cwd: "."}
		}
		for _, path := range signal.Paths {
			finding.Related = append(finding.Related, policy.FindingLocation{Path: path, Message: signal.Summary})
		}
		findings = append(findings, finding)
	}
	return findings
}
