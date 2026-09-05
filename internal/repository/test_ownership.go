package repository

import "github.com/riteofstring/code-polishy/internal/policy"

func (repo Repository) TestOwnerships(path string) []policy.TestOwnership {
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
	if !repo.IsTest(path) || !repo.IsExecutableSource(path) {
		return repo.ModuleNames(path)
	}
	names := []string{}
	for _, ownership := range repo.TestOwnerships(path) {
		names = append(names, ownership.Module)
	}
	return names
}
