package testing

import (
	"context"
	"errors"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

func runCapturedSuite(ctx context.Context, root string, boundary runner.Runner, command policy.Command) (runner.Result, bool, error) {
	captured, ok := boundary.(runner.OutputRunner)
	if !ok {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, true, errors.New("native doctests require bounded command output")
	}
	result, output, err := captured.RunWithOutput(ctx, root, command)
	category := runner.FailureCategoryFor(ctx, result, err)
	if category == "" || category == runner.FailureCommandExit {
		if artifactErr := testartifact.CaptureCommandOutput(command, output.Stdout); artifactErr != nil {
			result.FailureCategory = runner.FailureOperational
			err = errors.Join(err, artifactErr)
		}
	}
	return result, true, err
}
