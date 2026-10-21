package testing

import (
	"context"
	"errors"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

func runCapturedSuite(ctx context.Context, root string, boundary runner.Runner, command policy.Command) (runner.Result, error) {
	result, output, err := runner.RunCaptured(ctx, boundary, root, command, runner.CaptureOutput)
	category := runner.FailureCategoryFor(ctx, result, err)
	if category == "" || category == runner.FailureCommandExit {
		if artifactErr := testartifact.CaptureCommandOutput(command, output.Stdout); artifactErr != nil {
			result.FailureCategory = runner.FailureOperational
			err = errors.Join(err, artifactErr)
		}
	}
	return result, err
}
