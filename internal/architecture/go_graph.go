package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func goSourceGraph(repo repository.Repository, selected, allFiles []string, inventory repository.GoModuleInventory) sourceGraphPart {
	modules := inventory.Modules
	scope := newGoGraphScope(repo, selected, allFiles, modules)
	part := sourceGraphPart{}
	unitByImport := map[string]string{}
	nodeByPath := map[string]sourcegraph.Node{}
	files := map[string]*ast.File{}
	positions := token.NewFileSet()
	packages := map[string]string{}
	for index := 0; index < len(scope.queue); index++ {
		path := scope.queue[index]
		node, importPath, parsed, err := readGoGraphSource(repo, path, modules, positions)
		if err == nil {
			node, err = resolveGoGraphPackage(node, importPath, parsed.Name.Name, packages, unitByImport)
		}
		if err != nil {
			part.findings = append(part.findings, goGraphCoverage(path, err.Error()))
			part.incomplete = true
			continue
		}
		files[path] = parsed
		scope.includeImports(parsed)
		part.nodes = append(part.nodes, node)
		nodeByPath[path] = node
	}
	for _, problem := range inventory.Problems {
		if scope.relevantProblem(problem) {
			part.findings = append(part.findings, goGraphCoverage(problem.Path, problem.Message))
			part.incomplete = true
		}
	}
	for _, node := range part.nodes {
		edges, edgeFindings, edgeIncomplete := goFileGraphEdges(node, modules, unitByImport, files[node.Path], positions)
		part.edges = append(part.edges, edges...)
		part.findings = append(part.findings, edgeFindings...)
		part.incomplete = part.incomplete || edgeIncomplete
	}
	part.findings = append(part.findings, goGraphModuleFindings(repo, selected, nodeByPath, part.nodes, part.edges)...)
	return part
}

func readGoGraphSource(repo repository.Repository, path string, modules []repository.GoModule, positions *token.FileSet) (sourcegraph.Node, string, *ast.File, error) {
	module, found := repo.OwningGoModule(path, modules)
	if !found {
		return sourcegraph.Node{}, "", nil, fmt.Errorf("source has no owning Go module")
	}
	owner, err := sourceModuleOwner(repo, path)
	if err != nil {
		return sourcegraph.Node{}, "", nil, err
	}
	importPath, unit, ok := goPackageIdentity(module, path)
	if !ok {
		return sourcegraph.Node{}, "", nil, fmt.Errorf("source has no contained Go package identity")
	}
	data, err := repo.Read(path)
	if err != nil {
		return sourcegraph.Node{}, "", nil, fmt.Errorf("read Go source: %w", err)
	}
	parsed, err := parser.ParseFile(positions, path, data, 0)
	if err != nil {
		return sourcegraph.Node{}, "", nil, fmt.Errorf("parse Go source: %w", err)
	}
	node := sourcegraph.Node{
		Path: path, Language: "go", Generated: repo.IsGenerated(path), Test: repo.IsTest(path),
		Root: module.Root, Module: owner, Resolution: unit,
	}
	return node, importPath, parsed, nil
}

func resolveGoGraphPackage(node sourcegraph.Node, importPath, packageName string, packages, unitByImport map[string]string) (sourcegraph.Node, error) {
	externalTest := node.Test && strings.HasSuffix(packageName, "_test")
	if externalTest {
		packageName = strings.TrimSuffix(packageName, "_test")
	}
	if previous, exists := packages[node.Resolution]; exists && previous != packageName {
		return sourcegraph.Node{}, fmt.Errorf("directory contains ambiguous Go package declarations")
	}
	packages[node.Resolution] = packageName
	if previous, duplicate := unitByImport[importPath]; duplicate && previous != node.Resolution {
		return sourcegraph.Node{}, fmt.Errorf("package import identity is ambiguous")
	}
	if !node.Test {
		unitByImport[importPath] = node.Resolution
	}
	if externalTest {
		node.Resolution += ":external-test"
	}
	return node, nil
}

func goPackageIdentity(module repository.GoModule, path string) (string, string, bool) {
	directory := filepath.ToSlash(filepath.Dir(path))
	relative := directory
	if module.Root != "." {
		var err error
		relative, err = filepath.Rel(filepath.FromSlash(module.Root), filepath.FromSlash(directory))
		if err != nil {
			return "", "", false
		}
		relative = filepath.ToSlash(relative)
	}
	if relative == ".." || strings.HasPrefix(relative, "../") {
		return "", "", false
	}
	importPath := module.Name
	if relative != "." && relative != "" {
		importPath += "/" + relative
	}
	if importPath == "" {
		return "", "", false
	}
	return importPath, "go-package:" + module.Manifest + ":" + importPath, true
}

func goFileGraphEdges(
	node sourcegraph.Node,
	modules []repository.GoModule,
	unitByImport map[string]string,
	parsed *ast.File,
	set *token.FileSet,
) ([]sourcegraph.Edge, []policy.Finding, bool) {
	edges := []sourcegraph.Edge{}
	findings := []policy.Finding{}
	incomplete := false
	for _, imported := range parsed.Imports {
		raw, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			findings = append(findings, goGraphCoverage(node.Path, "Go import path cannot be decoded"))
			incomplete = true
			continue
		}
		_, internal := internalGoDirectory(modules, raw)
		if !internal {
			continue
		}
		targetUnit, resolved := unitByImport[raw]
		if !resolved {
			findings = append(findings, goGraphCoverage(node.Path, fmt.Sprintf("local Go import %q has no governed target package", raw)))
			incomplete = true
			continue
		}
		position := set.Position(imported.Pos())
		edges = append(edges, sourcegraph.Edge{
			Source: node.Path, Target: raw, SourceResolution: node.Resolution, TargetResolution: targetUnit,
			Line: position.Line, Column: position.Column, Ecosystem: "go", Kind: sourcegraph.EdgeRuntime,
		})
	}
	return edges, findings, incomplete
}

func goGraphModuleFindings(
	repo repository.Repository,
	selected []string,
	nodeByPath map[string]sourcegraph.Node,
	nodes []sourcegraph.Node,
	edges []sourcegraph.Edge,
) []policy.Finding {
	selectedSet := make(map[string]bool, len(selected))
	for _, path := range selected {
		selectedSet[path] = true
	}
	moduleByUnit := map[string]string{}
	for _, node := range nodes {
		if node.Test {
			continue
		}
		if previous, found := moduleByUnit[node.Resolution]; !found || node.Module < previous {
			moduleByUnit[node.Resolution] = node.Module
		}
	}
	findings := []policy.Finding{}
	for _, edge := range edges {
		source := nodeByPath[edge.Source]
		targetModule := moduleByUnit[edge.TargetResolution]
		if !selectedSet[edge.Source] || source.Test || source.Module == targetModule {
			continue
		}
		declared := repo.Config.Modules[repo.Config.ModuleByName[source.Module]]
		if slices.Contains(declared.DependsOn, targetModule) {
			continue
		}
		findings = append(findings, policy.Finding{
			Check: "architecture.moduleDependency", Path: edge.Source, Line: edge.Line, Column: edge.Column,
			Subject: targetModule,
			Message: fmt.Sprintf("line %d module %q imports module %q without declaring dependsOn", edge.Line, source.Module, targetModule),
		})
	}
	sort.Slice(findings, func(left, right int) bool {
		return findings[left].Path+"\x00"+findings[left].Subject+"\x00"+findings[left].Message <
			findings[right].Path+"\x00"+findings[right].Subject+"\x00"+findings[right].Message
	})
	return findings
}

func goGraphCoverage(path, message string) policy.Finding {
	return policy.Finding{Check: "architecture.importCoverage", Path: path, Subject: "go", Message: message}
}
