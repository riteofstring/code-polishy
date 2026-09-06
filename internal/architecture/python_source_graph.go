package architecture

import (
	"sort"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func pythonGraphNodes(
	repo repository.Repository,
	project repository.PythonProject,
	sources []string,
	dependencies map[string]map[string]bool,
) []sourcegraph.Node {
	paths := map[string]bool{}
	for _, source := range sources {
		paths[source] = true
	}
	for _, targets := range dependencies {
		for target := range targets {
			paths[target] = true
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	nodes := make([]sourcegraph.Node, 0, len(ordered))
	for _, path := range ordered {
		owner, err := sourceModuleOwner(repo, path)
		if err != nil {
			continue
		}
		nodes = append(nodes, sourcegraph.Node{
			Path: path, Language: "python", Generated: repo.IsGenerated(path), Test: repo.IsTest(path),
			Root: project.Root, Module: owner, Resolution: "file:" + path,
		})
	}
	return nodes
}

func pythonGraphEdge(source, target string, line, column int, kind string) sourcegraph.Edge {
	return sourcegraph.Edge{
		Source: source, Target: target, SourceResolution: "file:" + source, TargetResolution: "file:" + target,
		Line: line, Column: column, Ecosystem: "python", Kind: sourcegraph.EdgeKind(kind),
	}
}
