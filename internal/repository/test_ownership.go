package repository

import "github.com/riteofstring/code-polishy/internal/policy"

func (repo Repository) TestOwnerships(path string) []policy.TestOwnership {
	if repo.pathFactCache != nil {
		return cachedPathFact(repo.pathFactCache, &repo.pathFactCache.testOwnerships, path, cloneTestOwnerships, func() []policy.TestOwnership { return repo.computeTestOwnerships(path) })
	}
	return repo.computeTestOwnerships(path)
}

func (repo Repository) computeTestOwnerships(path string) []policy.TestOwnership {
	if !repo.IsTest(path) || !repo.IsExecutableSource(path) {
		return nil
	}
	owners := []policy.TestOwnership{}
	for _, ownership := range repo.Config.Tests.Ownership {
		if policy.MatchesAny(path, ownership.Paths) {
			owners = append(owners, ownership)
		}
	}
	return owners
}

func (repo Repository) OwnerModuleNames(path string) []string {
	if repo.pathFactCache != nil {
		return cachedPathFact(repo.pathFactCache, &repo.pathFactCache.ownerModules, path, cloneStrings, func() []string { return repo.computeOwnerModuleNames(path) })
	}
	return repo.computeOwnerModuleNames(path)
}

func (repo Repository) computeOwnerModuleNames(path string) []string {
	if !repo.IsTest(path) || !repo.IsExecutableSource(path) {
		return repo.ModuleNames(path)
	}
	names := []string{}
	for _, ownership := range repo.TestOwnerships(path) {
		names = append(names, ownership.Module)
	}
	return names
}
