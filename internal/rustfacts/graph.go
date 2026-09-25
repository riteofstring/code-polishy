package rustfacts

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type ModuleSource struct {
	Path string
	Root string
}

type ModuleImport struct {
	Source   string
	Target   string
	Line     int
	Column   int
	ReExport bool
}

type ModuleGraph struct {
	Sources  []ModuleSource
	Imports  []ModuleImport
	Findings []policy.Finding
}

type rustModule struct {
	crate     *rustCrate
	parent    *rustModule
	file      string
	directory string
	inline    bool
	lexical   bool
	noPrelude bool
	bindings  map[string]rustBinding
	globs     []rustBinding
	facts     []SourceGraphFact
}

type rustBinding struct {
	module   *rustModule
	file     string
	external bool
	standard bool
	extern   bool
	alias    []string
	scope    *rustModule
}

type rustCrate struct {
	pkg      Package
	target   Target
	root     *rustModule
	modules  []*rustModule
	files    map[string]bool
	findings []policy.Finding
	noStd    bool
}

type moduleInventory struct {
	repo      repository.Repository
	sources   map[string]Source
	available map[string]bool
	crates    []*rustCrate
}

func ResolveModules(repo repository.Repository, workspaces []Workspace, sources Sources, selected, allFiles []string) ModuleGraph {
	inventory := moduleInventory{repo: repo, sources: map[string]Source{}, available: map[string]bool{}}
	for _, path := range allFiles {
		inventory.available[path] = true
	}
	for _, source := range sources.Files {
		inventory.sources[source.Path] = source
	}
	inventory.buildCrates(workspaces)
	return inventory.resolveSelected(selected)
}

func (inventory *moduleInventory) buildCrates(workspaces []Workspace) {
	seen := map[string]bool{}
	for _, workspace := range workspaces {
		for _, pkg := range workspace.Packages {
			for _, target := range pkg.Targets {
				key := pkg.Manifest + ":" + target.Path
				if seen[key] {
					continue
				}
				seen[key] = true
				crate := &rustCrate{pkg: pkg, target: target, files: map[string]bool{}}
				crate.root = newRustModule(crate, nil, target.Path, filepath.ToSlash(filepath.Dir(target.Path)), false)
				inventory.crates = append(inventory.crates, crate)
				inventory.readModuleFile(crate.root, nil)
			}
		}
	}
}

func newRustModule(crate *rustCrate, parent *rustModule, file, directory string, inline bool) *rustModule {
	module := &rustModule{crate: crate, parent: parent, file: file, directory: directory, inline: inline, bindings: map[string]rustBinding{}}
	crate.modules = append(crate.modules, module)
	crate.files[file] = true
	return module
}

func (inventory *moduleInventory) resolveSelected(selected []string) ModuleGraph {
	result := ModuleGraph{}
	queue := []*rustCrate{}
	for _, path := range selected {
		if inventory.repo.Language(path) != "rust" || !inventory.repo.NativeAnalysis(path, "architecture") {
			continue
		}
		found := false
		for _, crate := range inventory.crates {
			if crate.files[path] {
				queue = append(queue, crate)
				found = true
			}
		}
		if !found {
			result.Findings = append(result.Findings, moduleCoverage(path, SourceGraphFact{}, "source has no parsed Cargo target/module membership"))
		}
	}
	visited := map[*rustCrate]bool{}
	paths := map[string]bool{}
	for len(queue) > 0 {
		crate := queue[0]
		queue = queue[1:]
		if visited[crate] {
			continue
		}
		visited[crate] = true
		result.Findings = append(result.Findings, crate.findings...)
		for file := range crate.files {
			if !paths[file] {
				paths[file] = true
				result.Sources = append(result.Sources, ModuleSource{Path: file, Root: filepath.ToSlash(filepath.Dir(crate.pkg.Manifest))})
			}
		}
		queue = append(queue, inventory.resolveCrate(crate, &result)...)
	}
	slices.SortFunc(result.Sources, func(left, right ModuleSource) int { return strings.Compare(left.Path, right.Path) })
	return result
}

func (inventory *moduleInventory) resolveCrate(crate *rustCrate, result *ModuleGraph) []*rustCrate {
	connected := []*rustCrate{}
	for _, module := range crate.modules {
		connected = append(connected, inventory.resolveModule(module, result)...)
	}
	return connected
}

func (inventory *moduleInventory) resolveModule(module *rustModule, result *ModuleGraph) []*rustCrate {
	connected := []*rustCrate{}
	for _, fact := range module.facts {
		binding, resolved, err := inventory.resolveFact(module, fact)
		if err != nil {
			result.Findings = append(result.Findings, moduleCoverage(module.file, fact, err.Error()))
			continue
		}
		if !resolved {
			continue
		}
		if binding.module != nil {
			connected = append(connected, binding.module.crate)
			binding.file = binding.module.file
		}
		if binding.scope != nil {
			connected = append(connected, binding.scope.crate)
		}
		if binding.file != "" && binding.file != module.file {
			result.Imports = append(result.Imports, ModuleImport{Source: module.file, Target: binding.file, Line: fact.Line, Column: fact.Column, ReExport: fact.Kind == "use" && fact.Public})
		}
	}
	return connected
}

func (inventory *moduleInventory) resolveFact(module *rustModule, fact SourceGraphFact) (rustBinding, bool, error) {
	switch fact.Kind {
	case "macro":
		return rustBinding{}, false, inventory.resolveMacro(module, fact.Path, fact.Name, 0)
	case "unsupported":
		return rustBinding{}, false, fmt.Errorf("%s", fact.Name)
	case "use", "extern", "reference":
		binding, err := inventory.resolvePath(module, fact.Path, fact.Kind == "use", 0)
		return binding, true, err
	}
	return rustBinding{}, false, nil
}

func moduleCoverage(path string, fact SourceGraphFact, reason string) policy.Finding {
	return policy.Finding{Check: "architecture.importCoverage", Path: path, Subject: "rust", Line: fact.Line, Column: fact.Column, Message: fmt.Sprintf("Rust written-module graph is incomplete: %s", reason)}
}
