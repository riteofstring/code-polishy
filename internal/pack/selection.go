package pack

import (
	"path"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func SelectedFiles(repo repository.Repository, selection repository.Selection, command policy.Command, profile string) []string {
	files := []string{}
	for _, file := range selection.Files {
		if selectedProviderFile(repo, command, file, profile) {
			files = append(files, file)
		}
	}
	if !slices.Contains([]string{"typecheck", "dead-code", "architecture"}, command.Adapter.Capability) {
		return sortedUnique(files)
	}
	inventory, err := repo.AllFiles()
	if err != nil {
		return files
	}
	return sortedUnique(append(files, selectedInventoryFiles(repo, selection, command, profile, inventory)...))
}

func selectedInventoryFiles(repo repository.Repository, selection repository.Selection, command policy.Command, profile string, inventory []string) []string {
	units := newUnitInventory(inventory)
	changed := append(slices.Clone(selection.Files), selection.Candidate.Deleted...)
	triggered := slices.ContainsFunc(changed, func(file string) bool {
		return providerMetadata(file) || selectedProviderFile(repo, command, file, profile)
	})
	files := []string{}
	for _, file := range inventory {
		if !selectedProviderFile(repo, command, file, profile) {
			continue
		}
		unit := units.unit(repo, file)
		if selection.All || command.Adapter.Capability == "dead-code" && triggered || selectedUnitMetadata(changed, unit) {
			files = append(files, file)
		}
	}
	return files
}

func selectedProviderFile(repo repository.Repository, command policy.Command, file, profile string) bool {
	if !packCommandSelects(repo, command, file) {
		return false
	}
	owner := repo.AnalysisOwner(file, command.Adapter.Capability, profile)
	return owner.Name == command.Name || len(repo.Config.Checks) == 0
}

func providerMetadata(file string) bool {
	return slices.Contains([]string{"package.json", "pnpm-workspace.yaml", policy.ConfigFilename}, path.Base(file)) || javaScriptConfiguration(file)
}

func selectedUnitMetadata(changed []string, unit AnalysisUnit) bool {
	for _, file := range changed {
		if file == policy.ConfigFilename || file == unit.Manifest || file == unit.Configuration {
			return true
		}
		if path.Base(file) == "pnpm-workspace.yaml" && pathWithin(unit.PackageRoot, path.Dir(file)) {
			return true
		}
	}
	return false
}
