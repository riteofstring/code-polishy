package testing

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestIntegrationOwnershipPreservesFocusedAndFullSelection(t *testing.T) {
	t.Parallel()
	repo, files := integrationOwnershipRepository(t)
	if findings := CoverageFindings(repo, files); len(findings) != 0 {
		t.Fatalf("integration ownership rejected: %+v", findings)
	}
	for _, path := range files[2:] {
		if owners := repo.OwnerModuleNames(path); !slices.Equal(owners, []string{"domain"}) {
			t.Fatalf("owner of %s = %v", path, owners)
		}
		plan, err := BuildPlan(repo, Request{Changed: selectionForPaths(path)})
		if err != nil || !slices.Equal(suiteNames(plan.Suites), []string{"domain-unit"}) {
			t.Fatalf("focused plan for %s = %+v, error = %v", path, plan, err)
		}
	}
	full, err := BuildPlan(repo, Request{Full: true})
	if err != nil || !slices.Contains(suiteNames(full.Suites), "browser-integration") {
		t.Fatalf("full plan = %+v, error = %v", full, err)
	}
}

func TestIntegrationOwnershipCannotReplaceQuickCoverageOrHideExecution(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		change func(*repository.Repository)
		want   string
	}{
		{"missing focused suite", func(repo *repository.Repository) { repo.Config.Tests.Suites = repo.Config.Tests.Suites[1:] }, "no focused boundary"},
		{"expensive focused suite", func(repo *repository.Repository) { repo.Config.Tests.Suites[0].Cost = "expensive" }, "quick module-scoped"},
		{"wrong module", func(repo *repository.Repository) {
			suite := &repo.Config.Tests.Suites[2]
			suite.Scope, suite.Modules = "module", []string{"api"}
		}, "full-profile suite for module"},
		{"supplemental only", func(repo *repository.Repository) { repo.Config.Tests.Suites[2].RunOn = []string{"supplemental"} }, "without supplemental execution"},
		{"missing full", func(repo *repository.Repository) { repo.Config.Tests.Suites[2].RunOn = []string{"recommended"} }, "full-profile suite"},
		{"missing helper", func(repo *repository.Repository) {
			repo.Config.Tests.Suites[2].Paths = []string{"checks/browser.test.js"}
		}, "do not include this owned test"},
		{"unowned helper", func(repo *repository.Repository) {
			repo.Config.Tests.Ownership[1].Paths = []string{"checks/browser.test.js"}
		}, "no explicit tests.ownership"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo, files := integrationOwnershipRepository(t)
			testCase.change(&repo)
			findings := CoverageFindings(repo, files)
			if !slices.ContainsFunc(findings, func(finding policy.Finding) bool { return strings.Contains(finding.Message, testCase.want) }) {
				t.Fatalf("missing %q finding: %+v", testCase.want, findings)
			}
		})
	}
}

func TestFullOnlyExecutionCannotEmptyFocusedOwnership(t *testing.T) {
	t.Parallel()
	for _, cost := range []string{"quick", "standard", "expensive"} {
		t.Run(cost, func(t *testing.T) {
			repo, files := integrationOwnershipRepository(t)
			repo.Config.Tests.Ownership[0].ExecutionSuite = "browser-integration"
			repo.Config.Tests.Suites[0].Paths = []string{"domain/value.go"}
			repo.Config.Tests.Suites[2].Cost = cost
			repo.Config.Tests.Suites[2].Paths = append(repo.Config.Tests.Suites[2].Paths, "domain/value_test.go")
			findings := CoverageFindings(repo, files)
			if len(findings) != 1 || findings[0].Check != "policy.testOwnership" || findings[0].Subject != "domain-unit" ||
				!strings.Contains(findings[0].Message, "no owned executable test") {
				t.Fatalf("empty quick coverage admitted at cost %s: %+v", cost, findings)
			}
		})
	}
}

func TestSeparateExecutionRequiresItsReferencedFocusedSuiteToOwnTests(t *testing.T) {
	t.Parallel()
	repo, files := integrationOwnershipRepository(t)
	empty := repo.Config.Tests.Suites[0]
	empty.Name, empty.Paths = "empty-focused", []string{"domain/value.go"}
	repo.Config.Tests.Suites = append(repo.Config.Tests.Suites, empty)
	repo.Config.Tests.Ownership[1].FocusedSuite = empty.Name
	findings := CoverageFindings(repo, files)
	if len(findings) != 1 || findings[0].Subject != empty.Name || !strings.Contains(findings[0].Message, "no owned executable test") {
		t.Fatalf("unrelated quick suite concealed empty referenced suite: %+v", findings)
	}
	repo.Config.Tests.Ownership[1].FocusedSuite = "domain-unit"
	repo.Config.Tests.Ownership[0].ExecutionSuite = "domain-unit"
	if findings := CoverageFindings(repo, files); len(findings) != 0 {
		t.Fatalf("explicit quick execution rejected: %+v", findings)
	}
}

func integrationOwnershipRepository(t *testing.T) (repository.Repository, []string) {
	t.Helper()
	repo := ownershipRepository(t)
	files := []string{"domain/value.go", "domain/value_test.go", "checks/browser.test.js", "checks/browser-helper.js"}
	writeTestQualityFile(t, repo.Root, files[2], "import { exercise } from './browser-helper.js';\nexercise();\n")
	writeTestQualityFile(t, repo.Root, files[3], "export function exercise() { throw new Error('integration failure'); }\n")
	repo.Config.Tests.Paths = []string{"checks/**"}
	repo.Config.Tests.Ownership = append(repo.Config.Tests.Ownership, policy.TestOwnership{
		Paths: files[2:], Module: "domain", FocusedSuite: "domain-unit", ExecutionSuite: "browser-integration",
	})
	repo.Config.Tests.Suites = append(repo.Config.Tests.Suites, policy.TestSuite{
		Name: "browser-integration", Kind: "browser", Scope: "repository", Cost: "expensive", RunOn: []string{"full"},
		Argv: []string{"node", "--test", "checks/browser.test.js"}, Paths: files[2:],
	})
	return repo, files
}
