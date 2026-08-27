package testing

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	executionStatusPassed     = "passed"
	executionStatusFailed     = "failed"
	executionStatusNotStarted = "not-started"
)

// ExecutionCommand is the terminal-visible, policy-derived identity of one
// command. It carries presentation facts separately from argv.
type ExecutionCommand struct {
	Name               string
	Kind               string
	Scope              string
	Cost               string
	Argv               []string
	Cwd                string
	ExclusiveResources []string
	TimeoutSeconds     int
}

// ExecutionPlan is the visible plan for direct test execution.
type ExecutionPlan struct {
	Stage           string
	Level           string
	TrustedBase     string
	Candidate       string
	Reasons         []string
	ImpactedModules []string
	Commands        []ExecutionCommand
}

// ExecutionResult is emitted only after a command reaches a terminal state.
type ExecutionResult struct {
	Status            string
	ExecutionDuration time.Duration
	ResourceWait      time.Duration
	ResourceWaitKnown bool
}

// ExecutionReporter is the terminal-safe renderer for test execution. Direct
// commands use a concise human view unless the caller requests verbose output.
type ExecutionReporter struct {
	output  io.Writer
	plan    ExecutionPlan
	verbose bool
}

func newExecutionReporter(output io.Writer, plan ExecutionPlan, verbose bool) *ExecutionReporter {
	if output == nil {
		output = io.Discard
	}
	reporter := &ExecutionReporter{output: output, plan: cloneExecutionPlan(plan), verbose: verbose}
	if verbose {
		reporter.renderPlan()
	} else {
		reporter.renderSummary()
	}
	return reporter
}

func (reporter *ExecutionReporter) renderSummary() {
	count := len(reporter.plan.Commands)
	commands := "commands"
	if count == 1 {
		commands = "command"
	}
	fmt.Fprintf(reporter.output, "Running %d validation %s at %s level.\n", count, commands, reporter.plan.Level)
}

func cloneExecutionPlan(plan ExecutionPlan) ExecutionPlan {
	plan.Reasons = append([]string{}, plan.Reasons...)
	plan.ImpactedModules = append([]string{}, plan.ImpactedModules...)
	plan.Commands = append([]ExecutionCommand{}, plan.Commands...)
	for index := range plan.Commands {
		plan.Commands[index].Argv = append([]string{}, plan.Commands[index].Argv...)
		plan.Commands[index].ExclusiveResources = append([]string{}, plan.Commands[index].ExclusiveResources...)
	}
	return plan
}

func (reporter *ExecutionReporter) renderPlan() {
	plan := reporter.plan
	fmt.Fprintf(reporter.output, "EXECUTION PLAN stage=%s level=%s trusted-base=%s candidate=%s\n",
		terminalValue(plan.Stage), terminalValue(plan.Level), terminalValue(plan.TrustedBase), terminalValue(plan.Candidate))
	if len(plan.Reasons) == 0 {
		fmt.Fprintln(reporter.output, "EXECUTION SELECTION reason=\"none\"")
	} else {
		for _, reason := range plan.Reasons {
			fmt.Fprintf(reporter.output, "EXECUTION SELECTION reason=%s\n", terminalValue(reason))
		}
	}
	modules := append([]string{}, plan.ImpactedModules...)
	sort.Strings(modules)
	fmt.Fprintf(reporter.output, "EXECUTION IMPACT modules=%s\n", terminalValue(strings.Join(modules, ",")))
	quick, standard, expensive, checks, suites := executionPlanCounts(plan)
	fmt.Fprintf(reporter.output, "EXECUTION COMMANDS total=%d checks=%d suites=%d quick=%d standard=%d expensive=%d\n",
		len(plan.Commands), checks, suites, quick, standard, expensive)
	if plan.Level == "focused" {
		fmt.Fprintln(reporter.output, "EXECUTION DISCLOSURE focused-not-full=true")
	} else {
		fmt.Fprintln(reporter.output, "EXECUTION DISCLOSURE focused-not-full=false")
	}
	fmt.Fprintf(reporter.output, "EXECUTION DURATION expected=history-unavailable timeout-ceiling=%s\n", executionTimeoutCeiling(plan))
}

func executionPlanCounts(plan ExecutionPlan) (quick, standard, expensive, checks, suites int) {
	for _, command := range plan.Commands {
		if command.Kind == "check" {
			checks++
		} else {
			suites++
		}
		switch command.Cost {
		case "quick":
			quick++
		case "expensive":
			expensive++
		default:
			standard++
		}
	}
	return quick, standard, expensive, checks, suites
}

func executionTimeoutCeiling(plan ExecutionPlan) time.Duration {
	var total time.Duration
	for _, command := range plan.Commands {
		total += time.Duration(command.TimeoutSeconds) * time.Second
	}
	return total
}

// CommandWaiting renders the exact current command before resource acquisition
// begins. This keeps a blocked host resource visible instead of looking like a
// silent hung test.
func (reporter *ExecutionReporter) CommandWaiting(index int) {
	command, ok := reporter.command(index)
	if !ok {
		return
	}
	if !reporter.verbose {
		fmt.Fprintf(reporter.output, "[%d/%d] %s\n", index+1, len(reporter.plan.Commands), command.Name)
		return
	}
	state := "not-required"
	if len(command.ExclusiveResources) > 0 {
		state = "waiting"
	}
	fmt.Fprintf(reporter.output, "EXECUTION PROGRESS command=%d/%d name=%s kind=%s scope=%s cost=%s timeout=%s resources=%s resource-wait=%s elapsed=0s\n",
		index+1, len(reporter.plan.Commands), terminalValue(command.Name), terminalValue(command.Kind),
		terminalValue(command.Scope), terminalValue(command.Cost), time.Duration(command.TimeoutSeconds)*time.Second,
		terminalValue(strings.Join(command.ExclusiveResources, ",")), terminalValue(state))
}

// CommandResourceWaiting renders a periodic update while a command remains
// blocked on one of its declared exclusive resources.
func (reporter *ExecutionReporter) CommandResourceWaiting(index int, elapsed time.Duration) {
	command, ok := reporter.command(index)
	if !ok || len(command.ExclusiveResources) == 0 {
		return
	}
	if !reporter.verbose {
		fmt.Fprintf(reporter.output, "[%d/%d] waiting for %s (%s)\n", index+1, len(reporter.plan.Commands), strings.Join(command.ExclusiveResources, ", "), elapsed)
		return
	}
	fmt.Fprintf(reporter.output, "EXECUTION PROGRESS command=%d/%d name=%s kind=%s scope=%s cost=%s timeout=%s resources=%s resource-wait=%s elapsed=%s\n",
		index+1, len(reporter.plan.Commands), terminalValue(command.Name), terminalValue(command.Kind),
		terminalValue(command.Scope), terminalValue(command.Cost), time.Duration(command.TimeoutSeconds)*time.Second,
		terminalValue(strings.Join(command.ExclusiveResources, ",")), terminalValue("waiting"), elapsed)
}

// CommandFinished renders final command timing and outcome after the process
// completes or fails to start.
func (reporter *ExecutionReporter) CommandFinished(index int, result ExecutionResult) {
	command, ok := reporter.command(index)
	if !ok {
		return
	}
	if !reporter.verbose {
		if result.Status != executionStatusPassed {
			fmt.Fprintf(reporter.output, "[%d/%d] %s: %s after %s\n", index+1, len(reporter.plan.Commands), command.Name, result.Status, result.ExecutionDuration)
		}
		return
	}
	resourceWait := "unavailable"
	elapsed := result.ExecutionDuration
	if result.ResourceWaitKnown {
		resourceWait = result.ResourceWait.String()
		elapsed += result.ResourceWait
	}
	fmt.Fprintf(reporter.output, "EXECUTION RESULT command=%d/%d status=%s execution=%s resource-wait=%s elapsed=%s\n",
		index+1, len(reporter.plan.Commands), terminalValue(result.Status), result.ExecutionDuration, resourceWait, elapsed)
}

// CommandNotStarted records work deliberately suppressed after a failed
// prerequisite. It is output as status rather than evidence of execution.
func (reporter *ExecutionReporter) CommandNotStarted(index int) {
	reporter.CommandFinished(index, ExecutionResult{Status: executionStatusNotStarted, ResourceWaitKnown: true})
}

func (reporter *ExecutionReporter) command(index int) (ExecutionCommand, bool) {
	if index < 0 || index >= len(reporter.plan.Commands) {
		return ExecutionCommand{}, false
	}
	return reporter.plan.Commands[index], true
}

func terminalValue(value string) string {
	return strconv.Quote(value)
}
