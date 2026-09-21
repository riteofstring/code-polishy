package pack

import (
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func SelectedFiles(repo repository.Repository, selection repository.Selection, command policy.Command, profile string) []string {
	paths := slices.Clone(selection.Files)
	if selection.All {
		if inventory, err := repo.AllFiles(); err == nil {
			paths = inventory
		}
	}
	files := []string{}
	for _, file := range paths {
		if selectedProviderFile(repo, command, file, profile) {
			files = append(files, file)
		}
	}
	return sortedUnique(files)
}

func AdapterSelected(repo repository.Repository, selection repository.Selection, command policy.Command, profile string) bool {
	if len(SelectedFiles(repo, selection, command, profile)) > 0 {
		return true
	}
	for _, file := range selectionPaths(selection) {
		metadata, dependency := adapterMetadata(command.Adapter, file)
		projectWide := slices.Contains([]string{"typecheck", "dead-code", "architecture", "build", "dependency-policy", "lock-sync", "release-age", "security"}, command.Adapter.Capability)
		if projectWide && (metadata || dependency || file == policy.ConfigFilename) {
			return hasProjectDiscovery(command.Adapter)
		}
	}
	return false
}

func selectionPaths(selection repository.Selection) []string {
	return sortedUnique(append(append(slices.Clone(selection.Files), selection.Candidate.AddedOrModified...), selection.Candidate.Deleted...))
}

func selectedProviderFile(repo repository.Repository, command policy.Command, file, profile string) bool {
	if !packCommandSelects(repo, command, file) {
		return false
	}
	owner := repo.AnalysisOwner(file, command.Adapter.Capability, profile)
	return owner.Name == command.Name || len(repo.Config.Checks) == 0
}

func hasProjectDiscovery(adapter *policy.PackAdapter) bool {
	if adapter == nil {
		return false
	}
	return slices.ContainsFunc(adapter.Discovery, func(discovery policy.PackDiscovery) bool {
		return discovery.Mode != "file-scoped"
	})
}
