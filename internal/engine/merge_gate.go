package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/riteofstring/code-polishy/internal/architecture"
	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/artifactsecurity"
	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/quality"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/supplychain"
	"github.com/riteofstring/code-polishy/internal/testartifact"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

type MergeGateExecutionPlan struct {
	Level                 string
	Reasons               []string
	Selection             repository.Selection
	Tests                 testpolicy.Plan
	Commands              []MergeGateExecutionCommand
	BehaviorReview        behaviorReviewDecision
	FirstAdoption         bool
	BaseConfigurationPath string
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
	if execution.controller.alreadyPassed != nil {
		return engine.alreadyPassedMergeGate(ctx, base, plan, execution.controller)
	}
	if execution.reviewErr != nil {
		return engine.finalizeMergeGateReviewError(ctx, base, plan, execution)
	}
	if execution.reviewReport != nil {
		report := engine.withMergeGateMetadata(ctx, *execution.reviewReport, base, plan)
		return execution.controller.finalize(engine, report, nil)
	}
	if report, err := execution.controller.checkArchitectureReview(ctx, engine, base, plan.Level == testpolicy.MergeLevelDocumentation); err != nil || HasFindings(report) {
		report = withBehaviorReview(report, execution.controller.behaviorStatus)
		return execution.controller.finalize(engine, engine.withMergeGateMetadata(ctx, report, base, plan), err)
	}
	return engine.executeMergeGateCommands(ctx, base, plan, execution)
}

func (engine *Engine) alreadyPassedMergeGate(ctx context.Context, base string, plan MergeGateExecutionPlan, controller *gateRunController) (Report, error) {
	if err := engine.currentPythonReachabilityState(controller.pythonReachabilitySHA256); err != nil {
		return Report{}, err
	}
	if err := engine.currentGatePolicyValidity(controller.policyValiditySHA256); err != nil {
		return Report{}, err
	}
	evidence, err := engine.currentGitEvidence(controller.gitEvidenceSHA256)
	if err != nil {
		return Report{}, err
	}
	reused := []string{}
	for _, command := range controller.alreadyPassed.Commands {
		if command.Category == gaterun.OrdinaryTest {
			reused = append(reused, command.Name)
		}
	}
	report := Report{
		SourceDependencyGraph: sourcegraph.Clone(controller.alreadyPassed.SourceDependencyGraph),
		Findings:              append([]policy.Finding{}, controller.alreadyPassed.Findings...),
		Suppressed:            append([]policy.Suppressed{}, controller.alreadyPassed.Suppressed...),
		Assessed:              append([]policy.AssessedVulnerability{}, controller.alreadyPassed.Assessed...),
		ReleaseAges:           append([]policy.AssessedReleaseAge{}, controller.alreadyPassed.ReleaseAges...),
		BehaviorReview:        &controller.behaviorStatus,
		GitEvidence:           evidence,
		GateRunPolicy:         &GateRunPolicy{Status: "already-passed", ReportPath: controller.alreadyPassedPath, ReusedPhases: reused},
		Notes:                 []string{"the exact gate identity already passed; no validation commands executed"},
	}
	return engine.withMergeGateMetadata(ctx, report, base, plan), nil
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
	replayReport, replayReportErr := engine.behaviorReviewGateFailureReport(ctx, plan, replayErr)
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
		if err != nil || HasFindings(report) || len(plan.Tests.Suites) == 0 {
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
	if gateErr == nil && !HasFindings(report) && plannedRunner.next != len(commands) {
		return fmt.Errorf("merge gate completed before planned command %d of %d", plannedRunner.next+1, len(commands))
	}
	return gateErr
}

func (engine *Engine) withMergeGateMetadata(ctx context.Context, report Report, base string, plan MergeGateExecutionPlan) Report {
	if report.BehaviorReview == nil {
		report = withBehaviorReview(report, plan.BehaviorReview.status)
	}
	report = engine.withMergeGateTestQualityReminder(ctx, report, plan.Selection)
	if plan.FirstAdoption {
		report.Notes = append(report.Notes, firstAdoptionMergeGateReason(plan.BaseConfigurationPath, plan.Selection.Base))
	}
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
	baseConfiguration, err := engine.behaviorReviewConfigAt(selection.Base)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	mergeDecision, err := testpolicy.BuildMergeDecision(engine.Repository, selection)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	if !baseConfiguration.Present {
		mergeDecision.Level = testpolicy.MergeLevelFull
		mergeDecision.Reasons = []string{firstAdoptionMergeGateReason(baseConfiguration.Path, selection.Base)}
	}
	behaviorDecision, err := engine.behaviorReviewDecisionWithBaseConfiguration(
		context.Background(), selection, BehaviorReviewMerge, baseConfiguration.Config,
	)
	if err != nil {
		return MergeGateExecutionPlan{}, err
	}
	plan := MergeGateExecutionPlan{
		Level: mergeDecision.Level, Reasons: append([]string{}, mergeDecision.Reasons...), Selection: selection, BehaviorReview: behaviorDecision,
		FirstAdoption: !baseConfiguration.Present, BaseConfigurationPath: baseConfiguration.Path,
	}
	return plan, nil
}

func firstAdoptionMergeGateReason(path, base string) string {
	return fmt.Sprintf("first adoption: base configuration %q is absent at %s; candidate policy governs a full gate", path, base)
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
	plan.Commands = append(plan.Commands, mergeGateCheckCommands(gaterun.Check, plannedPolicyCheckCommands(engine.Repository, checkSelection, "gate"))...)
	plan.Commands = append(plan.Commands, mergeGateSuiteCommands(plan.Tests.Suites)...)
	plan.Commands = append(plan.Commands, mergeGateCheckCommands(gaterun.Build, quality.CommandsForProfiles(engine.Repository, checkSelection, "build"))...)
	return engine.appendMergeGateSupplyChainCommands(plan)
}

func plannedPolicyCheckCommands(repo repository.Repository, selection repository.Selection, profile string) []policy.Command {
	commands := quality.CheckCommands(repo, selection, profile)
	return append(commands, architecture.PythonGraphCommands(repo, selection.Files)...)
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
	if len(plan.Aggregations) > 0 {
		requiredSet := map[string]bool{}
		for _, name := range decision.requiredSuites {
			requiredSet[name] = true
		}
		kept := plan.Aggregations[:0]
		for _, aggregation := range plan.Aggregations {
			if !requiredSet[aggregation.Suite] {
				kept = append(kept, aggregation)
			}
		}
		plan.Aggregations = kept
	}
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
		TimeoutSeconds:    suite.TimeoutSeconds,
		SealedEnvironment: suite.Reusable,
		TestArtifacts:     append([]policy.TestArtifact{}, suite.Artifacts...),
	}
}

type mergeGatePlannedRunner struct {
	root     string
	viewRoot string
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

func (commandRunner *mergeGatePlannedRunner) ManagesTestArtifacts() bool { return true }

func (commandRunner *mergeGatePlannedRunner) TestArtifacts(name string, attempt int) []testartifact.Record {
	if provider, ok := commandRunner.delegate.(interface {
		TestArtifacts(string, int) []testartifact.Record
	}); ok {
		return provider.TestArtifacts(name, attempt)
	}
	return []testartifact.Record{}
}

func (commandRunner *mergeGatePlannedRunner) ReuseSuite(suite policy.TestSuite, attempt int) (testpolicy.SuiteExecution, bool, error) {
	if commandRunner.err != nil || commandRunner.next >= len(commandRunner.expected) ||
		!samePolicyCommand(commandRunner.expected[commandRunner.next].Command, suiteCommand(suite)) {
		return testpolicy.SuiteExecution{}, false, nil
	}
	controller, ok := commandRunner.delegate.(testpolicy.SuiteReuseController)
	if !ok {
		return testpolicy.SuiteExecution{}, false, nil
	}
	execution, reused, err := controller.ReuseSuite(suite, attempt)
	if err != nil || !reused {
		return execution, reused, err
	}
	commandRunner.next++
	return execution, true, nil
}

func (commandRunner *mergeGatePlannedRunner) RecordSuite(execution testpolicy.SuiteExecution) error {
	if controller, ok := commandRunner.delegate.(testpolicy.SuiteReuseController); ok {
		return controller.RecordSuite(execution)
	}
	return nil
}

func (commandRunner *mergeGatePlannedRunner) ReceiptNotes() []string {
	if provider, ok := commandRunner.delegate.(interface{ ReceiptNotes() []string }); ok {
		return provider.ReceiptNotes()
	}
	return nil
}

func (commandRunner *mergeGatePlannedRunner) PrepareSuiteView(suite policy.TestSuite) (string, func() error, error) {
	if controller, ok := commandRunner.delegate.(testpolicy.SuiteExecutionViewController); ok {
		root, cleanup, err := controller.PrepareSuiteView(suite)
		if err != nil {
			return "", nil, err
		}
		commandRunner.viewRoot = root
		return root, func() error {
			commandRunner.viewRoot = ""
			return cleanup()
		}, nil
	}
	return "", nil, errors.New("test execution view controller is unavailable")
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

func (commandRunner *mergeGatePlannedRunner) RunStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	if err := commandRunner.start(root, command); err != nil {
		return runner.Result{ExitStatus: -1}, runner.Output{}, err
	}
	if observed, ok := commandRunner.delegate.(runner.StructuredRunner); ok {
		return observed.RunStructured(ctx, root, command)
	}
	if observed, ok := commandRunner.delegate.(runner.OutputRunner); ok {
		return observed.RunWithOutput(ctx, root, command)
	}
	return runner.Result{ExitStatus: -1}, runner.Output{}, fmt.Errorf("merge gate command runner cannot capture structured output for %q", command.Name)
}

func (commandRunner *mergeGatePlannedRunner) start(root string, command policy.Command) error {
	if commandRunner.err != nil {
		return commandRunner.err
	}
	if (root != commandRunner.root && root != commandRunner.viewRoot) || commandRunner.next >= len(commandRunner.expected) {
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
		slices.Equal(expected.ExclusiveResources, actual.ExclusiveResources) && slices.Equal(expected.PassFilePaths, actual.PassFilePaths) &&
		slices.Equal(expected.TestArtifacts, actual.TestArtifacts)
}

func (engine *Engine) fullGate(ctx context.Context) (Report, error) {
	doctor, err := engine.Doctor(ctx)
	if err != nil || HasFindings(doctor) {
		return doctor, err
	}
	selection, err := engine.Repository.Select("all", nil)
	if err != nil {
		return Report{}, err
	}
	checked := engine.Check(ctx, selection, "gate")
	if HasFindings(checked) {
		return engine.combine(doctor, checked), nil
	}
	verified, err := engine.verify(ctx, false, true)
	if err != nil || HasFindings(verified) {
		return engine.combine(engine.combine(doctor, checked), verified), err
	}
	supply, err := engine.SupplyChain(ctx, false)
	return engine.combine(engine.combine(engine.combine(doctor, checked), verified), supply), err
}

func (engine *Engine) fullMergeGate(ctx context.Context, tests testpolicy.Plan) (Report, error) {
	doctor, err := engine.Doctor(ctx)
	if err != nil || HasFindings(doctor) {
		return doctor, err
	}
	selection, err := engine.Repository.Select("all", nil)
	if err != nil {
		return Report{}, err
	}
	checked := engine.Check(ctx, selection, "gate")
	if HasFindings(checked) {
		return engine.combine(doctor, checked), nil
	}
	verified, err := engine.testExactPlan(ctx, tests, selection, true)
	if err != nil || HasFindings(verified) {
		return engine.combine(engine.combine(doctor, checked), verified), err
	}
	buildFindings := quality.RunCommands(ctx, engine.Repository, selection, engine.Runner, "build")
	built := engine.finish(buildFindings, []string{"completed full build profile"})
	if HasFindings(built) {
		return engine.combine(engine.combine(engine.combine(doctor, checked), verified), built), nil
	}
	supply, err := engine.SupplyChain(ctx, false)
	return engine.combine(engine.combine(engine.combine(engine.combine(doctor, checked), verified), built), supply), err
}

func (engine *Engine) recommendedMergeGateWithTests(ctx context.Context, selection repository.Selection, tests testpolicy.Plan) (Report, error) {
	doctor, err := engine.Doctor(ctx)
	if err != nil || HasFindings(doctor) {
		return doctor, err
	}
	checked := engine.Check(ctx, selection, "gate")
	report := engine.combine(doctor, checked)
	if HasFindings(checked) {
		return report, nil
	}
	verified, err := engine.testExactPlan(ctx, tests, selection, true)
	report = engine.combine(report, verified)
	if err != nil || HasFindings(verified) {
		return report, err
	}
	buildFindings := quality.RunCommands(ctx, engine.Repository, selection, engine.Runner, "build")
	built := engine.finish(buildFindings, []string{"completed applicable build profile"})
	report = engine.combine(report, built)
	if HasFindings(built) {
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
