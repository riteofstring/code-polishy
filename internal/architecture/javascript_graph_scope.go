package architecture

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type javascriptGraphScope struct {
	repo     repository.Repository
	roots    []string
	files    map[string][]string
	governed map[string]bool
	projects map[string]bool
	queue    []string
}

func newJavaScriptGraphScope(repo repository.Repository, selected, allFiles []string) *javascriptGraphScope {
	scope := &javascriptGraphScope{repo: repo, files: map[string][]string{}, governed: map[string]bool{}, projects: map[string]bool{}}
	for _, path := range allFiles {
		scope.governed[path] = true
		if javascriptProjectControl(path) {
			scope.roots = append(scope.roots, filepath.ToSlash(filepath.Dir(path)))
		}
	}
	sortProjectRoots(scope.roots)
	scope.roots = slices.Compact(scope.roots)
	for _, path := range allFiles {
		if repo.Language(path) == "typescript" {
			root := scope.project(path)
			scope.files[root] = append(scope.files[root], path)
		}
	}
	for _, path := range selected {
		scope.selectPath(path, allFiles)
	}
	return scope
}

func (scope *javascriptGraphScope) selectPath(path string, allFiles []string) {
	if filepath.Base(path) == nodeManifestName {
		roots := javascriptPackageRoots(allFiles)
		owner := filepath.ToSlash(filepath.Dir(path))
		for _, source := range allFiles {
			if scope.repo.Language(source) == "typescript" && javascriptPackageRoot(scope.repo.JavaScriptContextPath(source), roots) == owner {
				scope.includePath(source)
			}
		}
		return
	}
	if scope.repo.Language(path) == "typescript" || javascriptProjectControl(path) {
		scope.includePath(path)
	}
}

func javascriptProjectControl(path string) bool {
	name := filepath.Base(path)
	return name == nodeManifestName || strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json")
}

func (scope *javascriptGraphScope) project(path string) string {
	return javascriptPackageRoot(scope.repo.JavaScriptContextPath(path), scope.roots)
}

func (scope *javascriptGraphScope) includePath(path string) {
	project := scope.project(path)
	if scope.projects[project] {
		return
	}
	scope.projects[project] = true
	scope.queue = append(scope.queue, scope.files[project]...)
}

func (scope *javascriptGraphScope) includeImports(facts []javascript.ImportFact) {
	for _, fact := range facts {
		if scope.governed[fact.Resolved] && scope.repo.Language(fact.Resolved) == "typescript" {
			scope.includePath(fact.Resolved)
		}
	}
}
