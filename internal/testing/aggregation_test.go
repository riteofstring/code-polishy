package testing

import (
	"context"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPlanCollapsesDeclaredCoverageAndExactDuplicateCommands(t *testing.T) {
	t.Parallel()
	config := policy.Config{Tests: policy.Testing{Suites: []policy.TestSuite{
		{Name: "component", Kind: "unit", Scope: "module", Modules: []string{"domain"}, Argv: []string{"go", "test", "./domain/..."}, Cwd: ".", RunOn: []string{"full"}, TimeoutSeconds: 900},
		{Name: "aggregate", Kind: "integration", Scope: "repository", Argv: []string{"go", "test", "./..."}, Cwd: ".", Covers: []string{"component"}, RunOn: []string{"full"}, TimeoutSeconds: 900},
		{Name: "first", Kind: "contract", Scope: "repository", Argv: []string{"go", "test", "./contract/..."}, Cwd: ".", RunOn: []string{"full"}, TimeoutSeconds: 900},
		{Name: "second", Kind: "contract", Scope: "repository", Argv: []string{"go", "test", "./contract/..."}, Cwd: ".", RunOn: []string{"full"}, TimeoutSeconds: 900},
	}}}
	plan, err := BuildPlan(repository.Repository{Config: config}, Request{Full: true})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, suite := range plan.Suites {
		names = append(names, suite.Name)
	}
	if !slices.Equal(names, []string{"aggregate", "first"}) || len(plan.Aggregations) != 2 ||
		plan.Aggregations[0] != (SuiteAggregation{Suite: "component", ExecutedBy: "aggregate", Reason: "covered"}) ||
		plan.Aggregations[1] != (SuiteAggregation{Suite: "second", ExecutedBy: "first", Reason: "duplicate-command"}) {
		t.Fatalf("plan = %+v", plan)
	}
	commandRunner := &aggregationRunner{}
	result := RunWithEvidence(t.Context(), repository.Repository{Root: t.TempDir()}, commandRunner, plan, nil)
	if len(result.Findings) != 0 || !slices.Equal(commandRunner.names, []string{"aggregate", "first"}) {
		t.Fatalf("result = %+v, commands = %v", result, commandRunner.names)
	}
}

type aggregationRunner struct{ names []string }

func (commandRunner *aggregationRunner) Run(_ context.Context, _ string, command policy.Command) error {
	commandRunner.names = append(commandRunner.names, command.Name)
	return nil
}
