package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/riteofstring/code-polishy/internal/artifactsecurity"
	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/quality"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/supplychain"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

type MergeGateExecutionPlan struct {
	Level          string
	Reasons        []string
	Selection      repository.Selection
	Tests          testpolicy.Plan
	Commands       []MergeGateExecutionCommand
	BehaviorReview behaviorReviewDecision
}

type MergeGateExecutionCommand struct {
	Category gaterun.CommandCategory
	Kind     string
	Scope    string
	Cost     string
	Command  policy.Command
}

type mergeGateExecution struct {
	controller    *gateRunController
	proofCommands []MergeGateExecutionCommand
	reviewReport  *Report
	reviewErr     error
}

func (engine *Engine) Gate(ctx context.Context) (Report, error) {
	report, err := engine.fullGate(ctx)
	return withMergePolicy(report, testpolicy.MergeLevelFull, "", []string{"complete gate requested"}), err
}

func (engine *Engine) MergeGate(ctx context.Context, base string) (Report, error) {
	return engine.MergeGateWithOptions(ctx, base, MergeGateOptions{})
}

func (engine *Engine) MergeGateWithOptions(ctx context.Context, base string, options MergeGateOptions) (Report, error) {
	plan, err := engine.PlanMergeGateExecution(base)
	if err != nil {
		return Report{}, err
	}
	return engine.executeMergeGatePlan(ctx, base, plan, options)
}

func (engine *Engine) executeMergeGatePlan(ctx context.Context, base string, plan MergeGateExecutionPlan, options MergeGateOptions) (Report, error) {
	execution, err := engine.prepareMergeGateExecution(ctx, base, plan, options)
	if err != nil {
		return withMergePolicy(Report{}, plan.Level, base, plan.Reasons), err
	}
	if execution.reviewErr != nil {
		return engine.finalizeMergeGateReviewError(ctx, base, plan, execution)
	}
	if execution.reviewReport != nil {
		report := engine.withMergeGateMetadata(ctx, *execution.reviewReport, base, plan)
		return execution.controller.finalize(engine, report, nil)
	}
	return engine.executeMergeGateCommands(ctx, base, plan, execution)
}

func (engine *Engine) prepareMergeGateExecution(ctx context.Context, base string, plan MergeGateExecutionPlan, options MergeGateOptions) (mergeGateExecution, error) {
	candidate, workingTreeCandidate, err := gateCandidateIdentity(engine.Repository, plan.Selection, plan.Level == testpolicy.MergeLevelDocumentation)
	if err != nil {
		return mergeGateExecution{}, err
	}
	replayPlan, reviewReport, reviewErr := engine.behaviorReviewReplayPlanReport(ctx, plan)
	proofCommands, err := gateBehaviorProofCommands(replayPlan)
	if err != nil {
		return mergeGateExecution{}, err
	}
	gateCommands := append(proofCommands, plan.Commands...)
	initialStatus := plan.BehaviorReview.status
	if reviewReport != nil && reviewReport.BehaviorReview != nil {
		initialStatus = *reviewReport.BehaviorReview
	}
	if reviewErr == nil && reviewReport == nil && plan.BehaviorReview.required {
		initialStatus = plan.BehaviorReview.withState(BehaviorReviewPassed, replayPlan.Receipt.ReviewID, behaviorReviewReceiptPath)
	}
	controller, err := newGateRunController(engine, gaterun.MergeGate, base, plan.Selection.Base, candidate, plan.Level, gateCommands, initialStatus, options.Resume)
	if err != nil {
		return mergeGateExecution{}, err
	}
	controller.workingTreeCandidate = workingTreeCandidate
	return mergeGateExecution{
		controller: controller, proofCommands: proofCommands, reviewReport: reviewReport, reviewErr: reviewErr,
	}, nil
}

func (engine *Engine) finalizeMergeGateReviewError(ctx context.Context, base string, plan MergeGateExecutionPlan, execution mergeGateExecution) (Report, error) {
	report := withBehaviorReview(Report{}, execution.controller.behaviorStatus)
	report = engine.withMergeGateMetadata(ctx, report, base, plan)
	return execution.controller.finalize(engine, report, execution.reviewErr)
}

func (engine *Engine) executeMergeGateCommands(ctx context.Context, base string, plan MergeGateExecutionPlan, execution mergeGateExecution) (Report, error) {
	if replayErr := execution.replayBehaviorReview(ctx, engine, plan.Selection.Base, plan.BehaviorReview.selection); replayErr != nil {
		return engine.finalizeMergeGateReplayFailure(ctx, base, plan, execution.controller, replayErr)
	}
	return engine.executePlannedMergeGate(ctx, base, plan, execution.controller)
}

func (execution mergeGateExecution) replayBehaviorReview(
	ctx context.Context, engine *Engine, base string, selection behaviorreview.ReviewSelection,
) error {
	if len(execution.proofCommands) == 0 {
		return nil
	}
	return execution.controller.replayBehaviorReview(ctx, engine, base, selection)
}

func (engine *Engine) finalizeMergeGateReplayFailure(ctx context.Context, base string, plan MergeGateExecutionPlan, controller *gateRunController, replayErr error) (Report, error) {
	replayReport, replayReportErr := engine.behaviorReviewGateFailureReport(plan, replayErr)
	report := Report{}
	if replayReport != nil {
		report = *replayReport
	}
	report = withBehaviorReview(report, plan.BehaviorReview.withState(
		BehaviorReviewFailed, controller.behaviorReview.ReviewID, controller.behaviorReview.ReceiptPath,
	))
	report = engine.withMergeGateMetadata(ctx, report, base, plan)
	return controller.finalize(engine, report, replayReportErr)
}

func (engine *Engine) executePlannedMergeGate(ctx context.Context, base string, plan MergeGateExecutionPlan, controller *gateRunController) (Report, error) {
	plannedRunner := &mergeGatePlannedRunner{root: engine.Repository.Root, delegate: controller.runner, expected: plan.Commands}
	plannedEngine := *engine
	plannedEngine.Runner = plannedRunner
	report, gateErr := plannedEngine.runMergeGateLevel(ctx, plan)
	gateErr = mergeGateExecutionError(gateErr, report, plannedRunner, plan.Commands)
	report = withBehaviorReview(report, controller.behaviorStatus)
	report = engine.withMergeGateMetadata(ctx, report, base, plan)
	return controller.finalize(engine, report, gateErr)
}

func (engine *Engine) runMergeGateLevel(ctx context.Context, plan MergeGateExecutionPlan) (Report, error) {
	switch plan.Level {
	case testpolicy.MergeLevelFull:
		return engine.fullMergeGate(ctx, plan.Tests)
	case testpolicy.MergeLevelDocumentation:
		report, err := engine.documentationMergeGate(ctx, plan.Selection)
		if err != nil || len(report.Findings) != 0 || len(plan.Tests.Suites) == 0 {
			return report, err
		}
		tested, testErr := engine.testExactPlan(ctx, plan.Tests, plan.Selection, true)
		return engine.combine(report, tested), testErr
	default:
		return engine.recommendedMergeGateWithTests(ctx, plan.Selection, plan.Tests)
	}
}

func mergeGateExecutionError(gateErr error, report Report, plannedRunner *mergeGatePlannedRunner, commands []MergeGateExecutionCommand) error {
	if plannedRunner.err != nil {
		gateErr = errors.Join(gateErr, plannedRunner.err)
	}
	if gateErr == nil && len(report.Findings) == 0 && plannedRunner.next != len(commands) {
		return fmt.Errorf("merge gate completed before planned command %d of %d", plannedRunner.next+1, len(commands))
	}
	return gateErr
}

func (engine *Engine) withMergeGateMetadata(ctx context.Context, report Report, base string, plan MergeGateExecutionPlan) Report {
	if report.BehaviorReview == nil {
		report = withBehaviorReview(report, plan.BehaviorReview.status)
	}
	report = engine.withMergeGateTestQualityReminder(ctx, report, plan.Selection)
	return withMergePolicy(report, plan.Level, base, plan.Reasons)
}

func (engine *Engine) PlanMergeGateExecution(base string) (MergeGateExecutionPlan, error) {
	plan, err := engine.mergeGateBaseExecutionPlan(base)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	if plan.Level == testpolicy.MergeLevelDocumentation {
		return engine.planDocumentationMergeGateExecution(plan)
	}
	return engine.planOrdinaryMergeGateExecution(plan)
}

func (engine *Engine) mergeGateBaseExecutionPlan(base string) (MergeGateExecutionPlan, error) {
	selection, err := engine.Repository.SelectBase(base)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	mergeDecision, err := testpolicy.BuildMergeDecision(engine.Repository, selection)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	behaviorDecision, err := engine.behaviorReviewDecision(context.Background(), selection, BehaviorReviewMerge)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	plan := MergeGateExecutionPlan{
		Level: mergeDecision.Level, Reasons: append([]string{}, mergeDecision.Reasons...), Selection: selection, BehaviorReview: behaviorDecision,
	}
	return plan, nil
}

func (engine *Engine) planDocumentationMergeGateExecution(plan MergeGateExecutionPlan) (MergeGateExecutionPlan, error) {
	tests, err := testpolicy.BuildPlan(engine.Repository, testpolicy.Request{Changed: plan.Selection})
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	tests, err = engine.forceBehaviorReviewSuites(tests, plan.BehaviorReview)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	plan.Tests = tests
	plan.Commands = mergeGateSuiteCommands(plan.Tests.Suites)
	return plan, nil
}

func (engine *Engine) planOrdinaryMergeGateExecution(plan MergeGateExecutionPlan) (MergeGateExecutionPlan, error) {
	checkSelection, request, err := engine.mergeGateTestRequest(plan)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	tests, err := testpolicy.BuildPlan(engine.Repository, request)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	tests, err = engine.forceBehaviorReviewSuites(tests, plan.BehaviorReview)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	plan.Tests = tests
	plan.Commands = append(plan.Commands, mergeGateCheckCommands(gaterun.Check, quality.CheckCommands(engine.Repository, checkSelection, "gate"))...)
	plan.Commands = append(plan.Commands, mergeGateSuiteCommands(plan.Tests.Suites)...)
	plan.Commands = append(plan.Commands, mergeGateCheckCommands(gaterun.Build, quality.CommandsForProfiles(engine.Repository, checkSelection, "build"))...)
	return engine.appendMergeGateSupplyChainCommands(plan)
}

func (engine *Engine) mergeGateTestRequest(plan MergeGateExecutionPlan) (repository.Selection, testpolicy.Request, error) {
	if plan.Level != testpolicy.MergeLevelFull {
		return plan.Selection, testpolicy.Request{Changed: plan.Selection, Recommended: true}, nil
	}
	selection, err := engine.Repository.Select("all", nil)
	if err != nil {
		return repository.Selection{}, testpolicy.Request{}, err
	}
	return selection, testpolicy.Request{Full: true}, nil
}

func (engine *Engine) appendMergeGateSupplyChainCommands(plan MergeGateExecutionPlan) (MergeGateExecutionPlan, error) {
	supplySelection, err := engine.Repository.Select("all", nil)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	if plan.Level != testpolicy.MergeLevelFull {
		plan.Commands = append(plan.Commands, mergeGateCheckCommands(gaterun.SupplyChain, quality.CommandsForProfiles(engine.Repository, supplySelection, "supply-chain"))...)
		return plan, nil
	}
	onlineCommands, err := supplychain.OnlineCommands(engine.Repository, supplySelection.Files)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	plan.Commands = append(plan.Commands, mergeGateCheckCommands(gaterun.SupplyChain, onlineCommands)...)
	plan.Commands = append(plan.Commands, mergeGateCheckCommands(gaterun.SupplyChain, quality.CommandsForProfiles(engine.Repository, supplySelection, "supply-chain", "supply-chain-online", "security"))...)
	plan.Commands = append(plan.Commands, mergeGateCheckCommands(gaterun.ArtifactSecurity, artifactsecurity.PlannedCommands(engine.Repository))...)
	return plan, nil
}

func mergeGateCheckCommands(category gaterun.CommandCategory, commands []policy.Command) []MergeGateExecutionCommand {
	result := make([]MergeGateExecutionCommand, 0, len(commands))
	for _, command := range commands {
		scope := "repository"
		if len(command.Modules) == 1 {
			scope = "module"
		}
		result = append(result, MergeGateExecutionCommand{Category: category, Kind: "check", Scope: scope, Cost: "standard", Command: command})
	}
	return result
}

func mergeGateSuiteCommands(suites []policy.TestSuite) []MergeGateExecutionCommand {
	result := make([]MergeGateExecutionCommand, 0, len(suites))
	seen := map[string]bool{}
	for _, suite := range suites {
		if seen[suite.Name] {
			continue
		}
		seen[suite.Name] = true
		result = append(result, MergeGateExecutionCommand{Category: gaterun.OrdinaryTest, Kind: suite.Kind, Scope: suite.Scope, Cost: suite.Cost, Command: suiteCommand(suite)})
	}
	return result
}

func (engine *Engine) forceBehaviorReviewSuites(plan testpolicy.Plan, decision behaviorReviewDecision) (testpolicy.Plan, error) {
	if !decision.required || len(decision.requiredSuites) == 0 {
		return plan, nil
	}
	configured := behaviorReviewConfiguredSuites(engine.Repository.Config.Tests.Suites)
	if err := validateBaseBehaviorReviewSuites(configured, decision.baseSelectedSuites); err != nil {
		return testpolicy.Plan{}, err
	}
	selected, err := forceBehaviorReviewSuites(plan.Suites, decision.requiredSuites, configured)
	if err != nil {
		return testpolicy.Plan{}, err
	}
	plan.Suites = selected
	return plan, nil
}

func behaviorReviewConfiguredSuites(suites []policy.TestSuite) map[string]policy.TestSuite {
	configured := make(map[string]policy.TestSuite, len(suites))
	for _, suite := range suites {
		configured[suite.Name] = suite
	}
	return configured
}

func validateBaseBehaviorReviewSuites(configured map[string]policy.TestSuite, baseSuites []policy.TestSuite) error {
	for _, baseSuite := range baseSuites {
		candidateSuite, err := configuredBehaviorReviewSuite(configured, baseSuite.Name)
		if err != nil {
			return err
		}
		if !sameTestSuiteSemantics(candidateSuite, baseSuite) {
			return fmt.Errorf("selected behavior review suite %q no longer matches its base-selected definition", baseSuite.Name)
		}
	}
	return nil
}

func configuredBehaviorReviewSuite(configured map[string]policy.TestSuite, name string) (policy.TestSuite, error) {
	suite, found := configured[name]
	if !found {
		return policy.TestSuite{}, fmt.Errorf("selected behavior review suite %q is unavailable in the candidate configuration", name)
	}
	if !policy.BehaviorReviewSuiteAllowed(suite) {
		return policy.TestSuite{}, fmt.Errorf("selected behavior review suite %q is ineligible in the candidate configuration", name)
	}
	return suite, nil
}

func forceBehaviorReviewSuites(existing []policy.TestSuite, required []string, configured map[string]policy.TestSuite) ([]policy.TestSuite, error) {
	selected, seen := uniqueBehaviorReviewSuites(existing, len(required))
	for _, name := range required {
		suite, err := configuredBehaviorReviewSuite(configured, name)
		if err != nil {
			return nil, err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		selected = append(selected, suite)
	}
	return selected, nil
}

func uniqueBehaviorReviewSuites(suites []policy.TestSuite, additional int) ([]policy.TestSuite, map[string]bool) {
	selected := make([]policy.TestSuite, 0, len(suites)+additional)
	seen := map[string]bool{}
	for _, suite := range suites {
		if seen[suite.Name] {
			continue
		}
		seen[suite.Name] = true
		selected = append(selected, suite)
	}
	return selected, seen
}

func suiteCommand(suite policy.TestSuite) policy.Command {
	return policy.Command{
		Name: suite.Name, Argv: append([]string{}, suite.Argv...), Cwd: suite.Cwd,
		Paths: append([]string{}, suite.Paths...), Modules: append([]string{}, suite.Modules...),
		Environment: append([]string{}, suite.Environment...), ExclusiveResources: append([]string{}, suite.ExclusiveResources...),
		TimeoutSeconds: suite.TimeoutSeconds,
	}
}

type mergeGatePlannedRunner struct {
	root     string
	delegate runner.Runner
	expected []MergeGateExecutionCommand
	next     int
	err      error
}

func (commandRunner *mergeGatePlannedRunner) TestDiagnosticRunner() runner.Runner {
	if provider, ok := commandRunner.delegate.(TestDiagnosticRunnerProvider); ok {
		if diagnosticRunner := provider.TestDiagnosticRunner(); diagnosticRunner != nil {
			return diagnosticRunner
		}
	}
	return commandRunner.delegate
}

func (commandRunner *mergeGatePlannedRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, err := commandRunner.RunWithResult(ctx, root, command)
	return err
}

func (commandRunner *mergeGatePlannedRunner) RunWithResult(ctx context.Context, root string, command policy.Command) (runner.Result, error) {
	if err := commandRunner.start(root, command); err != nil {
		return runner.Result{ExitStatus: -1}, err
	}
	if observed, ok := commandRunner.delegate.(interface {
		RunWithResult(context.Context, string, policy.Command) (runner.Result, error)
	}); ok {
		return observed.RunWithResult(ctx, root, command)
	}
	started := time.Now()
	err := commandRunner.delegate.Run(ctx, root, command)
	return runner.Result{ExecutionDuration: time.Since(started)}, err
}

func (commandRunner *mergeGatePlannedRunner) RunWithOutput(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	if err := commandRunner.start(root, command); err != nil {
		return runner.Result{ExitStatus: -1}, runner.Output{}, err
	}
	if observed, ok := commandRunner.delegate.(runner.OutputRunner); ok {
		return observed.RunWithOutput(ctx, root, command)
	}
	return runner.Result{ExitStatus: -1}, runner.Output{}, fmt.Errorf("merge gate command runner cannot capture output for %q", command.Name)
}

func (commandRunner *mergeGatePlannedRunner) start(root string, command policy.Command) error {
	if commandRunner.err != nil {
		return commandRunner.err
	}
	if root != commandRunner.root || commandRunner.next >= len(commandRunner.expected) {
		commandRunner.err = fmt.Errorf("merge gate started a command outside its execution plan at position %d", commandRunner.next+1)
		return commandRunner.err
	}
	if samePolicyCommand(commandRunner.expected[commandRunner.next].Command, command) {
		commandRunner.next++
		return nil
	}
	if artifactsecurity.IsCleanupCommand(command) {
		for index := commandRunner.next + 1; index < len(commandRunner.expected); index++ {
			if samePolicyCommand(commandRunner.expected[index].Command, command) {
				commandRunner.next = index + 1
				return nil
			}
		}
	}
	commandRunner.err = fmt.Errorf("merge gate started a command outside its execution plan at position %d", commandRunner.next+1)
	return commandRunner.err
}

func samePolicyCommand(expected, actual policy.Command) bool {
	if artifactsecurity.MatchesPlannedCommand(expected, actual) {
		return true
	}
	return samePolicyCommandIdentity(expected, actual) && samePolicyCommandCollections(expected, actual)
}

func samePolicyCommandIdentity(expected, actual policy.Command) bool {
	return expected.Name == actual.Name && expected.Cwd == actual.Cwd && expected.TimeoutSeconds == actual.TimeoutSeconds &&
		expected.Managed == actual.Managed && expected.PassFiles == actual.PassFiles && expected.SealedEnvironment == actual.SealedEnvironment
}

func samePolicyCommandCollections(expected, actual policy.Command) bool {
	return slices.Equal(expected.Argv, actual.Argv) && slices.Equal(expected.Provides, actual.Provides) &&
		slices.Equal(expected.Paths, actual.Paths) && slices.Equal(expected.Modules, actual.Modules) &&
		slices.Equal(expected.RunOn, actual.RunOn) && slices.Equal(expected.Environment, actual.Environment) &&
		slices.Equal(expected.ExclusiveResources, actual.ExclusiveResources) && slices.Equal(expected.PassFilePaths, actual.PassFilePaths)
}

func (engine *Engine) fullGate(ctx context.Context) (Report, error) {
	doctor, err := engine.Doctor(ctx)
	if err != nil || len(doctor.Findings) > 0 {
		return doctor, err
	}
	selection, err := engine.Repository.Select("all", nil)
	if err != nil {
		return Report{}, err
	}
	checked := engine.Check(ctx, selection, "gate")
	if len(checked.Findings) > 0 {
		return engine.combine(doctor, checked), nil
	}
	verified, err := engine.verify(ctx, false, true)
	if err != nil || len(verified.Findings) > 0 {
		return engine.combine(engine.combine(doctor, checked), verified), err
	}
	supply, err := engine.SupplyChain(ctx, false)
	return engine.combine(engine.combine(engine.combine(doctor, checked), verified), supply), err
}

func (engine *Engine) fullMergeGate(ctx context.Context, tests testpolicy.Plan) (Report, error) {
	doctor, err := engine.Doctor(ctx)
	if err != nil || len(doctor.Findings) > 0 {
		return doctor, err
	}
	selection, err := engine.Repository.Select("all", nil)
	if err != nil {
		return Report{}, err
	}
	checked := engine.Check(ctx, selection, "gate")
	if len(checked.Findings) > 0 {
		return engine.combine(doctor, checked), nil
	}
	verified, err := engine.testExactPlan(ctx, tests, selection, true)
	if err != nil || len(verified.Findings) > 0 {
		return engine.combine(engine.combine(doctor, checked), verified), err
	}
	buildFindings := quality.RunCommands(ctx, engine.Repository, selection, engine.Runner, "build")
	built := engine.finish(buildFindings, []string{"completed full build profile"})
	if len(built.Findings) > 0 {
		return engine.combine(engine.combine(engine.combine(doctor, checked), verified), built), nil
	}
	supply, err := engine.SupplyChain(ctx, false)
	return engine.combine(engine.combine(engine.combine(engine.combine(doctor, checked), verified), built), supply), err
}

func (engine *Engine) recommendedMergeGateWithTests(ctx context.Context, selection repository.Selection, tests testpolicy.Plan) (Report, error) {
	doctor, err := engine.Doctor(ctx)
	if err != nil || len(doctor.Findings) > 0 {
		return doctor, err
	}
	checked := engine.Check(ctx, selection, "gate")
	report := engine.combine(doctor, checked)
	if len(checked.Findings) > 0 {
		return report, nil
	}
	verified, err := engine.testExactPlan(ctx, tests, selection, true)
	report = engine.combine(report, verified)
	if err != nil || len(verified.Findings) > 0 {
		return report, err
	}
	buildFindings := quality.RunCommands(ctx, engine.Repository, selection, engine.Runner, "build")
	built := engine.finish(buildFindings, []string{"completed applicable build profile"})
	report = engine.combine(report, built)
	if len(built.Findings) > 0 {
		return report, nil
	}
	supply, err := engine.SupplyChain(ctx, true)
	return engine.combine(report, supply), err
}

func (engine *Engine) documentationMergeGate(ctx context.Context, selection repository.Selection) (Report, error) {
	findings := quality.DocumentationFindings(ctx, engine.Repository, selection.Candidate.AddedOrModified, selection.Candidate.Deleted)
	return engine.finish(findings, []string{"completed documentation contract with zero application test suites"}), nil
}

func withMergePolicy(report Report, level, base string, reasons []string) Report {
	report.MergePolicy = &MergePolicy{Level: level, Base: base, Reasons: append([]string{}, reasons...)}
	return report
}
