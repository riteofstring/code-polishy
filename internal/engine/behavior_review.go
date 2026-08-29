package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

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
	required, err := engine.behaviorReviewRequired(plan.Selection.Base)
	if err != nil || !required {
		return nil, err
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

func (engine *Engine) behaviorReviewRequired(base string) (bool, error) {
	baseRequired, err := engine.behaviorReviewRequiredAt(base)
	if err != nil {
		return false, err
	}
	return baseRequired || behaviorReviewConfigured(engine.Repository.Config), nil
}

func (engine *Engine) behaviorReviewRequiredAt(revision string) (bool, error) {
	configPath, err := engine.configuredPolicyPath()
	if err != nil {
		return false, fmt.Errorf("resolve behavior-review configuration path: %w", err)
	}
	baseRepository := engine.Repository
	baseRepository.Config = policy.Config{}
	paths, err := baseRepository.FilesAt(revision)
	if err != nil {
		return false, fmt.Errorf("list behavior-review configuration at %s: %w", revision, err)
	}
	if !slices.Contains(paths, configPath) {
		return false, nil
	}
	data, err := baseRepository.ReadAt(revision, configPath)
	if err != nil {
		return false, fmt.Errorf("read behavior-review configuration at %s: %w", revision, err)
	}
	configuration, err := policy.Parse(data, configPath)
	if err != nil {
		return false, fmt.Errorf("parse behavior-review configuration at %s: %w", revision, err)
	}
	return behaviorReviewConfigured(configuration), nil
}

func (engine *Engine) configuredPolicyPath() (string, error) {
	path := engine.Repository.Config.ConfigPath
	if path == "" {
		path = policy.ConfigFilename
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(engine.Repository.Root, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return engine.Repository.NormalizePath(resolved)
}

func behaviorReviewConfigured(configuration policy.Config) bool {
	review := configuration.Verification.BehaviorReview
	return review != nil && review.Required
}
