package architecture

import (
	"fmt"
	"strings"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonObjectImportCoverage(repo repository.Repository, project repository.PythonProject, sources []string, dependencies map[string]map[string]bool, coverage map[string]string, facts map[string]pythonSourceFact) {
	index := newPythonModuleIndex(project)
	for _, source := range sources {
		if coverage[source] == "" {
			coverage[source] = pythonObjectSourceCoverage(repo, project, index, source, dependencies[source], facts[source])
		}
	}
}

func pythonObjectDeclarations(repo repository.Repository, project, source string) []policy.PythonComputedImport {
	result := []policy.PythonComputedImport{}
	for _, declaration := range repo.Config.Scope.PythonComputedImports {
		if declaration.Project == project && declaration.Importer == source && declaration.Callee == "pkgutil.resolve_name" {
			result = append(result, declaration)
		}
	}
	return result
}

func pythonObjectSourceCoverage(repo repository.Repository, project repository.PythonProject, index pythonModuleIndex, source string, dependencies map[string]bool, facts pythonSourceFact) string {
	declarations := pythonObjectDeclarations(repo, project.Manifest, source)
	matched := map[int]bool{}
	for _, imported := range facts.ObjectImports {
		if facts.ExternalImports[imported.Site] {
			continue
		}
		if imported.Error != "" {
			return fmt.Sprintf("line %d object import is unproven: %s", imported.Site.Line, imported.Error)
		}
		if imported.Guard != nil {
			return fmt.Sprintf("line %d guarded object input requires external composition evidence", imported.Site.Line)
		}
		if imported.Registry != nil {
			position, message := pythonObjectDeclarationPosition(project, source, facts, imported, declarations)
			if message != "" {
				return message
			}
			matched[position] = true
		}
		if message := addPythonObjectDependencies(repo, index, imported, dependencies); message != "" {
			return message
		}
	}
	for position, declaration := range declarations {
		if !matched[position] {
			return fmt.Sprintf("line %d column %d scope.pythonComputedImports object-loader declaration is stale", declaration.Line, declaration.Column)
		}
	}
	return ""
}

func pythonObjectDeclarationPosition(project repository.PythonProject, source string, facts pythonSourceFact, imported pythonfacts.ObjectImport, declarations []policy.PythonComputedImport) (int, string) {
	fact := pythonComputedImportFact{Callee: "pkgutil.resolve_name", Callable: imported.Callable, Line: imported.Site.Line, Column: imported.Site.Column,
		EndLine: imported.EndSite.Line, EndColumn: imported.EndSite.Column, Argument: imported.Argument, Shape: "module-object-call/v1"}
	position, message := pythonComputedDeclarationPosition(declarations, fact)
	if message != "" {
		return -1, message
	}
	declaration := declarations[position]
	if message := pythonComputedIdentityMessage(project, source, facts, fact, declaration); message != "" {
		return -1, message
	}
	if message := pythonObjectRegistryMessage(imported, declaration); message != "" {
		return -1, message
	}
	return position, ""
}

func pythonObjectRegistryMessage(imported pythonfacts.ObjectImport, declaration policy.PythonComputedImport) string {
	if declaration.EntryPointGroup != "" || len(declaration.Targets) != 0 || len(declaration.Configuration) != 1 || !strings.Contains(declaration.Namespace, ".") {
		return "object import requires one exact governed registry and a bounded namespace without a duplicate target inventory"
	}
	input := declaration.Configuration[0]
	if input.Path != imported.Registry.Path || input.JSONPointer != imported.Registry.JSONPointer || input.SHA256 != imported.Registry.SHA256 {
		return fmt.Sprintf("line %d object import registry input, selector, or digest is stale", imported.Site.Line)
	}
	for _, target := range imported.Targets {
		if target.Module != declaration.Namespace && !strings.HasPrefix(target.Module, declaration.Namespace+".") {
			return fmt.Sprintf("line %d object import target %q escapes namespace %q", imported.Site.Line, target.Module, declaration.Namespace)
		}
	}
	return ""
}

func pythonObjectTargetPaths(index pythonModuleIndex, module string) []string {
	result := []string{}
	for _, path := range index.files[module] {
		if strings.HasSuffix(path, ".py") {
			result = append(result, path)
		}
	}
	return result
}

func addPythonObjectDependencies(repo repository.Repository, index pythonModuleIndex, imported pythonfacts.ObjectImport, dependencies map[string]bool) string {
	for _, target := range imported.Targets {
		if !pythonLocalModule(index, target.Module) && imported.Registry == nil {
			continue
		}
		paths := pythonObjectTargetPaths(index, target.Module)
		if len(paths) != 1 || pythonAmbiguousImportModule(index, target.Module) {
			return fmt.Sprintf("line %d object import target %q does not resolve to one governed runtime module", imported.Site.Line, target.Module)
		}
		if _, err := sourceModuleOwner(repo, paths[0]); err != nil {
			return fmt.Sprintf("line %d object import target %q has invalid module ownership: %v", imported.Site.Line, target.Module, err)
		}
		dependencies[paths[0]] = true
	}
	return ""
}

func pythonObjectGraphEdges(index pythonModuleIndex, source string, imports []pythonfacts.ObjectImport, dependencies map[string]bool, covered map[string]bool, seen map[string]bool) []sourcegraph.Edge {
	edges := []sourcegraph.Edge{}
	for _, imported := range imports {
		for _, target := range imported.Targets {
			for _, path := range pythonObjectTargetPaths(index, target.Module) {
				if !dependencies[path] {
					continue
				}
				edge := pythonGraphEdge(source, path, imported.Site.Line, imported.Site.Column, "proven-dynamic")
				identity := pythonEdgeIdentity(edge)
				if !seen[identity] {
					seen[identity] = true
					edges = append(edges, edge)
				}
				covered[path] = true
			}
		}
	}
	return edges
}
