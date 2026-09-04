package testing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

func TestRunReusesSuiteWithoutStartingCommandOrArtifactExecution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	suite := reusableSuiteFixture()
	commandRunner := &reuseFixtureRunner{reusable: true}
	result := RunWithEvidence(t.Context(), repository.Repository{Root: root}, commandRunner, Plan{Suites: []policy.TestSuite{suite}}, nil)
	if len(result.Findings) != 0 || len(result.Executions) != 1 || !result.Executions[0].Reused || commandRunner.runs != 0 {
		t.Fatalf("result = %+v, runs = %d", result, commandRunner.runs)
	}
	if _, err := os.Stat(filepath.Join(root, testartifact.RootName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reused suite created artifact execution: %v", err)
	}
}

func TestRunRecordsFreshPassingSuite(t *testing.T) {
	t.Parallel()
	commandRunner := &reuseFixtureRunner{}
	result := RunWithEvidence(t.Context(), repository.Repository{Root: t.TempDir()}, commandRunner, Plan{Suites: []policy.TestSuite{reusableSuiteFixture()}}, nil)
	if len(result.Findings) != 0 || commandRunner.runs != 1 || commandRunner.recorded != 1 {
		t.Fatalf("result = %+v, runner = %+v", result, commandRunner)
	}
}

type reuseFixtureRunner struct {
	reusable bool
	runs     int
	recorded int
}

func (commandRunner *reuseFixtureRunner) Run(context.Context, string, policy.Command) error {
	commandRunner.runs++
	return nil
}

func (commandRunner *reuseFixtureRunner) ReuseSuite(suite policy.TestSuite, attempt int) (SuiteExecution, bool, error) {
	if !commandRunner.reusable {
		return SuiteExecution{}, false, nil
	}
	return SuiteExecution{
		Suite: suite, Result: runner.Result{ExitStatus: 0, ExecutionDuration: 2 * time.Second}, ResultKnown: true,
		Attempt: attempt, ReceiptPath: ".code-polishy-reports/test-receipts/source.json", ReceiptSHA256: "digest",
	}, true, nil
}

func (commandRunner *reuseFixtureRunner) RecordSuite(execution SuiteExecution) error {
	commandRunner.recorded++
	return nil
}

func reusableSuiteFixture() policy.TestSuite {
	return policy.TestSuite{
		Name: "unit", Kind: "unit", Scope: "repository", Cost: "quick", Argv: []string{"true"}, Cwd: ".",
		RunOn: []string{"full"}, ExclusiveResources: []string{}, TimeoutSeconds: 30,
	}
}
