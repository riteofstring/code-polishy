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

type checkpointGateState struct {
	Base, Candidate, Scope, ExactBase, ReviewID string
}

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

func (engine *Engine) validateBehaviorReviewGateReceipt(ctx context.Context, base string) (behaviorreview.GateReceipt, error) {
	return behaviorreview.ValidateGateReceipt(ctx, engine.Repository, behaviorreview.ValidateGateReceiptOptions{Base: base})
}

func (engine *Engine) replayBehaviorReviewGateReceipt(ctx context.Context, base string, receipt behaviorreview.GateReceipt) error {
	if len(receipt.Proofs) == 0 {
		return nil
	}
	commandRunner, ok := engine.Runner.(runner.OutputRunner)
	if !ok {
		return fmt.Errorf("%w: behavior review proof replay requires a runner that captures command output", behaviorreview.ErrOperational)
	}
	_, err := behaviorreview.ReplayGateReceipt(ctx, engine.Repository, commandRunner, behaviorreview.ValidateGateReceiptOptions{Base: base})
	return err
}

func behaviorReviewGateReceiptFinding(err error) (*policy.Finding, error) {
	if err == nil {
		return nil, nil
	}
	if errors.Is(err, behaviorreview.ErrOperational) || errors.Is(err, behaviorreview.ErrCleanup) {
		return nil, err
	}
	return &policy.Finding{
		Check: "policy.behaviorReview", Path: ".code-polishy-reports/behavior-review/receipt.json", Subject: "gate-receipt", Message: err.Error(),
	}, nil
}

func (engine *Engine) behaviorReviewGateReport(ctx context.Context, plan MergeGateExecutionPlan) (behaviorreview.GateReceipt, *Report, error) {
	if plan.Level == testpolicy.MergeLevelDocumentation {
		return behaviorreview.GateReceipt{}, nil, nil
	}
	receipt, err := engine.validateBehaviorReviewGateReceipt(ctx, plan.Selection.Base)
	if err != nil {
		report, reportErr := engine.behaviorReviewGateFailureReport(plan, err)
		return behaviorreview.GateReceipt{}, report, reportErr
	}
	if err := engine.replayBehaviorReviewGateReceipt(ctx, plan.Selection.Base, receipt); err != nil {
		report, reportErr := engine.behaviorReviewGateFailureReport(plan, err)
		return behaviorreview.GateReceipt{}, report, reportErr
	}
	return receipt, nil, nil
}

func (engine *Engine) behaviorReviewGateFailureReport(plan MergeGateExecutionPlan, reviewErr error) (*Report, error) {
	finding, err := behaviorReviewGateReceiptFinding(reviewErr)
	if err != nil || finding == nil {
		return nil, err
	}
	report := engine.finish([]policy.Finding{*finding}, nil)
	report = engine.withTestQualityReminder(report, plan.Selection.Candidate)
	return &report, nil
}

func (engine *Engine) CheckpointGate(ctx context.Context, base string) (Report, error) {
	candidate, err := engine.Repository.CleanHead()
	if err != nil {
		return Report{}, err
	}
	selection, err := engine.Repository.SelectBase(base)
	if err != nil {
		return Report{}, err
	}
	if len(selection.Candidate.Paths()) == 0 {
		return withCheckpointPolicy(Report{}, "unchanged", base, candidate, ""), nil
	}
	classification := engine.Repository.ClassifyDocumentationCandidate(selection)
	if classification.Ordinary {
		report, gateErr := engine.documentationMergeGate(ctx, selection)
		report = engine.withTestQualityReminder(report, selection.Candidate)
		state := checkpointGateState{Base: base, Candidate: candidate, Scope: behaviorreview.CheckpointScopeDocumentation, ExactBase: selection.Base}
		return engine.finishCheckpointGate(ctx, report, gateErr, state)
	}
	plan := MergeGateExecutionPlan{Level: testpolicy.MergeLevelRecommended, Selection: selection}
	receipt, reviewReport, reviewErr := engine.behaviorReviewGateReport(ctx, plan)
	if reviewErr != nil {
		return withCheckpointPolicy(Report{}, behaviorreview.CheckpointScopeChanged, base, candidate, ""), reviewErr
	}
	if reviewReport != nil {
		return withCheckpointPolicy(*reviewReport, behaviorreview.CheckpointScopeChanged, base, candidate, ""), nil
	}
	checked := engine.Check(ctx, selection, "check")
	report := checked
	if len(checked.Findings) == 0 {
		tested, testErr := engine.test(ctx, testpolicy.Request{Changed: selection}, true)
		report = engine.combine(report, tested)
		if testErr != nil {
			return withCheckpointPolicy(report, behaviorreview.CheckpointScopeChanged, base, candidate, ""), testErr
		}
	}
	report = engine.withTestQualityReminder(report, selection.Candidate)
	state := checkpointGateState{
		Base: base, Candidate: candidate, Scope: behaviorreview.CheckpointScopeChanged,
		ExactBase: selection.Base, ReviewID: receipt.ReviewID,
	}
	return engine.finishCheckpointGate(ctx, report, nil, state)
}

func (engine *Engine) finishCheckpointGate(ctx context.Context, report Report, gateErr error, state checkpointGateState) (Report, error) {
	if gateErr != nil || len(report.Findings) > 0 {
		return withCheckpointPolicy(report, state.Scope, state.Base, state.Candidate, ""), gateErr
	}
	recorded, err := behaviorreview.RecordCheckpoint(ctx, engine.Repository, behaviorreview.RecordCheckpointOptions{
		Base: state.ExactBase, Candidate: state.Candidate, Scope: state.Scope, BehaviorReviewID: state.ReviewID,
	})
	if err != nil {
		return withCheckpointPolicy(report, state.Scope, state.Base, state.Candidate, ""), err
	}
	report.Notes = append(report.Notes, "recorded accepted checkpoint "+state.Candidate)
	return withCheckpointPolicy(report, state.Scope, state.Base, state.Candidate, recorded.ReceiptPath), nil
}
