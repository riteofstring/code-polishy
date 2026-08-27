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

	"github.com/riteofstring/code-polishy/internal/agents"
	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

const usage = `Usage: code-polishy [global options] <command> [options]

Commands:
  version
  agents <install|sync|check>
  lock
  release-manifest <write|verify|materialize> [options]
  change-boundary --base COMMIT --module NAME... [--allow-path PATH...] [--allow-new-path PATH...]
  task-session --module NAME... [options] -- COMMAND [ARG...]
  check [--git-changes|--staged|--all|--files PATH...|--name NAME...]
  gate
  merge-gate --base REF
  test [--changed [--base REF]|--recommended [--base REF]|--all|--supplemental|--module NAME...|--suite NAME...]
  test-plan [--base REF]
  test-levels [--base REF]
  verify [--tests-only]
  architecture [--git-changes|--staged|--all|--files PATH...]
  supply-chain [--offline]
  dependency-review --base REF
  artifact-security
  doctor [--strict]
  format [--git-changes|--staged|--all|--files PATH...]
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
	if status, governed := requireLockedRelease(invocation); !governed {
		return status
	}
	if status, handled := handleMetaCommand(invocation); handled {
		return status
	}
	policyEngine, err := engine.Open(invocation.repoRoot, invocation.policyRoot, invocation.configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		return 2
	}
	policyEngine.Verbose = invocation.verbose
	handler, exists := commandHandlers()[invocation.command]
	if !exists {
		return usageError("unknown command " + invocation.command)
	}
	result, err := handler(context.Background(), policyEngine, invocation.arguments)
	if err != nil {
		if result.report.MergePolicy != nil {
			printMergePolicyForMode(os.Stdout, result.report.MergePolicy, invocation.verbose)
		}
		return operationalError(err)
	}
	if result.quiet {
		return 0
	}
	printReport(result.report, invocation.verbose)
	if engine.HasFindings(result.report) {
		return 1
	}
	return 0
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
	case "agents":
		return handleAgentsMeta(invocation), true
	case "lock":
		return handleLockMeta(invocation), true
	case "install-bundle":
		// scripts/install.ps1 invokes this from the locally built release. It is
		// an installation implementation detail, not a public acquisition path.
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
	options, err := parseChangeBoundaryOptions(invocation.arguments)
	if err != nil {
		return usageError(err.Error())
	}
	if options.base == "" || len(options.modules) == 0 {
		return usageError("change-boundary requires --base COMMIT and at least one --module NAME")
	}
	report, err := engine.CheckChangeBoundary(
		invocation.repoRoot, invocation.policyRoot, invocation.configPath, options.base, options.modules, options.paths, options.newPaths,
	)
	if err != nil {
		return operationalError(err)
	}
	printReport(report, invocation.verbose)
	if engine.HasFindings(report) {
		return 1
	}
	return 0
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
		return usageError("agents requires exactly one of install, sync, or check")
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
		return usageError("agents requires exactly one of install, sync, or check")
	}
}

// requireLockedRelease keeps an installed release from governing a repository
// that requires another one, so running a release binary directly is not a way
// around the lock the launcher enforces. Exempt commands either establish or
// report release identity before a repository has a usable lock, or operate
// only on an explicit release tree without governing the target repository.
func requireLockedRelease(invocation invocation) (int, bool) {
	exempt := []string{"lock", "install-bundle", "release-manifest", "version", "--version", "help", "--help", "-h"}
	if slices.Contains(exempt, invocation.command) {
		return 0, true
	}
	if err := release.RequireLockedRelease(invocation.repoRoot, invocation.policyRoot); err != nil {
		return operationalError(err), false
	}
	return 0, true
}

// handleLockMeta writes the lock that requires the running release. It is the
// only operation that creates or changes a target lock: every other command
// reads one.
func handleLockMeta(invocation invocation) int {
	if len(invocation.arguments) != 0 {
		return usageError("lock does not accept options")
	}
	manifest, installed, err := release.ReadManifest(invocation.policyRoot)
	if err != nil {
		return operationalError(err)
	}
	if !installed {
		return operationalError(fmt.Errorf(
			"%s is a Code Polishy source checkout rather than an installed release, and a source checkout cannot satisfy a target lock",
			invocation.policyRoot,
		))
	}
	lock := release.LockFor(manifest)
	path := filepath.Join(invocation.repoRoot, release.LockFilename)
	if err := os.WriteFile(path, release.RenderLock(lock), 0o644); err != nil {
		return operationalError(fmt.Errorf("write %s: %w", path, err))
	}
	fmt.Printf("PASS %s requires Code Polishy %s %s\n", release.LockFilename, lock.CodePolishyVersion, lock.ReleaseDigest)
	return 0
}

func printVersion(invocation invocation) int {
	if len(invocation.arguments) != 0 {
		return usageError("version does not accept options")
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
	report engine.Report
	quiet  bool
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
		"doctor":            handleDoctor,
		"gate":              handleGate,
		"merge-gate":        handleMergeGate,
		"check":             handleCheck,
		"architecture":      handleSelectionCommand("architecture"),
		"format":            handleSelectionCommand("format"),
		"fix":               handleSelectionCommand("format"),
		"list-files":        handleSelectionCommand("list-files"),
		"test":              handleTest,
		"test-plan":         handleTestPlan,
		"test-levels":       handleTestLevels,
		"verify":            handleVerify,
		"supply-chain":      handleSupplyChain,
		"dependency-review": handleDependencyReview,
		"artifact-security": handleArtifactSecurity,
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
		return commandResult{}, err
	}
	if flags.NArg() != 0 || len(names) == 0 {
		return commandResult{}, errors.New("check --name requires at least one configured check name and no selection options")
	}
	report, err := policyEngine.CheckNamed(ctx, names)
	return commandResult{report: report}, err
}

func handleDoctor(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if !onlyAllowed(arguments, "--strict") {
		return commandResult{}, fmt.Errorf("doctor accepts only --strict")
	}
	report, err := policyEngine.Doctor(ctx)
	return commandResult{report: report}, err
}

func handleGate(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if len(arguments) != 0 {
		return commandResult{}, fmt.Errorf("gate does not accept options")
	}
	report, err := policyEngine.Gate(ctx)
	return commandResult{report: report}, err
}

func handleMergeGate(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	base, err := parseRequiredBaseOption("merge-gate", arguments)
	if err != nil {
		return commandResult{}, err
	}
	report, err := policyEngine.MergeGate(ctx, base)
	return commandResult{report: report}, err
}

func handleSelectionCommand(operation string) commandHandler {
	return func(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
		mode, files, err := parseSelection(arguments)
		if err != nil {
			return commandResult{}, err
		}
		selection, err := policyEngine.Select(mode, files)
		if err != nil {
			return commandResult{}, err
		}
		switch operation {
		case "check":
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
		return commandResult{}, err
	}
	report, err := policyEngine.TestPlan(base)
	return commandResult{report: report}, err
}

func handleTestLevels(_ context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	base, err := parseBaseOption("test-levels", arguments)
	if err != nil {
		return commandResult{}, err
	}
	report, err := policyEngine.TestPlan(base)
	return commandResult{report: report}, err
}

func handleVerify(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if !onlyAllowed(arguments, "--tests-only") {
		return commandResult{}, fmt.Errorf("verify accepts only --tests-only")
	}
	report, err := policyEngine.Verify(ctx, len(arguments) == 1)
	return commandResult{report: report}, err
}

func handleSupplyChain(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if !onlyAllowed(arguments, "--offline") {
		return commandResult{}, fmt.Errorf("supply-chain accepts only --offline")
	}
	report, err := policyEngine.SupplyChain(ctx, len(arguments) == 1)
	return commandResult{report: report}, err
}

func handleDependencyReview(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	base, err := parseRequiredBaseOption("dependency-review", arguments)
	if err != nil {
		return commandResult{}, err
	}
	report, err := policyEngine.DependencyReview(ctx, base)
	return commandResult{report: report}, err
}

func handleArtifactSecurity(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	if len(arguments) != 0 {
		return commandResult{}, fmt.Errorf("artifact-security does not accept options")
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

func parseSelection(arguments []string) (string, []string, error) {
	mode := "changes"
	files := []string{}
	modeSelected := false
	for index := 0; index < len(arguments); index++ {
		selected, known := selectionMode(arguments[index])
		if !known {
			return "", nil, fmt.Errorf("unknown selection option %q", arguments[index])
		}
		if modeSelected {
			return "", nil, fmt.Errorf("choose only one file selection mode")
		}
		mode, modeSelected = selected, true
		if mode != "files" {
			continue
		}
		index++
		for index < len(arguments) && !strings.HasPrefix(arguments[index], "--") {
			files = append(files, arguments[index])
			index++
		}
		index--
	}
	if mode == "files" && len(files) == 0 {
		return "", nil, fmt.Errorf("--files needs at least one path")
	}
	return mode, files, nil
}

func selectionMode(option string) (string, bool) {
	switch option {
	case "--git-changes":
		return "changes", true
	case "--staged":
		return "staged", true
	case "--all":
		return "all", true
	case "--files":
		return "files", true
	default:
		return "", false
	}
}

func parseTestRequest(policyEngine *engine.Engine, arguments []string) (testpolicy.Request, error) {
	options, err := parseTestOptions(arguments)
	if err != nil {
		return options.request, err
	}
	if err := validateTestOptions(options); err != nil {
		return options.request, err
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
	flags.BoolVar(&options.request.Supplemental, "supplemental", false, "run separately staged test-strength suites")
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

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func onlyAllowed(arguments []string, allowed string) bool {
	return len(arguments) == 0 || len(arguments) == 1 && arguments[0] == allowed
}

func printReport(report engine.Report, verbose bool) {
	printReportWithMode(os.Stdout, os.Stderr, report, verbose)
}

func printReportTo(stdout, stderr io.Writer, report engine.Report) {
	printReportWithMode(stdout, stderr, report, false)
}

func printReportWithMode(stdout, stderr io.Writer, report engine.Report, verbose bool) {
	printReportHeaders(stdout, report, verbose)
	for _, finding := range report.Findings {
		fmt.Fprintf(stderr, "FAIL %-34s %s [%s]\n     %s\n", finding.Check, finding.Path, finding.Subject, finding.Message)
	}
	for _, suppressed := range report.Suppressed {
		fmt.Fprintf(stdout, "WAIVED %-32s %s [%s] by %s\n", suppressed.Finding.Check, suppressed.Finding.Path, suppressed.Finding.Subject, suppressed.Exception.ID)
	}
	for _, assessed := range report.Assessed {
		observedSeverity := "unknown"
		if assessed.Finding.Vulnerability != nil {
			observedSeverity = policy.NormalizeVulnerabilitySeverity(assessed.Finding.Vulnerability.Severity)
		}
		fmt.Fprintf(
			stdout, "VULN-ACCEPTANCE %-23s %s [%s] by %s (observed %s, ceiling %s, %s/%s, approved by %s, expires %s)\n",
			assessed.Finding.Check, assessed.Finding.Path, assessed.Finding.Subject, assessed.Assessment.ID,
			observedSeverity, assessed.Assessment.Severity, assessed.Assessment.Status, assessed.Assessment.Basis, assessed.Assessment.ApprovedBy,
			assessed.Assessment.Expires.Format("2006-01-02"),
		)
	}
	for _, assessed := range report.ReleaseAges {
		fmt.Fprintf(stdout, "AGE-EXCEPTION %-25s %s [%s] by %s (%s)\n", assessed.Finding.Check, assessed.Finding.Path, assessed.Finding.Subject, assessed.Assessment.ID, assessed.Assessment.Category)
	}
	for _, advisory := range report.Advisories {
		fmt.Fprintf(stdout, "WARN %-34s %s [%s]\n     %s\n", advisory.Check, advisory.Path, advisory.Subject, advisory.Message)
	}
	for _, table := range report.Tables {
		printTable(stdout, table)
	}
	if verbose {
		for _, note := range report.Notes {
			fmt.Fprintln(stdout, "NOTE", note)
		}
	}
	if len(report.Findings) == 0 {
		fmt.Fprintln(stdout, "PASS policy completed without findings")
	} else {
		fmt.Fprintf(stderr, "FAILED with %d finding(s)\n", len(report.Findings))
	}
}

func printReportHeaders(output io.Writer, report engine.Report, verbose bool) {
	if report.MergePolicy != nil {
		printMergePolicyForMode(output, report.MergePolicy, verbose)
	}
	if report.ChangeBoundary != nil {
		printChangeBoundaryTo(output, report.ChangeBoundary)
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

func printTable(output io.Writer, table engine.Table) {
	if len(table.Columns) == 0 {
		return
	}
	widths := make([]int, len(table.Columns))
	for index, column := range table.Columns {
		widths[index] = len(column)
	}
	for _, row := range table.Rows {
		for index := range table.Columns {
			cell := tableCell(row, index)
			if len(cell) > widths[index] {
				widths[index] = len(cell)
			}
		}
	}
	if table.Title != "" {
		fmt.Fprintln(output, table.Title)
	}
	printTableBorder(output, widths)
	printTableRow(output, table.Columns, widths)
	printTableBorder(output, widths)
	for _, row := range table.Rows {
		printTableRow(output, row, widths)
	}
	printTableBorder(output, widths)
}

func printTableBorder(output io.Writer, widths []int) {
	fmt.Fprint(output, "+")
	for _, width := range widths {
		fmt.Fprint(output, strings.Repeat("-", width+2), "+")
	}
	fmt.Fprintln(output)
}

func printTableRow(output io.Writer, row []string, widths []int) {
	fmt.Fprint(output, "|")
	for index, width := range widths {
		fmt.Fprintf(output, " %-*s |", width, tableCell(row, index))
	}
	fmt.Fprintln(output)
}

func tableCell(row []string, index int) string {
	if index >= len(row) {
		return ""
	}
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(row[index])
}

func usageError(message string) int {
	fmt.Fprintln(os.Stderr, "usage error:", message)
	fmt.Fprint(os.Stderr, usage)
	return 2
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
