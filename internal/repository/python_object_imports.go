package repository

import (
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/pythonfacts"
)

func (repo Repository) PythonObjectImportInputs(project PythonProject) []pythonfacts.ObjectRegistryInput {
	paths := map[string]bool{}
	for _, declaration := range repo.Config.Scope.PythonComputedImports {
		if declaration.Project == project.Manifest && declaration.Callee == "pkgutil.resolve_name" {
			for _, input := range declaration.Configuration {
				paths[input.Path] = true
			}
		}
	}
	for _, declaration := range repo.Config.Scope.PythonExternalPluginImports {
		if declaration.Project == project.Manifest && declaration.Configuration != nil {
			paths[declaration.Configuration.Path] = true
		}
	}
	result := []pythonfacts.ObjectRegistryInput{}
	for path := range paths {
		input := pythonfacts.ObjectRegistryInput{Path: path}
		content, err := repo.pythonReachabilityRegistry(project, path)
		if err != nil {
			input.Error = err.Error()
		} else {
			input.Content = string(content)
		}
		result = append(result, input)
	}
	slices.SortFunc(result, func(left, right pythonfacts.ObjectRegistryInput) int { return strings.Compare(left.Path, right.Path) })
	return result
}
