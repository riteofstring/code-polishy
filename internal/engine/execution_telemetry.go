package engine

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type executionPhaseKey struct{}

type executionRecorder struct {
	mutex    sync.Mutex
	started  time.Time
	phases   []ExecutionPhase
	commands []ExecutionCommand
}

func (engine *Engine) BeginExecution(started time.Time, initialization time.Duration) {
	recorder := &executionRecorder{started: started}
	recorder.addPhase("repository-open", initialization)
	engine.execution = recorder
	engine.Runner = &executionRunner{delegate: engine.Runner, recorder: recorder}
}

func (engine *Engine) AttachExecution(report Report) Report {
	if engine.execution == nil {
		return report
	}
	report.Execution = engine.execution.snapshot(engine.Repository, report)
	return report
}

func (engine *Engine) executionPhase(ctx context.Context, name string, action func(context.Context)) {
	if engine.execution == nil {
		action(ctx)
		return
	}
	started := time.Now()
	action(context.WithValue(ctx, executionPhaseKey{}, name))
	engine.execution.addPhase(name, time.Since(started))
}

func (recorder *executionRecorder) addPhase(name string, duration time.Duration) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.phases = append(recorder.phases, ExecutionPhase{Name: name, DurationMilliseconds: duration.Milliseconds()})
}

func (recorder *executionRecorder) addCommand(ctx context.Context, command policy.Command, result runner.Result, elapsed time.Duration, runErr error) {
	phase, _ := ctx.Value(executionPhaseKey{}).(string)
	exitStatus := result.ExitStatus
	if runErr != nil && exitStatus == 0 {
		exitStatus = -1
	}
	entry := ExecutionCommand{
		Phase: phase, Name: command.Name, Cwd: command.Cwd, Argv: slices.Clone(command.Argv),
		DurationMilliseconds: elapsed.Milliseconds(), ResourceWaitMillis: result.ResourceWait.Milliseconds(), ExitStatus: exitStatus,
		FailureCategory: string(runner.FailureCategoryFor(ctx, result, runErr)),
	}
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.commands = append(recorder.commands, entry)
}

func (recorder *executionRecorder) addCommandPhases(ctx context.Context, command policy.Command, phases []runner.CommandPhase) {
	phase, _ := ctx.Value(executionPhaseKey{}).(string)
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	for index := len(recorder.commands) - 1; index >= 0; index-- {
		entry := &recorder.commands[index]
		if entry.Name != command.Name || entry.Phase != phase {
			continue
		}
		entry.Phases = make([]ExecutionPhase, 0, len(phases))
		for _, child := range phases {
			entry.Phases = append(entry.Phases, ExecutionPhase{Name: child.Name, DurationMilliseconds: child.Duration.Milliseconds()})
		}
		return
	}
}

func (recorder *executionRecorder) snapshot(repo interface {
	PythonProjectInventoryCacheStats() (int64, int64, int64)
	PathFactCacheStats() (int64, int64, int64)
}, report Report) *ExecutionTelemetry {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	hits, misses, builds := repo.PythonProjectInventoryCacheStats()
	pathHits, pathMisses, pathBuilds := repo.PathFactCacheStats()
	globHits, globMisses, globBuilds := policy.GlobCacheStats()
	return &ExecutionTelemetry{
		EvaluationDurationMilliseconds: time.Since(recorder.started).Milliseconds(),
		Scope:                          executionScope(report),
		Phases:                         slices.Clone(recorder.phases),
		Commands:                       slices.Clone(recorder.commands),
		Caches: []ExecutionCache{{
			Name: "python-project-inventory", Scope: "process", Hits: hits, Misses: misses, Builds: builds,
		}, {
			Name: "repository-path-facts", Scope: "repository", Hits: pathHits, Misses: pathMisses, Builds: pathBuilds,
		}, {
			Name: "glob-patterns", Scope: "process", Hits: globHits, Misses: globMisses, Builds: globBuilds,
		}},
	}
}

func executionScope(report Report) ExecutionScope {
	scope := ExecutionScope{}
	if report.RequestedSelection != nil {
		scope.RequestedOperands = len(report.RequestedSelection.Operands) + len(report.RequestedSelection.Modules)
		scope.SelectedPaths = len(report.RequestedSelection.Expanded)
	}
	paths := map[string]bool{}
	for _, context := range report.AnalysisContext {
		for _, path := range context.Paths {
			paths[path] = true
		}
	}
	scope.ContextPaths = len(paths)
	if report.SourceDependencyGraph != nil {
		scope.GraphNodes = len(report.SourceDependencyGraph.Nodes)
		scope.GraphEdges = len(report.SourceDependencyGraph.Edges)
	}
	return scope
}

type executionRunner struct {
	delegate runner.Runner
	recorder *executionRecorder
}

type resultRunner interface {
	RunWithResult(context.Context, string, policy.Command) (runner.Result, error)
}

func (commandRunner *executionRunner) RecordCommandPhases(ctx context.Context, command policy.Command, phases []runner.CommandPhase) {
	commandRunner.recorder.addCommandPhases(ctx, command, phases)
}

func (commandRunner *executionRunner) Run(ctx context.Context, root string, command policy.Command) error {
	started := time.Now()
	if delegate, ok := commandRunner.delegate.(resultRunner); ok {
		result, err := delegate.RunWithResult(ctx, root, command)
		commandRunner.recorder.addCommand(ctx, command, result, time.Since(started), err)
		return err
	}
	err := commandRunner.delegate.Run(ctx, root, command)
	commandRunner.recorder.addCommand(ctx, command, runner.Result{}, time.Since(started), err)
	return err
}

func (commandRunner *executionRunner) RunWithOutput(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	delegate, ok := commandRunner.delegate.(runner.OutputRunner)
	if !ok {
		return commandRunner.unsupported(ctx, command, "captured output")
	}
	started := time.Now()
	result, output, err := delegate.RunWithOutput(ctx, root, command)
	commandRunner.recorder.addCommand(ctx, command, result, time.Since(started), err)
	return result, output, err
}

func (commandRunner *executionRunner) RunStructured(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	delegate, ok := commandRunner.delegate.(runner.StructuredRunner)
	if !ok {
		return commandRunner.unsupported(ctx, command, "structured output")
	}
	started := time.Now()
	result, output, err := delegate.RunStructured(ctx, root, command)
	commandRunner.recorder.addCommand(ctx, command, result, time.Since(started), err)
	return result, output, err
}

func (commandRunner *executionRunner) RunWithWriters(ctx context.Context, root string, command policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	delegate, ok := commandRunner.delegate.(runner.StreamRunner)
	if !ok {
		result, _, err := commandRunner.unsupported(ctx, command, "streamed output")
		return result, err
	}
	started := time.Now()
	result, err := delegate.RunWithWriters(ctx, root, command, stdout, stderr)
	commandRunner.recorder.addCommand(ctx, command, result, time.Since(started), err)
	return result, err
}

func (commandRunner *executionRunner) unsupported(ctx context.Context, command policy.Command, output string) (runner.Result, runner.Output, error) {
	err := fmt.Errorf("the execution telemetry runner cannot delegate %s", output)
	result := runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}
	commandRunner.recorder.addCommand(ctx, command, result, 0, err)
	return result, runner.Output{}, err
}
