package architecture

import (
	"context"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func rustSourceGraph(ctx context.Context, repo repository.Repository, selected, allFiles []string, boundary runner.Runner) sourceGraphPart {
	selected = rustGraphSelection(repo, selected)
	if len(selected) == 0 {
		return sourceGraphPart{}
	}
	plan := rustGraphDiscovery(repo, selected, allFiles)
	discovery := rustfacts.Discover(ctx, repo, plan, boundary)
	sourcePlan := rustGraphSources(repo, allFiles)
	sources := rustfacts.ReadSources(ctx, repo, sourcePlan, boundary)
	graph := rustfacts.ResolveModules(repo, discovery.Workspaces, sources, selected, allFiles)
	part := sourceGraphPart{findings: append(discovery.Findings, graph.Findings...)}
	part.incomplete = len(part.findings) > 0
	governed := map[string]bool{}
	for _, path := range allFiles {
		governed[path] = true
	}
	for _, source := range graph.Sources {
		owner, err := sourceModuleOwner(repo, source.Path)
		if err != nil {
			part.findings = append(part.findings, policy.Finding{Check: "architecture.importCoverage", Path: source.Path, Subject: "rust", Message: err.Error()})
			part.incomplete = true
			continue
		}
		part.nodes = append(part.nodes, sourcegraph.Node{Path: source.Path, Language: "rust", Generated: repo.IsGenerated(source.Path), Test: repo.IsTest(source.Path), Root: source.Root, Module: owner, Resolution: "file:" + source.Path})
	}
	seen := map[sourcegraph.Edge]bool{}
	for _, imported := range graph.Imports {
		kind := sourcegraph.EdgeRuntime
		if imported.ReExport {
			kind = sourcegraph.EdgeReExport
		}
		edge := sourcegraph.Edge{Source: imported.Source, Target: imported.Target, SourceResolution: "file:" + imported.Source, TargetResolution: "file:" + imported.Target, Line: imported.Line, Column: imported.Column, Ecosystem: "rust", Kind: kind}
		if seen[edge] {
			continue
		}
		seen[edge] = true
		part.edges = append(part.edges, edge)
		if finding, found := importModuleFinding(repo, governed, imported.Source, imported.Target, imported.Line); found {
			finding.Line, finding.Column = imported.Line, imported.Column
			part.findings = append(part.findings, finding)
		}
	}
	if !part.incomplete {
		inputs, err := rustGraphInputs(repo, part, sourcePlan, sources, discovery)
		if err != nil {
			part.incomplete = true
			part.findings = append(part.findings, sourceGraphCoverageFinding(err.Error()))
		} else {
			part.inputs = inputs
		}
	}
	return part
}

func rustGraphSelection(repo repository.Repository, files []string) []string {
	selected := []string{}
	for _, path := range files {
		if repo.Language(path) == "rust" && repo.NativeAnalysis(path, "architecture") {
			selected = append(selected, path)
		}
	}
	return selected
}

func RustGraphCommands(repo repository.Repository, files []string) []policy.Command {
	files = rustGraphSelection(repo, files)
	if len(files) == 0 {
		return nil
	}
	allFiles, err := repo.AllFiles()
	if err != nil {
		return nil
	}
	commands := rustGraphDiscovery(repo, files, allFiles).Commands
	return append(commands, rustGraphSources(repo, allFiles).Commands...)
}

func rustGraphDiscovery(repo repository.Repository, files, allFiles []string) rustfacts.DiscoveryPlan {
	plan := rustfacts.PlanDiscovery(repo, files, allFiles)
	for index := range plan.Commands {
		plan.Commands[index].Name += "-architecture"
	}
	return plan
}

func rustGraphSources(repo repository.Repository, allFiles []string) rustfacts.SourcePlan {
	plan := rustfacts.PlanSources(repo, allFiles)
	for index := range plan.Commands {
		plan.Commands[index].Name += "-architecture"
	}
	return plan
}
