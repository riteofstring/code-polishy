package architecture

import (
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/repository"
)

type ModuleSummary struct {
	Name          string
	Production    int
	Tests         int
	Incoming      int
	Outgoing      int
	FocusedSuites int
}

func Summary(repo repository.Repository, files []string) []ModuleSummary {
	byName := initializeModuleSummaries(repo)
	countIncomingDependencies(repo, byName)
	countOwnedFiles(repo, files, byName)
	countFocusedSuites(repo, byName)
	return sortedModuleSummaries(byName)
}

func initializeModuleSummaries(repo repository.Repository) map[string]*ModuleSummary {
	byName := map[string]*ModuleSummary{}
	for _, module := range repo.Config.Modules {
		byName[module.Name] = &ModuleSummary{Name: module.Name, Outgoing: len(module.DependsOn)}
	}
	return byName
}

func countIncomingDependencies(repo repository.Repository, byName map[string]*ModuleSummary) {
	for _, module := range repo.Config.Modules {
		for _, dependency := range module.DependsOn {
			if summary := byName[dependency]; summary != nil {
				summary.Incoming++
			}
		}
	}
}

func countOwnedFiles(repo repository.Repository, files []string, byName map[string]*ModuleSummary) {
	for _, path := range files {
		if !repo.IsExecutableSource(path) {
			continue
		}
		owners := repo.ModuleNames(path)
		if len(owners) != 1 || byName[owners[0]] == nil {
			continue
		}
		if repo.IsTest(path) {
			byName[owners[0]].Tests++
		} else {
			byName[owners[0]].Production++
		}
	}
}

func countFocusedSuites(repo repository.Repository, byName map[string]*ModuleSummary) {
	for _, suite := range repo.Config.Tests.Suites {
		if suite.Scope != "module" || suite.Cost != "quick" || !slices.Contains(suite.RunOn, "focused") {
			continue
		}
		for _, module := range suite.Modules {
			if summary := byName[module]; summary != nil {
				summary.FocusedSuites++
			}
		}
	}
}

func sortedModuleSummaries(byName map[string]*ModuleSummary) []ModuleSummary {
	result := make([]ModuleSummary, 0, len(byName))
	for _, summary := range byName {
		result = append(result, *summary)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}
