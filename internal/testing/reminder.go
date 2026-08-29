package testing

import (
	"sort"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func ChangedTestPaths(repo repository.Repository, candidate repository.CandidateDelta) []string {
	paths := []string{}
	for _, path := range candidate.AddedOrModified {
		if repo.IsTest(path) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if len(result) == 0 || result[len(result)-1] != path {
			result = append(result, path)
		}
	}
	return result
}
