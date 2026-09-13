package architecture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func providerSourceGraph(ctx context.Context, repo repository.Repository, selected, allFiles []string, boundary runner.Runner) sourceGraphPart {
	part := sourceGraphPart{}
	if len(selected) == 0 {
		return part
	}
	part = providerArchitectureCoverage(repo, selected)
	for _, operation := range providerOperations(repo, selected, allFiles) {
		result := pack.RunAdapter(ctx, repo, operation.selection, operation.command, boundary, repo.AnalysisProfile())
		part = mergeSourceGraphParts(part, providerResultGraph(repo, operation.command, result))
	}
	return part
}

type providerOperation struct {
	command   policy.Command
	selection repository.Selection
}

func providerOperations(repo repository.Repository, selected, allFiles []string) []providerOperation {
	operations := []providerOperation{}
	if len(selected) == 0 {
		return operations
	}
	for _, command := range repo.Config.Checks {
		if command.Adapter == nil || command.Adapter.Capability != "architecture" {
			continue
		}
		selection := repository.Selection{Files: selected, All: len(selected) == len(allFiles) && slices.Equal(selected, allFiles)}
		if len(pack.SelectedFiles(repo, selection, command, repo.AnalysisProfile())) > 0 {
			operations = append(operations, providerOperation{command: command, selection: selection})
		}
	}
	return operations
}

func ProviderCommands(repo repository.Repository, selected []string) []policy.Command {
	allFiles, err := repo.AllFiles()
	if err != nil {
		return nil
	}
	commands := []policy.Command{}
	for _, operation := range providerOperations(repo, selected, allFiles) {
		prepared, selected, err := pack.PlannedExecution(repo, operation.selection, operation.command, repo.AnalysisProfile())
		if !selected {
			continue
		}
		if err != nil {
			prepared = operation.command
		}
		commands = append(commands, prepared)
	}
	return commands
}

func providerArchitectureCoverage(repo repository.Repository, selected []string) sourceGraphPart {
	part := sourceGraphPart{}
	for _, path := range selected {
		if !repo.IsExecutableSource(path) || repo.Language(path) == "" || repo.Language(path) == "shell" {
			continue
		}
		owner := repo.AnalysisOwner(path, "architecture", "")
		if owner.Problem != "" {
			part.incomplete = true
			part.findings = append(part.findings, policy.Finding{Check: "architecture.importCoverage", Path: path, Subject: "provider", Message: owner.Problem})
		}
	}
	return part
}

func providerResultGraph(repo repository.Repository, command policy.Command, result pack.Result) sourceGraphPart {
	part := sourceGraphPart{findings: result.Findings}
	if result.Digest == "" || result.Response.Coverage == nil || len(result.Response.Coverage.Unsupported) > 0 {
		part.incomplete = true
		return part
	}
	if result.Response.Facts == nil || result.Response.Facts.Imports == nil {
		part.incomplete = true
		part.findings = append(part.findings, sourceGraphCoverageFinding("provider did not supply import facts"))
		return part
	}
	for _, path := range result.Response.Coverage.Analyzed {
		owner, err := sourceModuleOwner(repo, path)
		if err != nil {
			part.incomplete = true
			part.findings = append(part.findings, sourceGraphCoverageFinding(err.Error()))
			continue
		}
		part.nodes = append(part.nodes, sourcegraph.Node{Path: path, Language: repo.Language(path), Generated: repo.IsGenerated(path), Test: repo.IsTest(path), Root: providerSourceRoot(result.Request, path), Module: owner, Resolution: "file:" + path})
	}
	part.imports = slices.Clone(*result.Response.Facts.Imports)
	for _, nodes := range providerNodeGroups(part.nodes) {
		input, err := providerFactInput(command, result, nodes)
		if err != nil {
			part.incomplete = true
			part.findings = append(part.findings, sourceGraphCoverageFinding(err.Error()))
			return part
		}
		part.inputs = append(part.inputs, input)
	}
	return part
}

func providerFactInput(command policy.Command, result pack.Result, nodes []sourcegraph.Node) (sourcegraph.FactInput, error) {
	input := sourcegraph.FactInput{Analyzer: "pack", Protocol: "code-polishy-pack/v3", Project: "pack/" + command.Adapter.PackName + "/" + command.Name, Root: ".", Paths: slices.Clone(result.Response.Coverage.Analyzed), FactsSHA256: result.Digest}
	input.Root = nodes[0].Root
	rootDigest := sha256.Sum256([]byte(input.Root))
	input.Project += "/" + hex.EncodeToString(rootDigest[:])
	input.Paths = []string{}
	for _, node := range nodes {
		input.Paths = append(input.Paths, node.Path)
	}
	provider := &sourcegraph.ProviderInput{Name: command.Adapter.PackName, Version: command.Adapter.PackVersion, Digest: command.Adapter.PackDigest}
	for _, language := range command.Adapter.Languages {
		provider.Languages = append(provider.Languages, language.Name)
	}
	for _, item := range []struct {
		destination *string
		value       any
	}{
		{&input.PartitionsSHA256, nodes}, {&input.ResolutionSHA256, result.Response.Facts.Imports}, {&provider.InputsSHA256, result.Response.Inputs}, {&provider.PolicySHA256, result.Request.Policy},
	} {
		data, err := json.Marshal(item.value)
		if err != nil {
			return sourcegraph.FactInput{}, err
		}
		digest := sha256.Sum256(data)
		*item.destination = hex.EncodeToString(digest[:])
	}
	if result.Request.Runtime != nil {
		provider.RuntimeSHA256 = result.Request.Runtime.SHA256
	}
	input.Provider = provider
	return input, nil
}

func connectProviderImports(repo repository.Repository, allFiles []string, part *sourceGraphPart) {
	nodes := map[string]sourcegraph.Node{}
	for _, node := range part.nodes {
		nodes[node.Path] = node
	}
	governed := map[string]bool{}
	for _, path := range allFiles {
		governed[path] = true
	}
	packages := newNodePackages(repo, allFiles)
	for _, fact := range part.imports {
		connection := connectProviderImport(repo, packages, governed, nodes, fact)
		part.findings = append(part.findings, connection.findings...)
		part.edges = append(part.edges, connection.edges...)
		part.incomplete = part.incomplete || connection.incomplete
	}
	part.findings = append(part.findings, packages.coverage...)
}

func connectProviderImport(repo repository.Repository, packages *nodePackages, governed map[string]bool, nodes map[string]sourcegraph.Node, fact pack.ImportFact) sourceGraphPart {
	part := sourceGraphPart{findings: providerImportChecks(repo, packages, governed, fact)}
	target, problem := providerImportTarget(repo, governed, nodes, fact)
	if problem != "" {
		providerGraphProblem(&part, fact, problem)
		return part
	}
	if target == nil {
		return part
	}
	source, found := nodes[fact.Path]
	if !found {
		providerGraphProblem(&part, fact, "source has no verified analysis")
		return part
	}
	ecosystem := source.Language
	if ecosystem == "typescript" {
		ecosystem = "javascript"
	}
	part.edges = []sourcegraph.Edge{{Source: source.Path, Target: target.Path, SourceResolution: source.Resolution, TargetResolution: target.Resolution, Line: fact.Line, Column: fact.Column, Ecosystem: ecosystem, Kind: sourcegraph.EdgeKind(fact.Kind)}}
	return part
}

func providerImportChecks(repo repository.Repository, packages *nodePackages, governed map[string]bool, fact pack.ImportFact) []policy.Finding {
	findings := []policy.Finding{}
	if finding, found := importModuleFinding(repo, governed, fact.Path, fact.Resolved, fact.Line); found {
		findings = append(findings, finding)
	}
	if repo.Language(fact.Path) == "typescript" {
		if finding, found := nodePackageFinding(repo, packages, fact.Path, fact.Resolved, fact.Package, fact.Line); found {
			findings = append(findings, finding)
		}
	}
	return findings
}

func providerImportTarget(repo repository.Repository, governed map[string]bool, nodes map[string]sourcegraph.Node, fact pack.ImportFact) (*sourcegraph.Node, string) {
	if fact.Resolved == "" {
		if !providerPackageReference(fact) {
			return nil, "import could not be resolved"
		}
		return nil, ""
	}
	if target, found := nodes[fact.Resolved]; found {
		return &target, ""
	}
	if governed[fact.Resolved] && !repo.IsExecutableSource(fact.Resolved) {
		return nil, ""
	}
	if !governed[fact.Resolved] && providerPackageReference(fact) {
		return nil, ""
	}
	return nil, "resolved executable target has no verified analysis"
}

func providerPackageReference(fact pack.ImportFact) bool {
	return fact.Package != "" && !strings.HasPrefix(fact.Specifier, ".") && !strings.HasPrefix(fact.Specifier, "/")
}

func providerGraphProblem(part *sourceGraphPart, fact pack.ImportFact, reason string) {
	part.incomplete = true
	part.findings = append(part.findings, policy.Finding{Check: "architecture.importCoverage", Path: fact.Path, Line: fact.Line, Column: fact.Column, Subject: fact.Specifier, Message: fmt.Sprintf("%s: %s", fact.Specifier, reason)})
}

func providerSourceRoot(request pack.Request, file string) string {
	for _, unit := range request.Units {
		if slices.Contains(unit.Members, file) {
			if unit.PackageRoot == "." || strings.HasPrefix(file, unit.PackageRoot+"/") {
				return unit.PackageRoot
			}
			return "."
		}
	}
	return "."
}

func providerNodeGroups(nodes []sourcegraph.Node) [][]sourcegraph.Node {
	groups := map[string][]sourcegraph.Node{}
	roots := []string{}
	for _, node := range nodes {
		if groups[node.Root] == nil {
			roots = append(roots, node.Root)
		}
		groups[node.Root] = append(groups[node.Root], node)
	}
	slices.Sort(roots)
	result := [][]sourcegraph.Node{}
	for _, root := range roots {
		result = append(result, groups[root])
	}
	return result
}
