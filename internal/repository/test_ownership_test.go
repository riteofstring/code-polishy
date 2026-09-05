package repository

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestModuleSelectionAndImpactUseExplicitTestOwnership(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir(), Config: policy.Config{
		Modules: []policy.Module{
			{Name: "api", Paths: []string{"api/**"}, DependsOn: []string{"domain"}},
			{Name: "domain", Paths: []string{"domain/**"}},
		},
		ModuleByName: map[string]int{"api": 0, "domain": 1},
		Tests: policy.Testing{
			Paths: []string{"checks/contract.py"},
			Ownership: []policy.TestOwnership{
				{Paths: []string{"api/domain_test.go"}, Module: "domain", FocusedSuite: "domain-unit"},
				{Paths: []string{"checks/contract.py"}, Module: "api", FocusedSuite: "api-unit"},
			},
		},
	}}
	writeFile(t, repo.Root, "api/handler.go", "package api\n")
	writeFile(t, repo.Root, "domain/value.go", "package domain\n")
	writeFile(t, repo.Root, "api/domain_test.go", "package api_test\n")
	writeFile(t, repo.Root, "checks/contract.py", "from api import handle\n")
	for _, testCase := range []struct {
		module string
		paths  []string
	}{
		{"api", []string{"api/handler.go", "checks/contract.py"}},
		{"domain", []string{"api/domain_test.go", "domain/value.go"}},
	} {
		selection, err := repo.moduleSelection([]string{testCase.module})
		if err != nil || !slices.Equal(selection.Files, testCase.paths) {
			t.Fatalf("%s selection = %+v, error = %v", testCase.module, selection, err)
		}
	}
	impact := repo.ImpactForPaths([]string{"api/domain_test.go"})
	if !slices.Equal(impact.DirectModules, []string{"domain"}) || !slices.Equal(impact.ImpactedModules, []string{"api", "domain"}) {
		t.Fatalf("test impact = %+v", impact)
	}
	repo.Config.Tests.Ownership = nil
	if owners := repo.OwnerModuleNames("api/domain_test.go"); len(owners) != 0 {
		t.Fatalf("production path assigned test ownership: %v", owners)
	}
	if repo.IsTest("api/handler.go") {
		t.Fatal("ownership changed production classification")
	}
}

func TestModuleSelectionRetainsNonExecutableTestInputs(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir(), Config: policy.Config{
		Modules:      []policy.Module{{Name: "api", Paths: []string{"api/**"}}},
		ModuleByName: map[string]int{"api": 0},
	}}
	writeFile(t, repo.Root, "api/behavior.test.txt", "expected response\n")
	writeFile(t, repo.Root, "api/behavior_test.go", "package api_test\n")
	selection, err := repo.moduleSelection([]string{"api"})
	if err != nil || !slices.Equal(selection.Files, []string{"api/behavior.test.txt"}) {
		t.Fatalf("module selection lost test data or inferred executable ownership: %+v, %v", selection, err)
	}
	impact := repo.ImpactForPaths([]string{"api/behavior.test.txt"})
	if !slices.Equal(impact.DirectModules, []string{"api"}) {
		t.Fatalf("test input lost its module impact: %+v", impact)
	}
}
