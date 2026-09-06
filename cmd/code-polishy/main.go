package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/agents"
	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/release"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

const usage = `Usage: code-polishy [global options] <command> [options]

Commands:
  version
  capabilities [--query TEXT] [--format human|json]
  task-start --intent-file PATH (--files PATH | --module NAME) [--feature NAME] [--situation NAME]
  docs <list|find|read>
  pack <install|verify|root>
  agents <install|sync|check>
  lock
  release-manifest <write|verify|materialize|archive|publish|index|oci-context> [options]
  change-boundary --base COMMIT --module NAME... [--allow-path PATH...] [--allow-new-path PATH...]
  task-session --module NAME... [options] -- COMMAND [ARG...]
  check [--git-changes|--staged|--all|--files PATH...|--module NAME...|--name NAME...]
  gate
  checkpoint-gate --base REF
  merge-gate --base REF [--resume]
  behavior-review capture-intent --intent-file PATH [--feature NAME...] [--format human|json]
  behavior-review require --base REF --feature NAME...
  behavior-review status --base REF [--format human|json]
  behavior-review prepare --base REF
  behavior-review finalize --base REF
  architecture-review <status|prepare|finalize> --base REF
  regression-proof --base REF --suite NAME --evidence PATH... --id ID [--red-exit STATUS]
  test [--changed [--base REF]|--recommended [--base REF]|--all|--supplemental [--resume]|--module NAME...|--suite NAME...]
  test-plan [--base REF]
  test-levels [--base REF]
  test-receipts export --output PATH
  test-receipts import --source PATH --sha256 DIGEST
  verify [--tests-only]
  architecture [--git-changes|--staged|--all|--files PATH...|--module NAME...]
  supply-chain [--offline]
  dependency-review --base REF
  artifact-security
  doctor [--strict]
  design-context (--module NAME... | [--git-changes|--staged|--all|--files PATH...]) [--situation NAME...]
  format [--git-changes|--staged|--all|--files PATH...|--module NAME...]
  fix [selection options]
  list-files [selection options]

Global options:
  --repo-root PATH    repository to govern (default: current directory)
  --policy-root PATH  Code Polishy checkout (default: inferred from executable)
  --config PATH       target config path, relative to repository root
  --verbose           show detailed execution telemetry and report notes
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(arguments []string) int {
	invocation, status := parseGlobalInvocation(arguments)
	if status != 0 {
		return status
	}
	if status, handled := handleContextualHelp(invocation); handled {
		return status
	}
	if status := validateInvocation(invocation); status != 0 {
		return status
	}
	if status, handled := handleMetaCommand(invocation); handled {
		return status
	}
	return runPolicyCommand(invocation)
}

func validateInvocation(invocation invocation) int {
	if _, known := commandHelpFor(invocation.command); !known {
		return commandUsageError("", "unknown command "+invocation.command)
	}
	if status, governed := requireLockedRelease(invocation); !governed {
		return status
	}
	return 0
}

func runPolicyCommand(invocation invocation) int {
	started := time.Now()
	outputOptions, commandArguments, parseErr := parseReportOutputOptions(invocation.command, invocation.arguments)
	if parseErr != nil {
		return commandUsageErrorForInvocation(invocation, parseErr.Error())
	}
	openStarted := time.Now()
	policyEngine, err := engine.Open(invocation.repoRoot, invocation.policyRoot, invocation.configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 2
	}
	policyEngine.Verbose = invocation.verbose
	if outputOptions.format != "human" {
		policyEngine.RouteProgress(os.Stderr)
	}
	policyEngine.BeginExecution(started, time.Since(openStarted))
	handler, exists := commandHandlers()[invocation.command]
	if !exists {
		return commandUsageError("", "unknown command "+invocation.command)
	}
	result, err := handler(context.Background(), policyEngine, commandArguments)
	if err == nil && !result.quiet {
		result.report = policyEngine.AttachExecution(result.report)
		result.report = engine.PrepareReportDisplay(result.report, outputOptions.display)
		result.report, err = policyEngine.FinalizeReport(invocation.command, result.report)
	}
	return renderCommandResultWithOptions(invocation, policyEngine, result, err, outputOptions)
}

func renderCommandResultWithOptions(invocation invocation, policyEngine *engine.Engine, result commandResult, err error, options reportOutputOptions) int {
	if err != nil {
		if isCommandInputError(err) {
			return commandUsageErrorForInvocation(invocation, err.Error())
		}
		printOperationalReportHeaders(os.Stdout, result.report, invocation.verbose)
		return operationalError(err)
	}
	messageOutput := io.Writer(os.Stdout)
	if options.format != "human" {
		messageOutput = os.Stderr
	}
	for _, message := range result.messages {
		fmt.Fprintln(messageOutput, message)
	}
	if result.quiet {
		return 0
	}
	failed := engine.HasFindings(result.report)
	view := engine.ReportView(result.report, options.display)
	if renderErr := renderReportOutput(policyEngine, view, invocation.verbose, options); renderErr != nil {
		return operationalError(renderErr)
	}
	if failed {
		return 1
	}
	return 0
}

func printOperationalReportHeaders(output io.Writer, report engine.Report, verbose bool) {
	if report.MergePolicy != nil {
		printMergePolicyForMode(output, report.MergePolicy, verbose)
	}
	if report.CheckpointPolicy != nil {
		printCheckpointPolicyForMode(output, report.CheckpointPolicy, verbose)
	}
	printBehaviorReview(output, report.BehaviorReview, verbose)
}

func handleMetaCommand(invocation invocation) (int, bool) {
	if status, handled := handleWorkflowMetaCommand(invocation); handled {
		return status, true
	}
	return handleCoreMetaCommand(invocation)
}

func handleCoreMetaCommand(invocation invocation) (int, bool) {
	switch invocation.command {
	case "help", "--help", "-h":
		fmt.Print(usage)
		return 0, true
	case "version", "--version":
		return printVersion(invocation), true
	case "docs":
		return handleDocsMeta(invocation), true
	case "pack":
		return handlePackMeta(invocation), true
	case "agents":
		return handleAgentsMeta(invocation), true
	case "lock":
		return handleLockMeta(invocation), true
	case "install-bundle":

		return handleInstallBundleMeta(invocation), true
	case "release-manifest":
		return handleReleaseManifestMeta(invocation), true
	case "change-boundary":
		return handleChangeBoundaryMeta(invocation), true
	default:
		return 0, false
	}
}

func handleWorkflowMetaCommand(invocation invocation) (int, bool) {
	switch invocation.command {
	case "task-session":
		return handleTaskSessionWorkflowMeta(invocation), true
	case "task-session-artifacts":
		return handleTaskSessionArtifactsMeta(invocation), true
	case "task-session-receipt":
		return handleTaskSessionReceiptMeta(invocation), true
	case "governed-environment":
		return handleGovernedEnvironmentMeta(invocation), true
	default:
		return 0, false
	}
}

func handleChangeBoundaryMeta(invocation invocation) int {
	outputOptions, commandArguments, err := parseReportOutputOptions(invocation.command, invocation.arguments)
	if err != nil {
		return commandUsageErrorForInvocation(invocation, err.Error())
	}
	options, err := parseChangeBoundaryOptions(commandArguments)
	if err != nil {
		return commandUsageError("change-boundary", err.Error())
	}
	if options.base == "" || len(options.modules) == 0 {
		return commandUsageError("change-boundary", "change-boundary requires --base COMMIT and at least one --module NAME")
	}
	report, err := engine.CheckChangeBoundary(
		invocation.repoRoot, invocation.policyRoot, invocation.configPath, options.base, options.modules, options.paths, options.newPaths,
	)
	if err != nil {
		return operationalError(err)
	}
	custodian, err := engine.OpenReportCustodian(invocation.repoRoot)
	if err != nil {
		return operationalError(err)
	}
	report = engine.PrepareReportDisplay(report, outputOptions.display)
	report, err = custodian.FinalizeReport(invocation.command, report)
	return renderCommandResultWithOptions(invocation, custodian, commandResult{report: report}, err, outputOptions)
}

type changeBoundaryOptions struct {
	base     string
	modules  []string
	paths    []string
	newPaths []string
}

func parseChangeBoundaryOptions(arguments []string) (changeBoundaryOptions, error) {
	options := changeBoundaryOptions{}
	flags := flag.NewFlagSet("change-boundary", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&options.base, "base", "", "exact trusted starting commit")
	flags.Var((*stringList)(&options.modules), "module", "module this task may change")
	flags.Var((*stringList)(&options.paths), "allow-path", "exact tracked non-control file this task may change")
	flags.Var((*stringList)(&options.newPaths), "allow-new-path", "exact new regular non-control file this task may create")
	if err := flags.Parse(arguments); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected change-boundary arguments: %s", strings.Join(flags.Args(), " "))
	}
	return options, nil
}

func handleAgentsMeta(invocation invocation) int {
	if len(invocation.arguments) != 1 {
		return commandUsageError("agents", "agents requires exactly one of install, sync, or check")
	}
	switch invocation.arguments[0] {
	case "install":
		message, err := agents.Install(invocation.repoRoot, invocation.policyRoot)
		if err != nil {
			return operationalError(err)
		}
		fmt.Println("PASS", message)
		return 0
	case "sync":
		message, err := agents.Sync(invocation.repoRoot, invocation.policyRoot)
		if err != nil {
			return operationalError(err)
		}
		fmt.Println("PASS", message)
		return 0
	case "check":
		status := agents.Check(invocation.repoRoot, invocation.policyRoot)
		if !status.Current {
			fmt.Fprintln(os.Stderr, "FAIL", status.Message)
			return 1
		}
		fmt.Println("PASS", status.Message)
		return 0
	default:
		return commandUsageError("agents", "agents requires exactly one of install, sync, or check")
	}
}

func requireLockedRelease(invocation invocation) (int, bool) {
	exempt := []string{"lock", "install-bundle", "release-manifest", "version", "docs", "pack", "--version", "help", "--help", "-h"}
	if slices.Contains(exempt, invocation.command) {
		return 0, true
	}
	if err := release.RequireLockedRelease(invocation.repoRoot, invocation.policyRoot); err != nil {
		return operationalError(err), false
	}
	return 0, true
}

func printVersion(invocation invocation) int {
	if len(invocation.arguments) != 0 {
		return commandUsageError("version", "version does not accept options")
	}
	data, err := os.ReadFile(filepath.Join(invocation.policyRoot, "VERSION"))
	if err != nil {
		return operationalError(fmt.Errorf("read policy version: %w", err))
	}
	fmt.Printf("code-polishy %s\n", strings.TrimSpace(string(data)))
	return 0
}

type invocation struct {
	repoRoot, policyRoot, configPath string
	command                          string
	arguments                        []string
	verbose                          bool
}

type commandResult struct {
	report   engine.Report
	quiet    bool
	messages []string
}

type commandHandler func(context.Context, *engine.Engine, []string) (commandResult, error)

func parseGlobalInvocation(arguments []string) (invocation, int) {
	global := flag.NewFlagSet("code-polishy", flag.ContinueOnError)
	global.SetOutput(os.Stderr)
	workingDirectory, _ := os.Getwd()
	defaultPolicyRoot := inferPolicyRoot()
	repoRoot := global.String("repo-root", workingDirectory, "target repository root")
	policyRoot := global.String("policy-root", defaultPolicyRoot, "policy checkout root")
	configPath := global.String("config", "", "target configuration path")
	verbose := global.Bool("verbose", false, "show detailed execution telemetry and report notes")
	commandIndex := firstCommandIndex(arguments)
	if commandIndex < 0 {
		fmt.Fprint(os.Stderr, usage)
		return invocation{}, 2
	}
	if err := global.Parse(arguments[:commandIndex]); err != nil {
		return invocation{}, 2
	}
	return invocation{
		repoRoot: *repoRoot, policyRoot: *policyRoot, configPath: *configPath,
		command: arguments[commandIndex], arguments: arguments[commandIndex+1:], verbose: *verbose,
	}, 0
}

func commandHandlers() map[string]commandHandler {
	return map[string]commandHandler{
		"capabilities":        handleCapabilities,
		"task-start":          handleTaskStart,
		"doctor":              handleDoctor,
		"gate":                handleGate,
		"checkpoint-gate":     handleCheckpointGate,
		"merge-gate":          handleMergeGate,
		"behavior-review":     handleBehaviorReview,
		"architecture-review": handleArchitectureReview,
		"regression-proof":    handleRegressionProof,
		"check":               handleCheck,
		"architecture":        handleSelectionCommand("architecture"),
		"format":              handleSelectionCommand("format"),
		"fix":                 handleSelectionCommand("format"),
		"list-files":          handleSelectionCommand("list-files"),
		"test":                handleTest,
		"test-plan":           handleTestPlan,
		"test-levels":         handleTestLevels,
		"test-receipts":       handleTestReceipts,
		"verify":              handleVerify,
		"supply-chain":        handleSupplyChain,
		"dependency-review":   handleDependencyReview,
		"artifact-security":   handleArtifactSecurity,
		"design-context":      handleDesignContext,
	}
}

func handleCheck(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	named := false
	for _, argument := range arguments {
		if argument == "--name" || strings.HasPrefix(argument, "--name=") {
			named = true
		}
	}
	if !named {
		return handleSelectionCommand("check")(ctx, policyEngine, arguments)
	}
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	names := []string{}
	flags.Var((*stringList)(&names), "name", "run one exact configured check")
	if err := flags.Parse(arguments); err != nil {
		return commandResult{}, commandInputError(err)
	}
	if flags.NArg() != 0 || len(names) == 0 {
		return commandResult{}, commandInputError(errors.New("check --name requires at least one configured check name and no selection options"))
	}
	report, err := policyEngine.CheckNamed(ctx, names)
	return commandResult{report: report}, err
}

func handleDoctor(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if !onlyAllowed(arguments, "--strict") {
		return commandResult{}, commandInputError(fmt.Errorf("doctor accepts only --strict"))
	}
	report, err := policyEngine.Doctor(ctx)
	return commandResult{report: report}, err
}

func handleGate(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if len(arguments) != 0 {
		return commandResult{}, commandInputError(fmt.Errorf("gate does not accept options"))
	}
	report, err := policyEngine.Gate(ctx)
	return commandResult{report: report}, err
}

func handleMergeGate(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseMergeGateOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	report, err := policyEngine.MergeGateWithOptions(ctx, options.base, engine.MergeGateOptions{Resume: options.resume})
	return commandResult{report: report}, err
}

func handleCheckpointGate(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	base, err := parseRequiredBaseOption("checkpoint-gate", arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	report, err := policyEngine.CheckpointGate(ctx, base)
	return commandResult{report: report}, err
}

func handleSelectionCommand(operation string) commandHandler {
	return func(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
		mode, files, err := parseSelection(arguments)
		if err != nil {
			return commandResult{}, commandInputError(err)
		}
		selection, err := policyEngine.Select(mode, files)
		if err != nil {
			return commandResult{}, err
		}
		switch operation {
		case "check":
			if mode == "changes" || mode == "staged" {
				return commandResult{report: policyEngine.CheckChangeAware(ctx, selection, "check")}, nil
			}
			return commandResult{report: policyEngine.Check(ctx, selection, "check")}, nil
		case "architecture":
			return commandResult{report: policyEngine.Architecture(ctx, selection)}, nil
		case "format":
			return commandResult{report: policyEngine.Format(ctx, selection)}, nil
		default:
			for _, path := range selection.Files {
				fmt.Println(path)
			}
			return commandResult{quiet: true}, nil
		}
	}
}

func handleTest(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	request, err := parseTestRequest(policyEngine, arguments)
	if err != nil {
		return commandResult{}, err
	}
	report, err := policyEngine.Test(ctx, request)
	return commandResult{report: report}, err
}

func handleTestPlan(_ context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	base, err := parseBaseOption("test-plan", arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	report, err := policyEngine.TestPlan(base)
	return commandResult{report: report}, err
}

func handleTestLevels(_ context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	base, err := parseBaseOption("test-levels", arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	report, err := policyEngine.TestPlan(base)
	return commandResult{report: report}, err
}

func handleVerify(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if !onlyAllowed(arguments, "--tests-only") {
		return commandResult{}, commandInputError(fmt.Errorf("verify accepts only --tests-only"))
	}
	report, err := policyEngine.Verify(ctx, len(arguments) == 1)
	return commandResult{report: report}, err
}

func handleSupplyChain(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if !onlyAllowed(arguments, "--offline") {
		return commandResult{}, commandInputError(fmt.Errorf("supply-chain accepts only --offline"))
	}
	report, err := policyEngine.SupplyChain(ctx, len(arguments) == 1)
	return commandResult{report: report}, err
}

func handleDependencyReview(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	base, err := parseRequiredBaseOption("dependency-review", arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	report, err := policyEngine.DependencyReview(ctx, base)
	return commandResult{report: report}, err
}

func handleArtifactSecurity(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if len(arguments) != 0 {
		return commandResult{}, commandInputError(fmt.Errorf("artifact-security does not accept options"))
	}
	return commandResult{report: policyEngine.ArtifactSecurity(ctx)}, nil
}

func firstCommandIndex(arguments []string) int {
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--verbose" || strings.HasPrefix(argument, "--verbose=") {
			continue
		}
		if argument == "--repo-root" || argument == "--policy-root" || argument == "--config" {
			index++
			continue
		}
		if strings.HasPrefix(argument, "--repo-root=") || strings.HasPrefix(argument, "--policy-root=") || strings.HasPrefix(argument, "--config=") {
			continue
		}
		return index
	}
	return -1
}

func parseTestRequest(policyEngine *engine.Engine, arguments []string) (testpolicy.Request, error) {
	options, err := parseTestOptions(arguments)
	if err != nil {
		return options.request, commandInputError(err)
	}
	if err := validateTestOptions(options); err != nil {
		return options.request, commandInputError(err)
	}
	if !testRequestNeedsChanges(options.request) {
		return options.request, nil
	}
	return withTestChanges(policyEngine, options.request, options.base)
}

type testOptions struct {
	request testpolicy.Request
	changed bool
	base    string
}

func parseTestOptions(arguments []string) (testOptions, error) {
	options := testOptions{}
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.BoolVar(&options.changed, "changed", false, "select suites affected by Git changes")
	flags.BoolVar(&options.request.Recommended, "recommended", false, "run the impact-based pre-merge set")
	flags.StringVar(&options.base, "base", "", "compare with the merge base of this Git ref")
	flags.BoolVar(&options.request.Full, "all", false, "run the full profile")
	flags.BoolVar(&options.request.Supplemental, "supplemental", false, "run explicitly selected supplemental hardening suites")
	flags.BoolVar(&options.request.Resume, "resume", false, "reuse still-valid supplemental suite receipts")
	flags.Var((*stringList)(&options.request.Modules), "module", "run focused suites for a module")
	flags.Var((*stringList)(&options.request.Suites), "suite", "run a named suite")
	if err := flags.Parse(arguments); err != nil {
		return options, err
	}
	if flags.NArg() != 0 {
		return options, fmt.Errorf("unexpected test arguments: %s", strings.Join(flags.Args(), " "))
	}
	return options, nil
}

func validateTestOptions(options testOptions) error {
	selectors := testSelectorCount(options.request)
	if selectors > 1 {
		return fmt.Errorf("choose only one of --recommended, --all, --supplemental, --module, or --suite")
	}
	if options.changed && selectors > 0 {
		return fmt.Errorf("--changed cannot be combined with --recommended, --all, --supplemental, --module, or --suite")
	}
	if options.base != "" && !options.request.Recommended && !options.changed {
		return fmt.Errorf("--base requires --changed or --recommended")
	}
	if options.request.Resume && !options.request.Supplemental {
		return fmt.Errorf("--resume requires --supplemental")
	}
	return nil
}

func testSelectorCount(request testpolicy.Request) int {
	selectors := 0
	if request.Full {
		selectors++
	}
	if request.Recommended {
		selectors++
	}
	if request.Supplemental {
		selectors++
	}
	if len(request.Modules) > 0 {
		selectors++
	}
	if len(request.Suites) > 0 {
		selectors++
	}
	return selectors
}

func testRequestNeedsChanges(request testpolicy.Request) bool {
	return !request.Full && !request.Supplemental && len(request.Modules) == 0 && len(request.Suites) == 0
}

func withTestChanges(policyEngine *engine.Engine, request testpolicy.Request, base string) (testpolicy.Request, error) {
	if base != "" {
		request.RequestedBase = base
		selection, err := policyEngine.SelectBase(base)
		request.Changed = selection
		return request, err
	}
	selection, err := policyEngine.Select("changes", nil)
	request.Changed = selection
	return request, err
}

func parseBaseOption(command string, arguments []string) (string, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	base := flags.String("base", "", "compare with the merge base of this Git ref")
	if err := flags.Parse(arguments); err != nil {
		return "", err
	}
	if flags.NArg() != 0 {
		return "", fmt.Errorf("unexpected %s arguments: %s", command, strings.Join(flags.Args(), " "))
	}
	return *base, nil
}

func parseRequiredBaseOption(command string, arguments []string) (string, error) {
	baseOptions := 0
	for _, argument := range arguments {
		if argument == "--base" || strings.HasPrefix(argument, "--base=") {
			baseOptions++
		}
	}
	if baseOptions != 1 {
		return "", fmt.Errorf("%s requires exactly one --base REF", command)
	}
	base, err := parseBaseOption(command, arguments)
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", fmt.Errorf("%s requires exactly one --base REF", command)
	}
	return base, nil
}

type mergeGateOptions struct {
	base   string
	resume bool
}

func parseMergeGateOptions(arguments []string) (mergeGateOptions, error) {
	baseOptions := 0
	resumeOptions := 0
	for _, argument := range arguments {
		if argument == "--base" || strings.HasPrefix(argument, "--base=") {
			baseOptions++
		}
		if argument == "--resume" {
			resumeOptions++
		}
	}
	if baseOptions != 1 {
		return mergeGateOptions{}, fmt.Errorf("merge-gate requires exactly one --base REF")
	}
	if resumeOptions > 1 {
		return mergeGateOptions{}, fmt.Errorf("merge-gate accepts --resume at most once")
	}
	flags := flag.NewFlagSet("merge-gate", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	base := flags.String("base", "", "compare with the merge base of this Git ref")
	resume := flags.Bool("resume", false, "reuse eligible ordinary-test receipts from an identical failed run")
	if err := flags.Parse(arguments); err != nil {
		return mergeGateOptions{}, err
	}
	if flags.NArg() != 0 || *base == "" {
		return mergeGateOptions{}, fmt.Errorf("merge-gate requires exactly one --base REF and accepts only --resume")
	}
	return mergeGateOptions{base: *base, resume: *resume}, nil
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func onlyAllowed(arguments []string, allowed string) bool {
	return len(arguments) == 0 || len(arguments) == 1 && arguments[0] == allowed
}

func printReportTo(stdout, stderr io.Writer, report engine.Report) {
	printReportWithMode(stdout, stderr, report, false)
}

func printReportWithMode(stdout, stderr io.Writer, report engine.Report, verbose bool) {
	printReportCompletion(stdout, stderr, report)
	printReportSummaryGroups(stdout, report.Summary)
	printFormattingOutcome(stdout, report.Formatting)
	printGitEvidence(stdout, report)
	printTestQualityReminder(stdout, report.TestQualityReminder)
	printReportHeaders(stdout, report, verbose)
	printExecutionTelemetry(stdout, report.Execution, verbose)
	printRepositoryContext(stdout, report.RepositoryContext)
	limit := engine.DefaultFindingDisplayLimit
	if report.Display != nil && report.Display.Limit > 0 {
		limit = report.Display.Limit
	}
	remaining := limit
	findings := boundedReportItems(report.Findings, &remaining)
	suppressed := boundedReportItems(report.Suppressed, &remaining)
	assessed := boundedReportItems(report.Assessed, &remaining)
	releaseAges := boundedReportItems(report.ReleaseAges, &remaining)
	printFindings(stdout, stderr, findings)
	printTestFailureEvidence(stderr, report.TestCommands, report.TestDiagnostics)
	printSuppressedFindings(stdout, suppressed)
	printVulnerabilityAssessments(stdout, assessed)
	printReleaseAgeAssessments(stdout, releaseAges)
	displayed := len(findings) + len(suppressed) + len(assessed) + len(releaseAges)
	total := len(report.Findings) + len(report.Suppressed) + len(report.Assessed) + len(report.ReleaseAges)
	if omitted := total - displayed; omitted > 0 {
		fmt.Fprintf(stdout, "OMITTED %d finding(s) from the bounded terminal view\n", omitted)
	}
	printReportTables(stdout, report.Tables)
	printReportNotes(stdout, report.Notes, verbose)
	if report.ReportPath != "" {
		fmt.Fprintln(stdout, "COMPLETE REPORT:", report.ReportPath)
	}
	if report.Display != nil && report.Display.Omitted > 0 {
		fmt.Fprintf(stdout, "FILTERED %d occurrence(s); displayed %d of %d\n", report.Display.Omitted, report.Display.Displayed, report.Display.Total)
	}
}

func printTestQualityReminder(output io.Writer, reminder *engine.TestQualityReminder) {
	if reminder == nil || len(reminder.ChangedTestPaths) == 0 && reminder.TaskBase == "" {
		return
	}
	fmt.Fprintln(output, "============================================================")
	fmt.Fprintln(output, "TEST QUALITY REMINDER")
	fmt.Fprintf(output, "MERGE-TARGET CHANGED TEST FILES: %d\n", len(reminder.ChangedTestPaths))
	if reminder.TaskBase != "" {
		fmt.Fprintf(output, "LATEST TASK CHANGED TEST FILES: %d since %s\n", len(reminder.TaskChangedTestPaths), reminder.TaskBase)
	}
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "Make sure none of the tests (new or old) are tautological or change-detector tests.")
	fmt.Fprintln(output, "Tautological: derives its expected result from the same production implementation being tested.")
	fmt.Fprintln(output, "Change-detector: mirrors private source spelling, extracted helper text, or collaborator choreography and therefore breaks under a behavior-preserving implementation change.")
	fmt.Fprintln(output, "============================================================")
}

func printReportHeaders(output io.Writer, report engine.Report, verbose bool) {
	printArchitectureReview(output, report)
	if report.ChangedTestScope != nil {
		printChangedTestScope(output, report.ChangedTestScope, verbose)
	}
	if report.MergePolicy != nil {
		printMergePolicyForMode(output, report.MergePolicy, verbose)
	}
	if report.CheckpointPolicy != nil {
		printCheckpointPolicyForMode(output, report.CheckpointPolicy, verbose)
	}
	printBehaviorReview(output, report.BehaviorReview, verbose)
	if report.GateRunPolicy != nil {
		printGateRunPolicy(output, report.GateRunPolicy)
	}
	if report.ChangeBoundary != nil {
		printChangeBoundaryTo(output, report.ChangeBoundary)
	}
}

func printGateRunPolicy(output io.Writer, gateRun *engine.GateRunPolicy) {
	fmt.Fprintln(output, "GATE RUN:", strings.ToUpper(gateRun.Status))
	if gateRun.ReportPath != "" {
		fmt.Fprintln(output, "GATE REPORT:", gateRun.ReportPath)
	}
	if len(gateRun.ReusedPhases) > 0 {
		fmt.Fprintln(output, "REUSED TEST PHASES:", strings.Join(gateRun.ReusedPhases, ", "))
	}
}

func printTestFailureEvidence(output io.Writer, commands []engine.TestCommandEvidence, diagnostics []engine.TestFailureDiagnostic) {
	for _, command := range commands {
		if command.FailureMessage == "" {
			continue
		}
		fmt.Fprintf(output, "TEST EVIDENCE %s attempt=%d category=%s exit=%d\n", command.Name, command.Attempt, command.FailureCategory, command.Result.ExitStatus)
		if len(command.ChangedModuleOverlap) > 0 || len(command.ImpactedModuleOverlap) > 0 {
			fmt.Fprintf(output, "     overlap changed=%s impacted=%s\n", strings.Join(command.ChangedModuleOverlap, ","), strings.Join(command.ImpactedModuleOverlap, ","))
		}
		if command.LogPath != "" {
			fmt.Fprintln(output, "     log", command.LogPath)
		}
	}
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(output, "TEST DIAGNOSIS %s %s\n", diagnostic.Suite, diagnostic.State)
	}
}

func printChangedTestScope(output io.Writer, scope *engine.ChangedTestScope, verbose bool) {
	if verbose {
		fmt.Fprintln(output, "TEST SCOPE COMPARISON:", strings.ToUpper(scope.Comparison))
		if scope.RequestedBase != "" {
			fmt.Fprintln(output, "TEST SCOPE REQUESTED BASE:", scope.RequestedBase)
		}
		fmt.Fprintln(output, "TEST SCOPE EXACT BASE:", scope.ExactBase)
		fmt.Fprintln(output, "TEST SCOPE CANDIDATE:", scope.Candidate)
		fmt.Fprintln(output, "TEST SCOPE GOVERNED PATHS:", scope.GovernedPathCount)
		return
	}
	if scope.Comparison == engine.ChangedTestComparisonMergeBase {
		fmt.Fprintf(output, "TEST SCOPE: merge-base(%s, HEAD) = %s plus working tree (%d governed paths)\n", scope.RequestedBase, scope.ExactBase, scope.GovernedPathCount)
		return
	}
	fmt.Fprintf(output, "TEST SCOPE: working tree against HEAD (%d governed paths)\n", scope.GovernedPathCount)
}

func printCheckpointPolicyForMode(output io.Writer, checkpoint *engine.CheckpointPolicy, verbose bool) {
	if verbose {
		fmt.Fprintln(output, "CHECKPOINT GATE SCOPE:", strings.ToUpper(checkpoint.Scope))
		fmt.Fprintln(output, "CHECKPOINT GATE BASE:", checkpoint.Base)
		fmt.Fprintln(output, "CHECKPOINT GATE CANDIDATE:", checkpoint.Candidate)
		if checkpoint.ReceiptPath != "" {
			fmt.Fprintln(output, "CHECKPOINT GATE RECEIPT:", checkpoint.ReceiptPath)
		}
		return
	}
	fmt.Fprintf(output, "CHECKPOINT GATE: %s against %s\n", strings.ToUpper(checkpoint.Scope), checkpoint.Base)
	if checkpoint.ReceiptPath != "" {
		fmt.Fprintln(output, "CHECKPOINT ACCEPTED:", checkpoint.Candidate)
	}
}

func printMergePolicyForMode(output io.Writer, mergePolicy *engine.MergePolicy, verbose bool) {
	if verbose {
		printMergePolicyTo(output, mergePolicy)
		return
	}
	if mergePolicy.Base == "" {
		fmt.Fprintf(output, "MERGE GATE: %s\n", strings.ToUpper(mergePolicy.Level))
		return
	}
	fmt.Fprintf(output, "MERGE GATE: %s against %s\n", strings.ToUpper(mergePolicy.Level), mergePolicy.Base)
}

func printChangeBoundaryTo(output io.Writer, boundary *engine.ChangeBoundary) {
	fmt.Fprintln(output, "CHANGE BOUNDARY BASE:", boundary.Base)
	fmt.Fprintln(output, "CHANGE BOUNDARY MODULES:", strings.Join(boundary.Modules, ", "))
	if len(boundary.Paths) > 0 {
		fmt.Fprintln(output, "CHANGE BOUNDARY EXACT PATHS:", strings.Join(boundary.Paths, ", "))
	}
	if len(boundary.NewPaths) > 0 {
		fmt.Fprintln(output, "CHANGE BOUNDARY NEW PATHS:", strings.Join(boundary.NewPaths, ", "))
	}
}

func printMergePolicyTo(output io.Writer, mergePolicy *engine.MergePolicy) {
	fmt.Fprintln(output, "MERGE POLICY LEVEL:", strings.ToUpper(mergePolicy.Level))
	if mergePolicy.Base != "" {
		fmt.Fprintln(output, "MERGE POLICY BASE:", mergePolicy.Base)
	}
	for _, reason := range mergePolicy.Reasons {
		fmt.Fprintln(output, "MERGE POLICY REASON:", reason)
	}
}

func operationalError(err error) int {
	fmt.Fprintln(os.Stderr, "operational error:", err)
	return 2
}

func inferPolicyRoot() string {
	executable, err := os.Executable()
	if err == nil {
		if root, found := findPolicyRoot(filepath.Dir(executable)); found {
			return root
		}
	}
	workingDirectory, _ := os.Getwd()
	if root, found := findPolicyRoot(workingDirectory); found {
		return root
	}
	return workingDirectory
}

func findPolicyRoot(start string) (string, bool) {
	current := filepath.Clean(start)
	for {
		version, versionErr := os.Stat(filepath.Join(current, "VERSION"))
		schema, schemaErr := os.Stat(filepath.Join(current, "schema", "code-polishy.schema.json"))
		if versionErr == nil && schemaErr == nil && version.Mode().IsRegular() && schema.Mode().IsRegular() {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}
