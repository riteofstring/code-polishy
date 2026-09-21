package pack

import (
	"path"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func governedInventory(repo repository.Repository, command policy.Command, paths []string, profile string) []InventoryEntry {
	entries := make([]InventoryEntry, 0, len(paths))
	for _, file := range sortedUnique(paths) {
		language, source := adapterLanguage(repo, command.Adapter, file)
		metadata, dependency := adapterMetadata(command.Adapter, file)
		_, asset, assetErr := repo.AssetLinkIdentity(file)
		asset = asset && assetErr == nil
		control := repo.IsControlInput(file)
		if !source && !metadata && !dependency && !asset && !control {
			continue
		}
		entry := InventoryEntry{
			Path: file, Language: language, Context: repo.SourceContextPath(file), Modules: repo.OwnerModuleNames(file),
			Source: source, Metadata: metadata, Dependency: dependency, Asset: asset, Test: repo.IsTest(file), Generated: repo.IsGenerated(file),
			Data: repo.IsData(file), Development: repo.IsDevelopment(file), Control: control,
		}
		if source && packCapabilitySelects(repo, command.Adapter.Capability, file) {
			if len(repo.Config.Checks) == 0 {
				entry.Owner = command.Name
			} else {
				owner := repo.AnalysisOwner(file, command.Adapter.Capability, profile)
				if owner.Problem == "" {
					entry.Owner = owner.Name
				}
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

func adapterLanguage(repo repository.Repository, adapter *policy.PackAdapter, file string) (string, bool) {
	if adapter == nil {
		return "", false
	}
	for _, language := range adapter.Languages {
		if policy.MatchesAny(file, language.Paths) || len(language.Paths) == 0 && repo.Language(file) == language.Name {
			return language.Name, true
		}
	}
	return "", false
}

func adapterMetadata(adapter *policy.PackAdapter, file string) (bool, bool) {
	if adapter == nil {
		return false, false
	}
	metadata, dependency := false, false
	for _, discovery := range adapter.Discovery {
		metadata = metadata || policy.MatchesAny(file, discovery.MetadataPatterns)
		dependency = dependency || policy.MatchesAny(file, discovery.DependencyPatterns)
	}
	return metadata, dependency
}

func sourceInput(entry InventoryEntry, scopes []string) SourceInput {
	return SourceInput{
		Scopes: slices.Clone(scopes), Owner: entry.Owner, Context: entry.Context, Path: entry.Path, Language: entry.Language,
		Test: entry.Test, Generated: entry.Generated, Data: entry.Data, Development: entry.Development,
	}
}

func initializeRequestScope(request *Request) {
	request.Scopes = []AnalysisScope{}
	request.DiagnosticFiles = slices.Clone(request.Files)
	request.WriteFiles = []string{}
	if request.Capability == "format" && request.Mode == "write" {
		request.WriteFiles = slices.Clone(request.Files)
	}
}

func coreEntryPoint(repo repository.Repository, root, file string) bool {
	if repo.IsTest(file) || policy.MatchesAny(file, repo.Config.Scope.EntryPoints) {
		return true
	}
	directory, name := path.Dir(file), path.Base(file)
	stem := strings.TrimSuffix(name, path.Ext(name))
	entry := slices.Contains([]string{"cli", "index", "main"}, stem)
	return directory == root && (entry || strings.HasSuffix(stem, ".config")) || directory == path.Join(root, "src") && entry
}
