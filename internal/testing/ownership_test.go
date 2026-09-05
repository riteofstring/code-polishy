package testing

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestTestOwnershipUsesOneProductionModuleWithoutAddingArchitectureEdges(t *testing.T) {
	t.Parallel()
	config := policy.Config{
		Modules:      []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}},
		ModuleByName: map[string]int{"domain": 0},
		Tests: policy.Testing{Paths: []string{"domain/checks.py"}, Ownership: []policy.TestOwnership{{Paths: []string{"domain/checks.py"}, Module: "domain", FocusedSuite: "domain-unit"}}, Suites: []policy.TestSuite{{
			Name: "domain-unit", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{"domain"},
			RunOn: []string{"focused", "recommended", "full"}, Paths: []string{"domain/**"},
		}}},
	}
	repo := repository.Repository{Root: t.TempDir(), Config: config}
	writeTestQualityFile(t, repo.Root, "domain/checks.py", "from domain import execute\ndef test_domain():\n    assert execute('value') == 'VALUE'\n")
	if findings := OwnershipFindings(repo, []string{"domain/checks.py"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	config.Tests.Suites[0].Paths = []string{"domain/other/**"}
	repo.Config = config
	findings := OwnershipFindings(repo, []string{"domain/checks.py"})
	if len(findings) != 1 || findings[0].Check != "policy.testOwnership" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestExplicitTestOwnerIsIndependentOfProductionPathOwnership(t *testing.T) {
	t.Parallel()
	repo := ownershipRepository(t)
	path := "domain/value_test.go"
	writeTestQualityFile(t, repo.Root, path, "package domain_test\n")
	repo.Config.Tests.Ownership = []policy.TestOwnership{{Paths: []string{path}, Module: "api", FocusedSuite: "api-unit"}}
	if findings := OwnershipFindings(repo, []string{path}); len(findings) != 0 {
		t.Fatalf("explicit owner rejected: %+v", findings)
	}
	if owners := repo.OwnerModuleNames(path); len(owners) != 1 || owners[0] != "api" {
		t.Fatalf("test owners = %v", owners)
	}
	repo.Config.Tests.Ownership = nil
	findings := OwnershipFindings(repo, []string{path})
	if len(findings) != 1 || findings[0].Subject != "unmapped" || len(repo.OwnerModuleNames(path)) != 0 {
		t.Fatalf("production path implicitly owned a test: %+v", findings)
	}
}

func TestTestOwnershipCoverageRejectsInvalidExecutionAndSourceAssignments(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		change func(*repository.Repository)
		want   string
	}{
		{"stale", func(repo *repository.Repository) {
			repo.Config.Tests.Ownership[0].Paths = []string{"domain/missing_test.go"}
		}, "stale"},
		{"production", func(repo *repository.Repository) { repo.Config.Tests.Ownership[0].Paths = []string{"domain/value.go"} }, "cannot classify"},
		{"uncovered", func(repo *repository.Repository) { repo.Config.Tests.Suites[0].Paths = []string{"elsewhere/**"} }, "do not include"},
		{"wrong module", func(repo *repository.Repository) { repo.Config.Tests.Suites[0].Modules = []string{"api"} }, "primary focused suite"},
		{"repository suite", func(repo *repository.Repository) { repo.Config.Tests.Suites[0].Scope = "repository" }, "primary focused suite"},
		{"overlap", func(repo *repository.Repository) {
			repo.Config.Tests.Ownership = append(repo.Config.Tests.Ownership, repo.Config.Tests.Ownership[0])
		}, "multiple ownership"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := ownershipRepository(t)
			testCase.change(&repo)
			findings := OwnershipFindings(repo, []string{"domain/value.go", "domain/value_test.go"})
			for _, finding := range findings {
				if finding.Check == "policy.testOwnership" && strings.Contains(finding.Message, testCase.want) {
					return
				}
			}
			t.Fatalf("no %q finding in %+v", testCase.want, findings)
		})
	}
}

func TestUnmappedTestDiagnosticsProveAPairAndExposeAmbiguousAlternatives(t *testing.T) {
	t.Parallel()
	repo := ownershipRepository(t)
	repo.Config.Tests.Ownership = nil
	findings := OwnershipFindings(repo, []string{"domain/value.go", "domain/value_test.go"})
	if len(findings) != 1 || findings[0].Fields["expectedOwner"] != "domain" {
		t.Fatalf("paired owner = %+v", findings)
	}
	var owner policy.TestOwnership
	if err := json.Unmarshal(findings[0].Remediation.Configuration, &owner); err != nil {
		t.Fatal(err)
	}
	if owner.Module != "domain" || owner.FocusedSuite != "domain-unit" || len(owner.Paths) != 1 || owner.Paths[0] != "domain/value_test.go" {
		t.Fatalf("suggested declaration = %+v", owner)
	}
	if !strings.Contains(findings[0].Remediation.Summary, "behavior the test verifies") {
		t.Fatalf("remediation = %+v", findings[0].Remediation)
	}
	repo.Config.Modules[0].Paths = append(repo.Config.Modules[0].Paths, "shared/**")
	repo.Config.Modules[1].Paths = append(repo.Config.Modules[1].Paths, "shared/**")
	writeTestQualityFile(t, repo.Root, "shared/check_test.go", "package shared\n")
	findings = OwnershipFindings(repo, []string{"shared/check_test.go"})
	if len(findings) != 1 || findings[0].Fields["expectedOwner"] != "" {
		t.Fatalf("fabricated ambiguous owner: %+v", findings)
	}
	var suggestion struct {
		Alternatives []testOwnerCandidate `json:"alternatives"`
	}
	if err := json.Unmarshal(findings[0].Remediation.Configuration, &suggestion); err != nil {
		t.Fatal(err)
	}
	if len(suggestion.Alternatives) != 2 || suggestion.Alternatives[0].Module != "api" || suggestion.Alternatives[1].Module != "domain" || suggestion.Alternatives[0].Ownership == nil || suggestion.Alternatives[1].Ownership == nil {
		t.Fatalf("alternatives = %+v", suggestion)
	}
}

func ownershipRepository(t *testing.T) repository.Repository {
	t.Helper()
	config := policy.Config{
		Modules:      []policy.Module{{Name: "domain", Paths: []string{"domain/**"}}, {Name: "api", Paths: []string{"api/**"}}},
		ModuleByName: map[string]int{"domain": 0, "api": 1},
		Tests:        policy.Testing{Ownership: []policy.TestOwnership{{Paths: []string{"domain/value_test.go"}, Module: "domain", FocusedSuite: "domain-unit"}}},
	}
	for _, module := range config.Modules {
		config.Tests.Suites = append(config.Tests.Suites, policy.TestSuite{Name: module.Name + "-unit", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{module.Name}, Paths: []string{"domain/*_test.go", "shared/*_test.go"}, RunOn: []string{"focused", "recommended", "full"}})
	}
	repo := repository.Repository{Root: t.TempDir(), Config: config}
	writeTestQualityFile(t, repo.Root, "domain/value.go", "package domain\n")
	writeTestQualityFile(t, repo.Root, "domain/value_test.go", "package domain\n")
	return repo
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
	findings := OwnershipFindings(repo, []string{"shared/value_test.go", "outside/value_test.go"})
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
}
