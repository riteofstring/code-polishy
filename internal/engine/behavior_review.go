package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func (engine *Engine) PrepareBehaviorReview(ctx context.Context, base, intentPath string) (behaviorreview.PrepareResult, error) {
	return behaviorreview.Prepare(ctx, engine.Repository, behaviorreview.PrepareOptions{Base: base, IntentPath: intentPath})
}

func (engine *Engine) FinalizeBehaviorReview(ctx context.Context, base string) (behaviorreview.FinalizeResult, error) {
	return behaviorreview.Finalize(ctx, engine.Repository, behaviorreview.FinalizeOptions{Base: base})
}

func (engine *Engine) ProveRegression(ctx context.Context, base, suite string, evidence []string, id string, expectedRedExitStatus int) (behaviorreview.ProveResult, error) {
	commandRunner, ok := engine.Runner.(runner.OutputRunner)
	if ok {
		return behaviorreview.Prove(ctx, engine.Repository, commandRunner, behaviorreview.ProveOptions{
			Base: base, Suite: suite, Evidence: append([]string{}, evidence...), ID: id, ExpectedRedExitStatus: expectedRedExitStatus,
		})
	}
	return behaviorreview.ProveResult{}, fmt.Errorf("regression-proof requires a runner that captures command output")
}

func (engine *Engine) validateBehaviorReviewMergeReceipt(ctx context.Context, base string) (behaviorreview.MergeReceipt, error) {
	return behaviorreview.ValidateMergeReceipt(ctx, engine.Repository, behaviorreview.ValidateMergeReceiptOptions{Base: base})
}

func (engine *Engine) behaviorReviewMergeReceiptFinding(ctx context.Context, base string) (*policy.Finding, error) {
	_, err := engine.validateBehaviorReviewMergeReceipt(ctx, base)
	if err == nil {
		return nil, nil
	}
	if errors.Is(err, behaviorreview.ErrOperational) || errors.Is(err, behaviorreview.ErrCleanup) {
		return nil, err
	}
	return &policy.Finding{
		Check: "policy.behaviorReview", Path: ".code-polishy-reports/behavior-review/receipt.json", Subject: "merge-receipt", Message: err.Error(),
	}, nil
}

func (engine *Engine) behaviorReviewRequired() bool {
	configuration := engine.Repository.Config.Verification.BehaviorReview
	return configuration != nil && configuration.Required
}
