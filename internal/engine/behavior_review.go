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
	commands      []MergeGateExecutionCommand
	proofCommands []MergeGateExecutionCommand
	controller    *gateRunController
	documentation bool
	reviewReport  *Report
	reviewErr     error
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
	if plan.Level == testpolicy.MergeLevelDocumentation {
		return behaviorreview.GateReplayPlan{}, nil, nil
	}
	replayPlan, err := behaviorreview.ReplayPlan(ctx, engine.Repository, behaviorreview.ValidateGateReceiptOptions{Base: plan.Selection.Base})
	if err == nil {
		return replayPlan, nil, nil
	}
	report, reportErr := engine.behaviorReviewGateFailureReport(plan, err)
	return behaviorreview.GateReplayPlan{}, report, reportErr
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
	return withCheckpointPolicy(Report{}, "unchanged", base, candidate, ""), nil
}

func (engine *Engine) prepareCheckpointGate(ctx context.Context, base string, selection repository.Selection) (checkpointGateExecution, Report, error) {
	documentation := engine.Repository.ClassifyDocumentationCandidate(selection).Ordinary
	candidate, workingTreeCandidate, err := gateCandidateIdentity(engine.Repository, selection, false)
	if err != nil {
		return checkpointGateExecution{}, Report{}, err
	}
	commands, _, err := engine.checkpointGateCommands(selection, documentation)
	if err != nil {
		return checkpointGateExecution{}, Report{}, err
	}
	state, plan := checkpointGateStateAndPlan(base, candidate, selection, documentation)
	replayPlan, reviewReport, reviewErr := engine.behaviorReviewReplayPlanReport(ctx, plan)
	proofCommands, err := gateBehaviorProofCommands(replayPlan)
	if err != nil {
		return checkpointGateExecution{}, checkpointGateReport(Report{}, state), err
	}
	controller, err := newGateRunController(engine, gaterun.CheckpointGate, base, selection.Base, candidate, state.Scope, append(proofCommands, commands...), false)
	if err != nil {
		return checkpointGateExecution{}, checkpointGateReport(Report{}, state), err
	}
	controller.workingTreeCandidate = workingTreeCandidate
	state.ReviewID = replayPlan.Receipt.ReviewID
	return checkpointGateExecution{
		state: state, selection: selection, plan: plan, commands: commands, proofCommands: proofCommands, controller: controller,
		documentation: documentation, reviewReport: reviewReport, reviewErr: reviewErr,
	}, Report{}, nil
}

func checkpointGateStateAndPlan(base, candidate string, selection repository.Selection, documentation bool) (checkpointGateState, MergeGateExecutionPlan) {
	scope := behaviorreview.CheckpointScopeChanged
	level := testpolicy.MergeLevelRecommended
	if documentation {
		scope = behaviorreview.CheckpointScopeDocumentation
		level = testpolicy.MergeLevelDocumentation
	}
	state := checkpointGateState{Base: base, Candidate: candidate, Scope: scope, ExactBase: selection.Base}
	return state, MergeGateExecutionPlan{Level: level, Selection: selection}
}

func (engine *Engine) executeCheckpointGate(ctx context.Context, execution checkpointGateExecution) (Report, error) {
	if execution.documentation {
		return engine.executeDocumentationCheckpointGate(ctx, execution)
	}
	if execution.reviewErr != nil {
		return execution.controller.finalize(engine, checkpointGateReport(Report{}, execution.state), execution.reviewErr)
	}
	if execution.reviewReport != nil {
		return execution.controller.finalize(engine, checkpointGateReport(*execution.reviewReport, execution.state), nil)
	}
	if len(execution.proofCommands) > 0 {
		if replayErr := execution.controller.replayBehaviorReview(ctx, engine, execution.selection.Base); replayErr != nil {
			return engine.finalizeCheckpointReplayFailure(execution, replayErr)
		}
	}
	return engine.executeChangedCheckpointGate(ctx, execution)
}

func (engine *Engine) executeDocumentationCheckpointGate(ctx context.Context, execution checkpointGateExecution) (Report, error) {
	report, gateErr := engine.documentationMergeGate(ctx, execution.selection)
	report = engine.withTestQualityReminder(report, execution.selection.Candidate)
	report, gateErr = engine.finishCheckpointGate(ctx, report, gateErr, execution.state)
	return execution.controller.finalize(engine, report, gateErr)
}

func (engine *Engine) finalizeCheckpointReplayFailure(execution checkpointGateExecution, replayErr error) (Report, error) {
	replayReport, gateErr := engine.behaviorReviewGateFailureReport(execution.plan, replayErr)
	report := Report{}
	if replayReport != nil {
		report = *replayReport
	}
	return execution.controller.finalize(engine, checkpointGateReport(report, execution.state), gateErr)
}

func (engine *Engine) executeChangedCheckpointGate(ctx context.Context, execution checkpointGateExecution) (Report, error) {
	plannedRunner := &mergeGatePlannedRunner{root: engine.Repository.Root, delegate: execution.controller.runner, expected: execution.commands}
	plannedEngine := *engine
	plannedEngine.Runner = plannedRunner
	report, testErr := engine.checkpointChecksAndTests(ctx, plannedEngine, execution.selection)
	if testErr != nil {
		return execution.controller.finalize(engine, checkpointGateReport(report, execution.state), testErr)
	}
	report = engine.withTestQualityReminder(report, execution.selection.Candidate)
	gateErr := checkpointPlannedRunnerError(plannedRunner, report, execution.commands)
	report, gateErr = engine.finishCheckpointGate(ctx, report, gateErr, execution.state)
	return execution.controller.finalize(engine, report, gateErr)
}

func (engine *Engine) checkpointChecksAndTests(ctx context.Context, plannedEngine Engine, selection repository.Selection) (Report, error) {
	report := plannedEngine.Check(ctx, selection, "check")
	if len(report.Findings) != 0 {
		return report, nil
	}
	tested, err := plannedEngine.test(ctx, testpolicy.Request{Changed: selection}, true)
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
		return []MergeGateExecutionCommand{}, testpolicy.Plan{}, nil
	}
	tests, err := testpolicy.BuildPlan(engine.Repository, testpolicy.Request{Changed: selection})
	if err != nil {
		return nil, testpolicy.Plan{}, err
	}
	commands := mergeGateCheckCommands(gaterun.Check, quality.CheckCommands(engine.Repository, selection, "check"))
	commands = append(commands, mergeGateSuiteCommands(tests.Suites)...)
	return commands, tests, nil
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
