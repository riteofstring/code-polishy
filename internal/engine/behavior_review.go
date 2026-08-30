package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
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

func (engine *Engine) replayBehaviorReviewMergeReceipt(ctx context.Context, base string, receipt behaviorreview.MergeReceipt) error {
	if len(receipt.Proofs) == 0 {
		return nil
	}
	commandRunner, ok := engine.Runner.(runner.OutputRunner)
	if !ok {
		return fmt.Errorf("%w: behavior review proof replay requires a runner that captures command output", behaviorreview.ErrOperational)
	}
	_, err := behaviorreview.ReplayMergeReceipt(ctx, engine.Repository, commandRunner, behaviorreview.ValidateMergeReceiptOptions{Base: base})
	return err
}

func behaviorReviewMergeReceiptFinding(err error) (*policy.Finding, error) {
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

func (engine *Engine) behaviorReviewMergeGateReport(ctx context.Context, plan MergeGateExecutionPlan) (*Report, error) {
	if plan.Level == testpolicy.MergeLevelDocumentation {
		return nil, nil
	}
	receipt, err := engine.validateBehaviorReviewMergeReceipt(ctx, plan.Selection.Base)
	if err != nil {
		return engine.behaviorReviewMergeGateFailureReport(plan, err)
	}
	if err := engine.replayBehaviorReviewMergeReceipt(ctx, plan.Selection.Base, receipt); err != nil {
		return engine.behaviorReviewMergeGateFailureReport(plan, err)
	}
	return nil, nil
}

func (engine *Engine) behaviorReviewMergeGateFailureReport(plan MergeGateExecutionPlan, reviewErr error) (*Report, error) {
	finding, err := behaviorReviewMergeReceiptFinding(reviewErr)
	if err != nil || finding == nil {
		return nil, err
	}
	report := engine.finish([]policy.Finding{*finding}, nil)
	report = engine.withTestQualityReminder(report, plan.Selection.Candidate)
	return &report, nil
}
