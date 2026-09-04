package testing

import (
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestTestOwnershipUsesOneProductionModuleWithoutAddingArchitectureEdges(t *testing.T) {
	t.Parallel()
	config := policy.Config{
		Scope:        policy.Scope{Tests: []string{"domain/checks.py"}},
		Modules:      []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}},
		ModuleByName: map[string]int{"domain": 0},
		Tests: policy.Testing{Suites: []policy.TestSuite{{
			Name: "domain-unit", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{"domain"},
			RunOn: []string{"focused", "recommended", "full"},
		}}},
	}
	repo := repository.Repository{Config: config}
	if findings := testOwnershipFindings(repo, []string{"domain/checks.py"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	config.Tests.Suites[0].Paths = []string{"domain/other/**"}
	repo.Config = config
	findings := testOwnershipFindings(repo, []string{"domain/checks.py"})
	if len(findings) != 1 || findings[0].Check != "policy.testOwnership" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestTestOwnershipRejectsUnownedAndMultiplyOwnedTestSource(t *testing.T) {
	t.Parallel()
	config := policy.Config{
		Modules: []policy.Module{
			{Name: "a", Paths: []string{"shared/**"}},
			{Name: "b", Paths: []string{"shared/**"}},
		},
		ModuleByName: map[string]int{"a": 0, "b": 1},
	}
	repo := repository.Repository{Config: config}
	findings := testOwnershipFindings(repo, []string{"shared/value_test.go", "outside/value_test.go"})
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
}
