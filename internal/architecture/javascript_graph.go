package architecture

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const javascriptImportBatchSize = 4096

func javascriptSourceGraph(ctx context.Context, repo repository.Repository, selected, allFiles []string) sourceGraphPart {
	scope := newJavaScriptGraphScope(repo, selected, allFiles)
	part := sourceGraphPart{}
	bounded, cancel := context.WithTimeout(ctx, javascriptImportBudget)
	defer cancel()
	facts := []javascript.ImportFact{}
	roots := javascriptPackageRoots(allFiles)
	for start := 0; start < len(scope.queue); {
		if len(scope.queue) > sourcegraph.MaximumNodes {
			part.findings = append(part.findings, sourceGraphCoverageFinding(fmt.Sprintf(
				"JavaScript source count exceeds the %d node limit", sourcegraph.MaximumNodes)))
			part.incomplete = true
			return part
		}
		end := min(start+javascriptImportBatchSize, len(scope.queue))
		batch, imports := readJavaScriptGraphBatch(bounded, repo, scope.queue[start:end], roots)
		part.nodes = append(part.nodes, batch.nodes...)
		part.findings = append(part.findings, batch.findings...)
		part.incomplete = part.incomplete || batch.incomplete
		if part.incomplete {
			return part
		}
		facts = append(facts, imports...)
		scope.includeImports(imports)
		start = end
	}
	part = javascriptGraphEdges(repo, allFiles, facts, part)
	governed := make(map[string]bool, len(allFiles))
	for _, path := range allFiles {
		governed[path] = true
	}
	packages := newNodePackages(repo, allFiles)
	part.findings = append(part.findings, javascriptFactFindings(repo, packages, governed, facts)...)
	part.findings = append(part.findings, packages.coverage...)
	for _, finding := range part.findings {
		if finding.Check == "architecture.importCoverage" {
			part.incomplete = true
		}
	}
	return part
}

func readJavaScriptGraphBatch(ctx context.Context, repo repository.Repository, paths, roots []string) (sourceGraphPart, []javascript.ImportFact) {
	part, readable := javascriptGraphNodes(repo, paths, roots)
	if len(readable) == 0 || part.incomplete {
		return part, nil
	}
	result, err := (javascript.Bundle{PolicyRoot: repo.PolicyRoot}).Imports(ctx, repo.Root, readable)
	if err != nil {
		part.findings = append(part.findings, policy.Finding{
			Check: "policy.tool", Path: "repository", Subject: "javascript-bundle", Message: err.Error(),
		})
		part.incomplete = true
		return part, nil
	}
	for _, entry := range result.Unsupported {
		part.findings = append(part.findings, importCoverageFinding(entry.Path,
			"the policy-owned import reader could not completely resolve this source: "+entry.Reason))
		part.incomplete = true
	}
	return part, result.Imports
}

func javascriptGraphNodes(repo repository.Repository, allFiles, roots []string) (sourceGraphPart, []string) {
	part := sourceGraphPart{}
	readable := []string{}
	for _, path := range allFiles {
		if repo.Language(path) != "typescript" {
			continue
		}
		if !javascriptSourceExtensions[strings.ToLower(filepath.Ext(path))] {
			part.findings = append(part.findings, importCoverageFinding(path,
				"the policy-owned import reader does not read this executable source"))
			part.incomplete = true
			continue
		}
		owner, err := sourceModuleOwner(repo, path)
		if err != nil {
			part.findings = append(part.findings, importCoverageFinding(path,
				"JavaScript or TypeScript source must belong to exactly one repository module"))
			part.incomplete = true
			continue
		}
		part.nodes = append(part.nodes, sourcegraph.Node{
			Path: path, Language: "typescript", Generated: repo.IsGenerated(path), Test: repo.IsTest(path),
			Root: javascriptPackageRoot(path, roots), Module: owner, Resolution: "file:" + path,
		})
		readable = append(readable, path)
	}
	if len(readable) > sourcegraph.MaximumNodes {
		part.findings = append(part.findings, sourceGraphCoverageFinding(fmt.Sprintf(
			"JavaScript source count exceeds the %d node limit", sourcegraph.MaximumNodes)))
		part.incomplete = true
		return part, nil
	}
	return part, readable
}

func javascriptPackageRoots(allFiles []string) []string {
	roots := []string{}
	for _, path := range allFiles {
		if filepath.Base(path) == nodeManifestName {
			roots = append(roots, filepath.ToSlash(filepath.Dir(path)))
		}
	}
	sort.Slice(roots, func(left, right int) bool {
		if len(roots[left]) == len(roots[right]) {
			return roots[left] < roots[right]
		}
		return len(roots[left]) > len(roots[right])
	})
	return roots
}

func javascriptPackageRoot(path string, roots []string) string {
	if root := containingProjectRoot(path, roots); root != "" {
		return root
	}
	return "."
}

func containingProjectRoot(path string, roots []string) string {
	for _, root := range roots {
		if root == "." || strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/") {
			return root
		}
	}
	return ""
}

func javascriptGraphEdges(
	repo repository.Repository,
	allFiles []string,
	facts []javascript.ImportFact,
	part sourceGraphPart,
) sourceGraphPart {
	nodes := make(map[string]sourcegraph.Node, len(part.nodes))
	for _, node := range part.nodes {
		nodes[node.Path] = node
	}
	governed := make(map[string]bool, len(allFiles))
	for _, path := range allFiles {
		governed[path] = true
	}
	for _, fact := range facts {
		source, found := nodes[fact.Path]
		if !found {
			part.findings = append(part.findings, importCoverageFinding(fact.Path, "the import reader reported a source absent from the canonical graph"))
			part.incomplete = true
			continue
		}
		if fact.Resolved == "" {
			if javascriptLocalSpecifier(fact.Specifier) {
				part.findings = append(part.findings, importCoverageFinding(fact.Path,
					fmt.Sprintf("line %d local import %q could not be resolved", fact.Line, fact.Specifier)))
				part.incomplete = true
			}
			continue
		}
		target, local := nodes[fact.Resolved]
		if !local {
			if javascriptMissingGraphTarget(repo, governed, fact) {
				part.findings = append(part.findings, importCoverageFinding(fact.Path,
					fmt.Sprintf("line %d import %q resolves outside the canonical executable graph", fact.Line, fact.Specifier)))
				part.incomplete = true
			}
			continue
		}
		part.edges = append(part.edges, sourcegraph.Edge{
			Source: fact.Path, Target: target.Path, SourceResolution: source.Resolution,
			TargetResolution: target.Resolution, Line: fact.Line, Column: fact.Column,
			Ecosystem: "javascript", Kind: sourcegraph.EdgeKind(fact.Kind),
		})
	}
	return part
}

func javascriptMissingGraphTarget(repo repository.Repository, governed map[string]bool, fact javascript.ImportFact) bool {
	return governed[fact.Resolved] && repo.IsExecutableSource(fact.Resolved) ||
		!installedPackage(fact.Resolved) && javascriptLocalSpecifier(fact.Specifier)
}

func javascriptLocalSpecifier(specifier string) bool {
	return strings.HasPrefix(specifier, ".") || strings.HasPrefix(specifier, "/") || strings.HasPrefix(specifier, "#")
}
