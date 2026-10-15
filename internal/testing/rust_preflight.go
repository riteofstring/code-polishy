package testing

import (
	"context"
	"errors"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func SuitePrerequisiteCommands(repo repository.Repository, suite policy.TestSuite) []policy.Command {
	if suite.Rust == nil {
		return nil
	}
	commands := []policy.Command{rustfacts.TestWorkspaceCommand(repo, suite.Name+"-workspace", suite.Rust.Manifest)}
	if suite.Reusable {
		commands = append(commands, rustfacts.TestWorkspaceCommand(repo, suite.Name+"-view-workspace", suite.Rust.Manifest))
	}
	return commands
}

func prepareRustSuite(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, suite policy.TestSuite, reporter *ExecutionReporter, offset int) (string, func() error, runner.Result, error) {
	root, cleanup := repo.Root, func() error { return nil }
	result := runner.Result{ExitStatus: -1}
	if suite.Rust == nil {
		return root, cleanup, result, nil
	}
	var source rustfacts.Workspace
	for index, command := range SuitePrerequisiteCommands(repo, suite) {
		if index > 0 {
			var err error
			root, cleanup, err = suiteExecutionRoot(repo.Root, commandRunner, suite)
			if err != nil {
				return "", nil, result, err
			}
		}
		var err error
		runContext := suiteRunContext(ctx, reporter, offset+index)
		var workspace rustfacts.Workspace
		workspace, result, err = rustfacts.ValidateTestWorkspace(runContext, repo, root, *suite.Rust, command, commandRunner)
		if err == nil && index > 0 && !rustfacts.SameTestWorkspace(source, workspace) {
			err = errors.New("reusable Rust view workspace differs from its source workspace; declare every Cargo input")
		}
		source = workspace
		reportPreparation(reporter, offset+index, result, err)
		if err != nil {
			return "", nil, result, errors.Join(err, cleanup())
		}
	}
	return root, cleanup, result, nil
}

func failedSuitePreparation(ctx context.Context, suite policy.TestSuite, attempt int, result runner.Result, err error) SuiteExecution {
	execution := failedSuiteExecution(suite, attempt, err)
	category := runner.FailureCategoryFor(ctx, result, err)
	if category == runner.FailureCanceled || category == runner.FailureTimeout {
		execution.FailureCategory = category
		execution.Result.FailureCategory = category
	}
	return execution
}

func reportPreparation(reporter *ExecutionReporter, index int, result runner.Result, err error) {
	execution := SuiteExecution{Result: result, ResultKnown: true}
	if err != nil {
		execution.FailureMessage = err.Error()
	}
	reportSuiteExecution(reporter, index, execution)
}
