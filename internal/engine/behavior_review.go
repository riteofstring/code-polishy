package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/quality"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

type checkpointGateState struct {
	Base, Candidate, Scope, ExactBase, ReviewID string
}

type checkpointGateExecution struct {
	state         checkpointGateState
	selection     repository.Selection
	plan          MergeGateExecutionPlan
	decision      behaviorReviewDecision
	commands      []MergeGateExecutionCommand
	proofCommands []MergeGateExecutionCommand
	controller    *gateRunController
	documentation bool
	reviewReport  *Report
	reviewErr     error
}

type checkpointGatePreparation struct {
	state                checkpointGateState
	plan                 MergeGateExecutionPlan
	workingTreeCandidate bool
	documentation        bool
}

type checkpointGateReviewPreparation struct {
	replayPlan    behaviorreview.GateReplayPlan
	proofCommands []MergeGateExecutionCommand
	reviewReport  *Report
	reviewErr     error
}

func (engine *Engine) CaptureBehaviorReviewIntent(ctx context.Context, intentPath string, features []string) (behaviorreview.CaptureIntentResult, error) {
	return behaviorreview.CaptureIntent(ctx, engine.Repository, behaviorreview.CaptureIntentOptions{
		IntentPath: intentPath, Features: append([]string{}, features...),
	})
}

func (engine *Engine) PrepareBehaviorReview(ctx context.Context, base string) (behaviorreview.PrepareResult, error) {
	selection, err := engine.Repository.SelectBase(base)
	if err != nil {
		return behaviorreview.PrepareResult{}, err
	}
	decision, err := engine.behaviorReviewDecision(ctx, selection, BehaviorReviewMerge)
	if err != nil {
		return behaviorreview.PrepareResult{}, err
	}
	if !decision.required {
		return behaviorreview.PrepareResult{}, fmt.Errorf("behavior review is not selected for this candidate")
	}
	return behaviorreview.Prepare(ctx, engine.Repository, behaviorreview.PrepareOptions{Base: base, Selection: decision.selection})
}

func (engine *Engine) RequireBehaviorReview(ctx context.Context, base string, features []string) (behaviorreview.RequireResult, error) {
	return behaviorreview.Require(ctx, engine.Repository, behaviorreview.RequireOptions{Base: base, Features: append([]string{}, features...)})
}

func (engine *Engine) BehaviorReviewStatus(ctx context.Context, base string) (BehaviorReviewStatus, error) {
	selection, err := engine.Repository.SelectBase(base)
	if err != nil {
		return BehaviorReviewStatus{}, err
	}
	decision, err := engine.behaviorReviewDecision(ctx, selection, BehaviorReviewMerge)
	if err != nil {
		return BehaviorReviewStatus{}, err
	}
	if !decision.required {
		return cloneBehaviorReviewStatus(decision.status), nil
	}
	receipt, err := behaviorreview.ValidateGateReceipt(ctx, engine.Repository, behaviorreview.ValidateGateReceiptOptions{
		Base: selection.Base, Selection: decision.selection,
	})
	if err == nil {
		return decision.withState(BehaviorReviewPassed, receipt.ReviewID, behaviorReviewReceiptPath), nil
	}
	if errors.Is(err, behaviorreview.ErrOperational) || errors.Is(err, behaviorreview.ErrCleanup) {
		return BehaviorReviewStatus{}, err
	}
	if errors.Is(err, behaviorreview.ErrMissingReceipt) {
		status := decision.withState(BehaviorReviewRequired, "", "")
		return engine.withFinalStateFindings(ctx, selection, decision, status), nil
	}
	if errors.Is(err, repository.ErrDirtyCandidate) {
		return decision.withState(BehaviorReviewRequired, "", ""), nil
	}
	status := decision.withState(BehaviorReviewFailed, "", "")
	return engine.withFinalStateFindings(ctx, selection, decision, status), nil
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

func (engine *Engine) behaviorReviewReplayPlanReport(ctx context.Context, plan MergeGateExecutionPlan) (behaviorreview.GateReplayPlan, *Report, error) {
	if !plan.BehaviorReview.required {
		return behaviorreview.GateReplayPlan{}, nil, nil
	}
	replayPlan, err := behaviorreview.ReplayPlan(ctx, engine.Repository, behaviorreview.ValidateGateReceiptOptions{
		Base: plan.Selection.Base, Selection: plan.BehaviorReview.selection,
	})
	if err == nil {
		return replayPlan, nil, nil
	}
	report, reportErr := engine.behaviorReviewGateFailureReport(ctx, plan, err)
	return behaviorreview.GateReplayPlan{}, report, reportErr
}

func (engine *Engine) behaviorReviewGateFailureReport(ctx context.Context, plan MergeGateExecutionPlan, reviewErr error) (*Report, error) {
	status := plan.BehaviorReview.withState(BehaviorReviewFailed, "", "")
	if errors.Is(reviewErr, behaviorreview.ErrMissingReceipt) {
		status = plan.BehaviorReview.withState(BehaviorReviewRequired, "", "")
	}
	status = engine.withFinalStateFindings(ctx, plan.Selection, plan.BehaviorReview, status)
	finding, err := behaviorReviewGateReceiptFinding(reviewErr)
	if err != nil || finding == nil {
		return nil, err
	}
	report := engine.finish([]policy.Finding{*finding}, nil)
	report = withBehaviorReview(report, status)
	report = engine.withTestQualityReminder(report, plan.Selection.Candidate)
	return &report, nil
}

func (engine *Engine) withFinalStateFindings(ctx context.Context, selection repository.Selection, decision behaviorReviewDecision, status BehaviorReviewStatus) BehaviorReviewStatus {
	findings, err := behaviorreview.FinalStateFindings(ctx, engine.Repository, behaviorreview.ValidateGateReceiptOptions{
		Base: selection.Base, Selection: decision.selection,
	})
	if err != nil {
		return status
	}
	status.FinalStateFindings = findings
	if len(findings) == 0 {
		status.FinalStateState = BehaviorReviewPassed
		return status
	}
	status.State = BehaviorReviewFailed
	status.FinalStateState = BehaviorReviewFailed
	return status
}

func (engine *Engine) CheckpointGate(ctx context.Context, base string) (Report, error) {
	selection, err := engine.Repository.SelectBase(base)
	if err != nil {
		return Report{}, err
	}
	if len(selection.Candidate.Paths()) == 0 {
		return engine.unchangedCheckpointGate(base)
	}
	execution, failure, err := engine.prepareCheckpointGate(ctx, base, selection)
	if err != nil {
		return failure, err
	}
	return engine.executeCheckpointGate(ctx, execution)
}

func (engine *Engine) unchangedCheckpointGate(base string) (Report, error) {
	candidate, err := engine.Repository.CleanHead()
	if err != nil {
		return Report{}, err
	}
	digest, err := emptyBehaviorReviewSelectionDigest()
	if err != nil {
		return Report{}, err
	}
	report := withCheckpointPolicy(Report{}, "unchanged", base, candidate, "")
	return withBehaviorReview(report, BehaviorReviewStatus{
		State: BehaviorReviewNotRun, RequiredBoundary: BehaviorReviewOnRequest,
		SelectedFeatures: []BehaviorReviewFeatureSelection{}, SelectionDigest: digest,
		Affected: []string{}, Configured: []string{}, TaskRequested: []string{}, Required: []string{}, Completed: []string{}, Missing: []string{},
	}), nil
}

func (engine *Engine) prepareCheckpointGate(ctx context.Context, base string, selection repository.Selection) (checkpointGateExecution, Report, error) {
	preparation, err := engine.checkpointGatePreparation(ctx, base, selection)
	if err != nil {
		return checkpointGateExecution{}, Report{}, err
	}
	review, err := engine.checkpointGateReviewPreparation(ctx, preparation.plan)
	if err != nil {
		return checkpointGateExecution{}, checkpointGateReport(Report{}, preparation.state), err
	}
	initialStatus := checkpointGateInitialBehaviorReviewStatus(preparation.plan.BehaviorReview, review)
	controller, err := newGateRunController(
		engine, gaterun.CheckpointGate, base, selection.Base, preparation.state.Candidate, preparation.state.Scope,
		append(review.proofCommands, preparation.plan.Commands...), initialStatus, false,
	)
	if err != nil {
		return checkpointGateExecution{}, checkpointGateReport(Report{}, preparation.state), err
	}
	controller.workingTreeCandidate = preparation.workingTreeCandidate
	preparation.state.ReviewID = review.replayPlan.Receipt.ReviewID
	return checkpointGateExecution{
		state: preparation.state, selection: selection, plan: preparation.plan, decision: preparation.plan.BehaviorReview,
		commands: preparation.plan.Commands, proofCommands: review.proofCommands, controller: controller,
		documentation: preparation.documentation, reviewReport: review.reviewReport, reviewErr: review.reviewErr,
	}, Report{}, nil
}

func (engine *Engine) checkpointGatePreparation(ctx context.Context, base string, selection repository.Selection) (checkpointGatePreparation, error) {
	documentation := engine.Repository.ClassifyDocumentationCandidate(selection).Ordinary
	candidate, workingTreeCandidate, err := gateCandidateIdentity(engine.Repository, selection, false)
	if err != nil {
		return checkpointGatePreparation{}, err
	}
	decision, err := engine.behaviorReviewDecision(ctx, selection, BehaviorReviewCheckpoint)
	if err != nil {
		return checkpointGatePreparation{}, err
	}
	commands, tests, err := engine.checkpointGateCommands(selection, documentation)
	if err != nil {
		return checkpointGatePreparation{}, err
	}
	tests, err = engine.forceBehaviorReviewSuites(tests, decision)
	if err != nil {
		return checkpointGatePreparation{}, err
	}
	commands = append(commands, mergeGateSuiteCommands(tests.Suites)...)
	state, plan := checkpointGateStateAndPlan(base, candidate, selection, documentation, decision, tests, commands)
	return checkpointGatePreparation{
		state: state, plan: plan, workingTreeCandidate: workingTreeCandidate, documentation: documentation,
	}, nil
}

func (engine *Engine) checkpointGateReviewPreparation(ctx context.Context, plan MergeGateExecutionPlan) (checkpointGateReviewPreparation, error) {
	replayPlan, reviewReport, reviewErr := engine.behaviorReviewReplayPlanReport(ctx, plan)
	proofCommands, err := gateBehaviorProofCommands(replayPlan)
	if err != nil {
		return checkpointGateReviewPreparation{}, err
	}
	return checkpointGateReviewPreparation{
		replayPlan: replayPlan, proofCommands: proofCommands, reviewReport: reviewReport, reviewErr: reviewErr,
	}, nil
}

func checkpointGateInitialBehaviorReviewStatus(decision behaviorReviewDecision, review checkpointGateReviewPreparation) BehaviorReviewStatus {
	if review.reviewReport != nil && review.reviewReport.BehaviorReview != nil {
		return *review.reviewReport.BehaviorReview
	}
	if review.reviewErr == nil && review.reviewReport == nil && decision.required {
		return decision.withState(BehaviorReviewPassed, review.replayPlan.Receipt.ReviewID, behaviorReviewReceiptPath)
	}
	return decision.status
}

func checkpointGateStateAndPlan(base, candidate string, selection repository.Selection, documentation bool, decision behaviorReviewDecision, tests testpolicy.Plan, commands []MergeGateExecutionCommand) (checkpointGateState, MergeGateExecutionPlan) {
	scope := behaviorreview.CheckpointScopeChanged
	level := testpolicy.MergeLevelRecommended
	if documentation {
		scope = behaviorreview.CheckpointScopeDocumentation
		level = testpolicy.MergeLevelDocumentation
	}
	state := checkpointGateState{Base: base, Candidate: candidate, Scope: scope, ExactBase: selection.Base}
	return state, MergeGateExecutionPlan{Level: level, Selection: selection, Tests: tests, Commands: commands, BehaviorReview: decision}
}

func (engine *Engine) executeCheckpointGate(ctx context.Context, execution checkpointGateExecution) (Report, error) {
	if execution.reviewErr != nil {
		report := withBehaviorReview(checkpointGateReport(Report{}, execution.state), execution.controller.behaviorStatus)
		return execution.controller.finalize(engine, report, execution.reviewErr)
	}
	if execution.reviewReport != nil {
		return execution.controller.finalize(engine, checkpointGateReport(*execution.reviewReport, execution.state), nil)
	}
	if len(execution.proofCommands) > 0 {
		if replayErr := execution.controller.replayBehaviorReview(ctx, engine, execution.selection.Base, execution.plan.BehaviorReview.selection); replayErr != nil {
			return engine.finalizeCheckpointReplayFailure(ctx, execution, replayErr)
		}
	}
	if execution.documentation {
		return engine.executeDocumentationCheckpointGate(ctx, execution)
	}
	return engine.executeChangedCheckpointGate(ctx, execution)
}

func (engine *Engine) executeDocumentationCheckpointGate(ctx context.Context, execution checkpointGateExecution) (Report, error) {
	report, gateErr := engine.documentationMergeGate(ctx, execution.selection)
	if gateErr == nil && len(report.Findings) == 0 && len(execution.plan.Tests.Suites) > 0 {
		plannedRunner := &mergeGatePlannedRunner{root: engine.Repository.Root, delegate: execution.controller.runner, expected: execution.commands}
		plannedEngine := *engine
		plannedEngine.Runner = plannedRunner
		tested, testErr := plannedEngine.testExactPlan(ctx, execution.plan.Tests, execution.selection, true)
		report = engine.combine(report, tested)
		gateErr = checkpointPlannedRunnerError(plannedRunner, report, execution.commands)
		if testErr != nil {
			gateErr = errors.Join(gateErr, testErr)
		}
	}
	report = engine.withTestQualityReminder(report, execution.selection.Candidate)
	report = withBehaviorReview(report, execution.controller.behaviorStatus)
	return engine.finalizeAcceptedCheckpointGate(ctx, execution.controller, report, gateErr, execution.state)
}

func (engine *Engine) finalizeCheckpointReplayFailure(ctx context.Context, execution checkpointGateExecution, replayErr error) (Report, error) {
	replayReport, gateErr := engine.behaviorReviewGateFailureReport(ctx, execution.plan, replayErr)
	report := Report{}
	if replayReport != nil {
		report = *replayReport
	}
	report = withBehaviorReview(report, execution.decision.withState(BehaviorReviewFailed, execution.controller.behaviorReview.ReviewID, execution.controller.behaviorReview.ReceiptPath))
	return execution.controller.finalize(engine, checkpointGateReport(report, execution.state), gateErr)
}

func (engine *Engine) executeChangedCheckpointGate(ctx context.Context, execution checkpointGateExecution) (Report, error) {
	plannedRunner := &mergeGatePlannedRunner{root: engine.Repository.Root, delegate: execution.controller.runner, expected: execution.commands}
	plannedEngine := *engine
	plannedEngine.Runner = plannedRunner
	report, testErr := engine.checkpointChecksAndTests(ctx, plannedEngine, execution.selection, execution.plan.Tests)
	if testErr != nil {
		report = withBehaviorReview(report, execution.controller.behaviorStatus)
		return execution.controller.finalize(engine, checkpointGateReport(report, execution.state), testErr)
	}
	report = engine.withTestQualityReminder(report, execution.selection.Candidate)
	report = withBehaviorReview(report, execution.controller.behaviorStatus)
	gateErr := checkpointPlannedRunnerError(plannedRunner, report, execution.commands)
	return engine.finalizeAcceptedCheckpointGate(ctx, execution.controller, report, gateErr, execution.state)
}

func (engine *Engine) checkpointChecksAndTests(ctx context.Context, plannedEngine Engine, selection repository.Selection, tests testpolicy.Plan) (Report, error) {
	report := plannedEngine.Check(ctx, selection, "check")
	if len(report.Findings) != 0 {
		return report, nil
	}
	tested, err := plannedEngine.testExactPlan(ctx, tests, selection, true)
	return engine.combine(report, tested), err
}

func checkpointPlannedRunnerError(commandRunner *mergeGatePlannedRunner, report Report, commands []MergeGateExecutionCommand) error {
	if commandRunner.err != nil || len(report.Findings) != 0 || commandRunner.next == len(commands) {
		return commandRunner.err
	}
	return fmt.Errorf("checkpoint gate completed before planned command %d of %d", commandRunner.next+1, len(commands))
}

func checkpointGateReport(report Report, state checkpointGateState) Report {
	return withCheckpointPolicy(report, state.Scope, state.Base, state.Candidate, "")
}

func (engine *Engine) checkpointGateCommands(selection repository.Selection, documentation bool) ([]MergeGateExecutionCommand, testpolicy.Plan, error) {
	if documentation {
		return []MergeGateExecutionCommand{}, testpolicy.Plan{Suites: []policy.TestSuite{}}, nil
	}
	tests, err := testpolicy.BuildPlan(engine.Repository, testpolicy.Request{Changed: selection})
	if err != nil {
		return nil, testpolicy.Plan{}, err
	}
	return mergeGateCheckCommands(gaterun.Check, quality.CheckCommands(engine.Repository, selection, "check")), tests, nil
}

func (engine *Engine) finalizeAcceptedCheckpointGate(ctx context.Context, controller *gateRunController, report Report, gateErr error, state checkpointGateState) (Report, error) {
	report = checkpointGateReport(report, state)
	if report.BehaviorReview == nil {
		report = withBehaviorReview(report, controller.behaviorStatus)
	}
	if gateErr != nil || len(report.Findings) != 0 {
		return controller.finalize(engine, report, gateErr)
	}
	prepared, err := controller.preparePassed(engine, &report)
	if err != nil {
		return controller.finalizeOperational(report, err)
	}
	recorded, err := behaviorreview.RecordCheckpoint(ctx, engine.Repository, behaviorreview.RecordCheckpointOptions{
		Base: state.ExactBase, Candidate: state.Candidate, Scope: state.Scope, BehaviorReview: controller.behaviorReview, GateRun: prepared.Evidence(),
	})
	if err != nil {
		return controller.finalizeOperational(report, errors.Join(err, prepared.Abort()))
	}
	final, err := prepared.Commit()
	if err != nil {
		return controller.finalizeOperational(report, errors.Join(err, prepared.Discard()))
	}
	report.GateRunPolicy = controller.gateRunPolicy(gaterun.RunPassed, final)
	report.Notes = append(report.Notes, "recorded accepted checkpoint "+state.Candidate)
	return withCheckpointPolicy(report, state.Scope, state.Base, state.Candidate, recorded.ReceiptPath), nil
}
