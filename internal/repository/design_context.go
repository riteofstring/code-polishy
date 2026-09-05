package repository

import (
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type DesignMatchReason struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type DesignMatch struct {
	Path    string              `json:"path"`
	Reasons []DesignMatchReason `json:"reasons"`
}

type DesignResolution struct {
	SelectedModules   []string      `json:"selectedModules"`
	SelectedPathCount int           `json:"selectedPathCount"`
	Matches           []DesignMatch `json:"matches"`
	UnmappedModules   []string      `json:"unmappedModules"`
	UnmappedPaths     []string      `json:"unmappedPaths"`
}

type designScope struct {
	files           map[string]bool
	modules         map[string]bool
	explicitModules map[string]bool
	coveredFiles    map[string]bool
	coveredModules  map[string]bool
	matchedModules  map[string]bool
}

func (repo Repository) ResolveDesignContext(paths, modules []string) (DesignResolution, error) {
	scope, err := repo.designScope(paths, modules)
	if err != nil {
		return DesignResolution{}, err
	}
	result := DesignResolution{SelectedModules: sortedSet(scope.modules), SelectedPathCount: len(scope.files), Matches: []DesignMatch{}, UnmappedModules: []string{}, UnmappedPaths: []string{}}
	for _, document := range repo.Config.Documentation.Design {
		reasons := repo.designMatchReasons(document, &scope)
		if len(reasons) > 0 {
			result.Matches = append(result.Matches, DesignMatch{Path: document.Path, Reasons: reasons})
		}
	}
	slices.SortFunc(result.Matches, func(left, right DesignMatch) int { return strings.Compare(left.Path, right.Path) })
	repo.designCoverage(&result, scope)
	return result, nil
}

func (repo Repository) designScope(paths, modules []string) (designScope, error) {
	scope := designScope{files: map[string]bool{}, modules: map[string]bool{}, explicitModules: map[string]bool{}, coveredFiles: map[string]bool{}, coveredModules: map[string]bool{}, matchedModules: map[string]bool{}}
	for _, module := range modules {
		if !repo.hasModule(module) {
			return scope, fmt.Errorf("unknown module %q", module)
		}
		if scope.explicitModules[module] {
			return scope, fmt.Errorf("module %q was specified more than once", module)
		}
		scope.modules[module], scope.explicitModules[module] = true, true
	}
	for _, path := range paths {
		normalized, err := repo.NormalizePath(path)
		if err != nil {
			continue
		}
		scope.files[normalized] = true
		for _, module := range repo.OwnerModuleNames(normalized) {
			scope.modules[module] = true
		}
	}
	return scope, nil
}

func (repo Repository) designMatchReasons(document policy.DesignDocument, scope *designScope) []DesignMatchReason {
	reasons := []DesignMatchReason{}
	if document.Module != "" && scope.modules[document.Module] {
		reasons = append(reasons, DesignMatchReason{Kind: "module", Value: document.Module})
		scope.coveredModules[document.Module], scope.matchedModules[document.Module] = true, true
	}
	for _, path := range document.SourcePaths {
		owners := repo.OwnerModuleNames(path)
		selected := scope.files[path]
		for _, module := range owners {
			selected = selected || scope.explicitModules[module]
		}
		if !selected {
			continue
		}
		reasons = append(reasons, DesignMatchReason{Kind: "source", Value: path})
		scope.coveredFiles[path] = true
		for _, module := range owners {
			scope.matchedModules[module] = true
		}
	}
	slices.SortFunc(reasons, func(left, right DesignMatchReason) int {
		return strings.Compare(left.Kind+"\x00"+left.Value, right.Kind+"\x00"+right.Value)
	})
	return slices.Compact(reasons)
}

func (repo Repository) designCoverage(result *DesignResolution, scope designScope) {
	missingModules := map[string]bool{}
	for path := range scope.files {
		covered := scope.coveredFiles[path]
		owners := repo.OwnerModuleNames(path)
		for _, module := range owners {
			covered = covered || scope.coveredModules[module]
		}
		if covered {
			continue
		}
		result.UnmappedPaths = append(result.UnmappedPaths, path)
		for _, module := range owners {
			missingModules[module] = true
		}
	}
	for module := range scope.explicitModules {
		if !scope.matchedModules[module] {
			missingModules[module] = true
		}
	}
	slices.Sort(result.UnmappedPaths)
	result.UnmappedModules = sortedSet(missingModules)
}

func (resolution DesignResolution) DocumentPaths() []string {
	paths := make([]string, len(resolution.Matches))
	for index, match := range resolution.Matches {
		paths[index] = match.Path
	}
	return paths
}
