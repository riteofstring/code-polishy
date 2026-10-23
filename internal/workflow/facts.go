package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/rhysd/actionlint"
)

const ProtocolVersion = "workflow-facts/v1"
const ParserIdentity = "actionlint@v1.7.12"
const MaximumInputBytes = 2 * 1024 * 1024
const maximumFactsBytes = 4 * 1024 * 1024
const maximumTriggers = 256
const maximumSchedules = 128
const maximumJobs = 512
const maximumSteps = 4096
const maximumStringBytes = 64 * 1024

var shellExecutionVariables = []string{"BASH_ENV", "SHELLOPTS", "PS4"}

type Facts struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ParserIdentity  string     `json:"parserIdentity"`
	Path            string     `json:"path"`
	Triggers        []Trigger  `json:"triggers"`
	Schedules       []Schedule `json:"schedules"`
	Jobs            []Job      `json:"jobs"`
}

type Trigger struct {
	Name   string `json:"name"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Schedule struct {
	Cron     string `json:"cron"`
	Timezone string `json:"timezone,omitempty"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

type Job struct {
	ID             string   `json:"id"`
	Needs          []string `json:"needs"`
	Condition      string   `json:"condition,omitempty"`
	AllowsFailure  bool     `json:"allowsFailure,omitempty"`
	RunsOnSchedule bool     `json:"runsOnSchedule"`
	Uses           string   `json:"uses,omitempty"`
	Line           int      `json:"line"`
	Column         int      `json:"column"`
	Steps          []Step   `json:"steps"`
}

type Step struct {
	ID                  string            `json:"id,omitempty"`
	Condition           string            `json:"condition,omitempty"`
	AllowsFailure       bool              `json:"allowsFailure,omitempty"`
	RunsOnSchedule      bool              `json:"runsOnSchedule"`
	Uses                string            `json:"uses,omitempty"`
	Run                 string            `json:"run,omitempty"`
	Shell               string            `json:"shell,omitempty"`
	ShellStartupUnknown bool              `json:"shellStartupUnknown,omitempty"`
	Inputs              map[string]string `json:"inputs,omitempty"`
	Line                int               `json:"line"`
	Column              int               `json:"column"`
}

type conditionTruth uint8

const (
	conditionUnknown conditionTruth = iota
	conditionFalse
	conditionTrue
)

func Parse(path string, data, configuration []byte) (Facts, error) {
	if len(data) > MaximumInputBytes {
		return Facts{}, fmt.Errorf("workflow exceeds the %d byte limit", MaximumInputBytes)
	}
	if err := validateStrings(path); err != nil {
		return Facts{}, err
	}
	parsed, diagnostics := actionlint.Parse(data)
	if len(diagnostics) > 0 {
		return Facts{}, diagnosticError(diagnostics)
	}
	if parsed == nil {
		return Facts{}, errors.New("actionlint returned no workflow facts")
	}
	config, err := parseConfiguration(configuration)
	if err != nil {
		return Facts{}, err
	}
	if diagnostics, err := semanticDiagnostics(path, parsed, config); err != nil {
		return Facts{}, err
	} else if len(diagnostics) > 0 {
		return Facts{}, diagnosticError(diagnostics)
	}
	facts, err := extractFacts(path, parsed)
	if err != nil {
		return Facts{}, err
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return Facts{}, fmt.Errorf("encode workflow facts: %w", err)
	}
	if len(encoded) > maximumFactsBytes {
		return Facts{}, fmt.Errorf("workflow facts exceed the %d byte limit", maximumFactsBytes)
	}
	return facts, nil
}

func semanticDiagnostics(path string, parsed *actionlint.Workflow, config *actionlint.Config) ([]*actionlint.Error, error) {
	actions := actionlint.NewLocalActionsCache(nil, nil)
	workflows := actionlint.NewLocalReusableWorkflowCache(nil, "", nil)
	rules := []actionlint.Rule{
		actionlint.NewRuleMatrix(),
		actionlint.NewRuleCredentials(),
		actionlint.NewRuleShellName(),
		actionlint.NewRuleRunnerLabel(),
		actionlint.NewRuleEvents(),
		actionlint.NewRuleJobNeeds(),
		actionlint.NewRuleAction(actions),
		actionlint.NewRuleEnvVar(),
		actionlint.NewRuleID(),
		actionlint.NewRuleGlob(),
		actionlint.NewRulePermissions(),
		actionlint.NewRuleWorkflowCall(path, workflows),
		actionlint.NewRuleExpression(actions, workflows),
		actionlint.NewRuleDeprecatedCommands(),
		actionlint.NewRuleIfCond(),
	}
	visitor := actionlint.NewVisitor()
	for _, rule := range rules {
		rule.SetConfig(config)
		visitor.AddPass(rule)
	}
	if err := visitor.Visit(parsed); err != nil {
		return nil, fmt.Errorf("evaluate actionlint workflow rules: %w", err)
	}
	diagnostics := []*actionlint.Error{}
	for _, rule := range rules {
		diagnostics = append(diagnostics, rule.Errs()...)
	}
	return diagnostics, nil
}

func extractFacts(path string, parsed *actionlint.Workflow) (Facts, error) {
	facts := Facts{ProtocolVersion: ProtocolVersion, ParserIdentity: ParserIdentity, Path: path, Triggers: []Trigger{}, Schedules: []Schedule{}, Jobs: []Job{}}
	triggers, schedules, err := extractTriggers(parsed.On)
	if err != nil {
		return Facts{}, err
	}
	facts.Triggers = triggers
	facts.Schedules = schedules
	if len(triggers) > maximumTriggers || len(schedules) > maximumSchedules || len(parsed.Jobs) > maximumJobs {
		return Facts{}, errors.New("workflow fact count exceeds the adapter limit")
	}
	facts.Jobs, err = extractJobs(parsed.Jobs, defaultRunShell(parsed.Defaults), parsed.Env)
	if err != nil {
		return Facts{}, err
	}
	return facts, nil
}

func extractTriggers(events []actionlint.Event) ([]Trigger, []Schedule, error) {
	triggers := []Trigger{}
	schedules := []Schedule{}
	for _, event := range events {
		line, column := eventPosition(event)
		name := event.EventName()
		if err := validateStrings(name); err != nil {
			return nil, nil, err
		}
		if err := validatePosition(line, column); err != nil {
			return nil, nil, err
		}
		triggers = append(triggers, Trigger{Name: name, Line: line, Column: column})
		if scheduled, ok := event.(*actionlint.ScheduledEvent); ok {
			entries, err := extractSchedules(scheduled.Schedules)
			if err != nil {
				return nil, nil, err
			}
			schedules = append(schedules, entries...)
		}
	}
	return triggers, schedules, nil
}

func extractSchedules(entries []*actionlint.ScheduleEntry) ([]Schedule, error) {
	schedules := make([]Schedule, 0, len(entries))
	for _, entry := range entries {
		timezone := ""
		if entry.Timezone != nil {
			timezone = entry.Timezone.Value
		}
		if err := validateStrings(entry.Cron.Value, timezone); err != nil {
			return nil, err
		}
		if err := validatePosition(entry.Cron.Pos.Line, entry.Cron.Pos.Col); err != nil {
			return nil, err
		}
		schedules = append(schedules, Schedule{Cron: entry.Cron.Value, Timezone: timezone, Line: entry.Cron.Pos.Line, Column: entry.Cron.Pos.Col})
	}
	return schedules, nil
}

func extractJobs(sources map[string]*actionlint.Job, defaultShell string, environment *actionlint.Env) ([]Job, error) {
	jobIDs := make([]string, 0, len(sources))
	for id := range sources {
		jobIDs = append(jobIDs, id)
	}
	sort.Strings(jobIDs)
	steps := 0
	jobs := make([]Job, 0, len(jobIDs))
	for _, id := range jobIDs {
		job, err := extractJob(id, sources[id], defaultShell, environment)
		if err != nil {
			return nil, err
		}
		steps += len(job.Steps)
		if steps > maximumSteps {
			return nil, errors.New("workflow step count exceeds the adapter limit")
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func extractJob(id string, source *actionlint.Job, defaultShell string, environment *actionlint.Env) (Job, error) {
	line, column := position(source.Pos)
	if err := validatePosition(line, column); err != nil {
		return Job{}, err
	}
	job := Job{ID: id, Needs: []string{}, RunsOnSchedule: conditionRunsOnSchedule(source.If), Line: line, Column: column, Steps: []Step{}}
	job.AllowsFailure = allowsFailure(source.ContinueOnError)
	for _, need := range source.Needs {
		job.Needs = append(job.Needs, strings.ToLower(need.Value))
	}
	if source.If != nil {
		job.Condition = source.If.Value
	}
	if source.WorkflowCall != nil && source.WorkflowCall.Uses != nil {
		job.Uses = source.WorkflowCall.Uses.Value
	}
	defaultShell = jobRunShell(source, defaultShell)
	for _, sourceStep := range source.Steps {
		step, err := extractStep(sourceStep, defaultShell)
		if err != nil {
			return Job{}, err
		}
		step.ShellStartupUnknown = jobStepShellStartup(source, sourceStep, environment)
		job.Steps = append(job.Steps, step)
	}
	values := append([]string{job.ID, job.Condition, job.Uses}, job.Needs...)
	if err := validateStrings(values...); err != nil {
		return Job{}, err
	}
	return job, nil
}

func jobStepShellStartup(job *actionlint.Job, step *actionlint.Step, workflowEnvironment *actionlint.Env) bool {
	environments := []*actionlint.Env{nil, workflowEnvironment, job.Env, step.Env}
	if job.Container != nil {
		environments[0] = job.Container.Env
	}
	for _, name := range shellExecutionVariables {
		if shellEnvironmentMayExecute(name, environments) {
			return true
		}
	}
	return false
}

func shellEnvironmentMayExecute(name string, environments []*actionlint.Env) bool {
	mayExecute := false
	for _, environment := range environments {
		if environment == nil {
			continue
		}
		if environment.Expression != nil {
			return true
		}
		variable := environment.Vars[strings.ToLower(name)]
		if variable == nil {
			continue
		}
		unsafe := shellVariableMayExecute(name, variable.Value.Value)
		if variable.Name.Value == name {
			mayExecute = unsafe
		} else {
			mayExecute = mayExecute || unsafe
		}
	}
	return mayExecute
}

func shellVariableMayExecute(name, value string) bool {
	if name == "PS4" {
		return strings.ContainsAny(value, "$`\\")
	}
	return value != ""
}

func extractStep(source *actionlint.Step, defaultShell string) (Step, error) {
	line, column := position(source.Pos)
	if err := validatePosition(line, column); err != nil {
		return Step{}, err
	}
	step := Step{RunsOnSchedule: conditionRunsOnSchedule(source.If), Line: line, Column: column}
	step.AllowsFailure = allowsFailure(source.ContinueOnError)
	if source.ID != nil {
		step.ID = source.ID.Value
	}
	if source.If != nil {
		step.Condition = source.If.Value
	}
	switch execution := source.Exec.(type) {
	case *actionlint.ExecAction:
		if execution.Uses != nil {
			step.Uses = execution.Uses.Value
		}
		step.Inputs = map[string]string{}
		for name, input := range execution.Inputs {
			if err := validateStrings(name, input.Value.Value); err != nil {
				return Step{}, err
			}
			step.Inputs[name] = input.Value.Value
		}
	case *actionlint.ExecRun:
		step.Shell = stepRunShell(execution, defaultShell)
		if execution.Run != nil {
			step.Run = execution.Run.Value
		}
	}
	if err := validateStrings(step.ID, step.Condition, step.Uses, step.Run, step.Shell); err != nil {
		return Step{}, err
	}
	return step, nil
}

func stepRunShell(execution *actionlint.ExecRun, defaultShell string) string {
	if execution.Shell != nil {
		return execution.Shell.Value
	}
	return defaultShell
}

func defaultRunShell(defaults *actionlint.Defaults) string {
	if defaults != nil && defaults.Run != nil && defaults.Run.Shell != nil {
		return defaults.Run.Shell.Value
	}
	return ""
}

func jobRunShell(source *actionlint.Job, workflowShell string) string {
	if shell := defaultRunShell(source.Defaults); shell != "" {
		return shell
	}
	if workflowShell != "" {
		return workflowShell
	}
	if source.Container != nil {
		return "sh"
	}
	shell := ""
	if source.RunsOn != nil {
		for _, label := range source.RunsOn.Labels {
			candidate := runnerLabelShell(label.Value)
			if candidate == "" {
				continue
			}
			if shell != "" && shell != candidate {
				return ""
			}
			shell = candidate
		}
	}
	return shell
}

func runnerLabelShell(label string) string {
	label = strings.ToLower(label)
	switch {
	case label == "windows", strings.HasPrefix(label, "windows-"):
		return "pwsh"
	case label == "linux", label == "macos", strings.HasPrefix(label, "ubuntu-"), strings.HasPrefix(label, "macos-"):
		return "bash"
	default:
		return ""
	}
}

func allowsFailure(value *actionlint.Bool) bool {
	return value != nil && (value.Value || value.Expression != nil)
}

func conditionRunsOnSchedule(condition *actionlint.String) bool {
	if condition == nil {
		return true
	}
	return ConditionRunsOnEvent(condition.Value, "schedule")
}

func ConditionRunsOnEvent(source, event string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return true
	}
	if strings.HasPrefix(source, "${{") && strings.HasSuffix(source, "}}") {
		source = strings.TrimSpace(source[len("${{") : len(source)-len("}}")])
	}
	expression, err := actionlint.NewExprParser().Parse(actionlint.NewExprLexer(source + "}}"))
	if err != nil {
		return false
	}
	return evaluateEventCondition(expression, event) == conditionTrue
}

func evaluateEventCondition(expression actionlint.ExprNode, event string) conditionTruth {
	switch typed := expression.(type) {
	case *actionlint.BoolNode:
		if typed.Value {
			return conditionTrue
		}
		return conditionFalse
	case *actionlint.NotOpNode:
		return invertCondition(evaluateEventCondition(typed.Operand, event))
	case *actionlint.CompareOpNode:
		return evaluateEventComparison(typed, event)
	case *actionlint.LogicalOpNode:
		return evaluateEventLogical(typed, event)
	case *actionlint.FuncCallNode:
		if (strings.EqualFold(typed.Callee, "always") || strings.EqualFold(typed.Callee, "success")) && len(typed.Args) == 0 {
			return conditionTrue
		}
	}
	return conditionUnknown
}

func evaluateEventLogical(expression *actionlint.LogicalOpNode, event string) conditionTruth {
	left := evaluateEventCondition(expression.Left, event)
	right := evaluateEventCondition(expression.Right, event)
	if expression.Kind == actionlint.LogicalOpNodeKindAnd {
		if left == conditionFalse || right == conditionFalse {
			return conditionFalse
		}
		if left == conditionTrue && right == conditionTrue {
			return conditionTrue
		}
		return conditionUnknown
	}
	if expression.Kind == actionlint.LogicalOpNodeKindOr {
		if left == conditionTrue || right == conditionTrue {
			return conditionTrue
		}
		if left == conditionFalse && right == conditionFalse {
			return conditionFalse
		}
	}
	return conditionUnknown
}

func invertCondition(value conditionTruth) conditionTruth {
	switch value {
	case conditionTrue:
		return conditionFalse
	case conditionFalse:
		return conditionTrue
	default:
		return conditionUnknown
	}
}

func evaluateEventComparison(expression *actionlint.CompareOpNode, event string) conditionTruth {
	if expression.Kind != actionlint.CompareOpNodeKindEq && expression.Kind != actionlint.CompareOpNodeKindNotEq {
		return conditionUnknown
	}
	left, leftKnown := eventConditionValue(expression.Left, event)
	right, rightKnown := eventConditionValue(expression.Right, event)
	if !leftKnown || !rightKnown {
		return conditionUnknown
	}
	equal := strings.EqualFold(left, right)
	if expression.Kind == actionlint.CompareOpNodeKindNotEq {
		equal = !equal
	}
	if equal {
		return conditionTrue
	}
	return conditionFalse
}

func eventConditionValue(expression actionlint.ExprNode, event string) (string, bool) {
	switch typed := expression.(type) {
	case *actionlint.StringNode:
		return typed.Value, true
	case *actionlint.ObjectDerefNode:
		variable, ok := typed.Receiver.(*actionlint.VariableNode)
		if ok && strings.EqualFold(variable.Name, "github") && strings.EqualFold(typed.Property, "event_name") {
			return event, true
		}
	}
	return "", false
}

func eventPosition(event actionlint.Event) (int, int) {
	switch typed := event.(type) {
	case *actionlint.ScheduledEvent:
		return position(typed.Pos)
	case *actionlint.WebhookEvent:
		return position(typed.Pos)
	case *actionlint.WorkflowDispatchEvent:
		return position(typed.Pos)
	case *actionlint.RepositoryDispatchEvent:
		return position(typed.Pos)
	case *actionlint.WorkflowCallEvent:
		return position(typed.Pos)
	}
	return 0, 0
}

func position(value *actionlint.Pos) (int, int) {
	if value == nil {
		return 0, 0
	}
	return value.Line, value.Col
}

func validateStrings(values ...string) error {
	for _, value := range values {
		if len(value) > maximumStringBytes || !utf8.ValidString(value) {
			return fmt.Errorf("workflow fact string exceeds the %d byte or encoding limit", maximumStringBytes)
		}
	}
	return nil
}

func validatePosition(line, column int) error {
	if line < 1 || column < 1 {
		return errors.New("workflow fact has an invalid source position")
	}
	return nil
}

func diagnosticError(diagnostics []*actionlint.Error) error {
	diagnostics = append([]*actionlint.Error{}, diagnostics...)
	sort.Slice(diagnostics, func(left, right int) bool {
		if diagnostics[left].Line != diagnostics[right].Line {
			return diagnostics[left].Line < diagnostics[right].Line
		}
		if diagnostics[left].Column != diagnostics[right].Column {
			return diagnostics[left].Column < diagnostics[right].Column
		}
		if diagnostics[left].Kind != diagnostics[right].Kind {
			return diagnostics[left].Kind < diagnostics[right].Kind
		}
		return diagnostics[left].Message < diagnostics[right].Message
	})
	parts := make([]string, 0, min(len(diagnostics), 8))
	for _, diagnostic := range diagnostics[:min(len(diagnostics), 8)] {
		message := strings.Join(strings.Fields(diagnostic.Message), " ")
		parts = append(parts, fmt.Sprintf("%d:%d %s: %s", diagnostic.Line, diagnostic.Column, diagnostic.Kind, message))
	}
	if len(diagnostics) > len(parts) {
		parts = append(parts, fmt.Sprintf("%d additional diagnostics", len(diagnostics)-len(parts)))
	}
	return fmt.Errorf("actionlint rejected workflow: %s", strings.Join(parts, "; "))
}
