package architecture

import (
	"go/ast"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/riteofstring/code-polishy/internal/repository"
	"golang.org/x/mod/modfile"
)

type goGraphScope struct {
	repo       repository.Repository
	modules    []repository.GoModule
	roots      []string
	workspaces []string
	files      map[string][]string
	controls   map[string]bool
	queue      []string
}

func newGoGraphScope(repo repository.Repository, selected, allFiles []string, modules []repository.GoModule) *goGraphScope {
	scope := &goGraphScope{repo: repo, modules: modules, files: map[string][]string{}, controls: map[string]bool{}}
	for _, path := range allFiles {
		switch filepath.Base(path) {
		case "go.mod":
			scope.roots = append(scope.roots, filepath.ToSlash(filepath.Dir(path)))
		case "go.work":
			scope.workspaces = append(scope.workspaces, filepath.ToSlash(filepath.Dir(path)))
		}
	}
	sortProjectRoots(scope.roots)
	sortProjectRoots(scope.workspaces)
	for _, path := range allFiles {
		if repo.Language(path) == "go" {
			manifest := scope.manifest(path)
			scope.files[manifest] = append(scope.files[manifest], path)
		}
	}
	for _, path := range selected {
		scope.selectPath(path)
	}
	return scope
}

func sortProjectRoots(roots []string) {
	sort.Slice(roots, func(left, right int) bool {
		if len(roots[left]) == len(roots[right]) {
			return roots[left] < roots[right]
		}
		return len(roots[left]) > len(roots[right])
	})
}

func (scope *goGraphScope) manifest(path string) string {
	root := containingProjectRoot(path, scope.roots)
	if root == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join(root, "go.mod"))
}

func (scope *goGraphScope) selectPath(path string) {
	switch {
	case filepath.Base(path) == "go.work":
		scope.includeWorkspace(path)
	case filepath.Base(path) == "go.mod":
		scope.includeManifest(path)
	case scope.repo.Language(path) == "go":
		manifest := scope.manifest(path)
		if manifest == "" {
			scope.queue = append(scope.queue, path)
			return
		}
		scope.includeManifest(manifest)
	}
}

func (scope *goGraphScope) includeManifest(manifest string) {
	if scope.controls[manifest] {
		return
	}
	scope.controls[manifest] = true
	if root := containingProjectRoot(manifest, scope.workspaces); root != "" {
		scope.controls[filepath.ToSlash(filepath.Join(root, "go.work"))] = true
	}
	for _, module := range scope.modules {
		if module.Manifest == manifest {
			scope.queue = append(scope.queue, scope.files[manifest]...)
			return
		}
	}
}

func (scope *goGraphScope) includeWorkspace(path string) {
	scope.controls[path] = true
	data, err := scope.repo.Read(path)
	if err != nil {
		return
	}
	workspace, err := modfile.ParseWork(path, data, nil)
	if err != nil {
		return
	}
	for _, use := range workspace.Use {
		root := filepath.FromSlash(use.Path)
		if !filepath.IsAbs(root) {
			root = filepath.Join(scope.repo.Root, filepath.Dir(filepath.FromSlash(path)), root)
		}
		manifest, err := scope.repo.NormalizePath(filepath.Join(root, "go.mod"))
		if err == nil {
			scope.includeManifest(manifest)
		}
	}
}

func (scope *goGraphScope) includeImports(file *ast.File) {
	for _, imported := range file.Imports {
		name, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			continue
		}
		directory, internal := internalGoDirectory(scope.modules, name)
		if internal {
			scope.includeManifest(scope.manifest(filepath.ToSlash(filepath.Join(directory, "source.go"))))
		}
	}
}

func (scope *goGraphScope) relevantProblem(problem repository.GoModuleProblem) bool {
	if scope.controls[problem.Path] {
		return true
	}
	names := map[string]bool{}
	for _, module := range scope.modules {
		if scope.controls[module.Manifest] {
			names[module.Name] = true
		}
	}
	for _, module := range scope.modules {
		if module.Manifest == problem.Path && names[module.Name] {
			return true
		}
	}
	return false
}
