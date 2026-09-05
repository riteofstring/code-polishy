package architecture

import (
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type ModuleSummary struct {
	Name          string `json:"name"`
	Production    int    `json:"production"`
	Tests         int    `json:"tests"`
	Incoming      int    `json:"incoming"`
	Outgoing      int    `json:"outgoing"`
	FocusedSuites int    `json:"focusedSuites"`
	External      int    `json:"externalCompositions"`
}

func Summary(repo repository.Repository, files []string, graph *sourcegraph.Graph) []ModuleSummary {
	byName := initializeModuleSummaries(repo)
	countIncomingDependencies(repo, byName)
	countOwnedFiles(repo, files, byName)
	countFocusedSuites(repo, byName)
	countExternalCompositions(graph, byName)
	return sortedModuleSummaries(byName)
}

func countExternalCompositions(graph *sourcegraph.Graph, summaries map[string]*ModuleSummary) {
	if graph == nil {
		return
	}
	owners := map[string]string{}
	for _, node := range graph.Nodes {
		owners[node.Path] = node.Module
	}
	for _, edge := range graph.External {
		if summary := summaries[owners[edge.Source]]; summary != nil {
			summary.External++
		}
	}
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
		owners := repo.OwnerModuleNames(path)
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
