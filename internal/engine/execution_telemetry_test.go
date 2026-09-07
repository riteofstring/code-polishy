package engine

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestExecutionTelemetryRecordsPhasesCommandsAndCacheUse(t *testing.T) {
	commandRunner := &telemetryTestRunner{}
	policyEngine := &Engine{Repository: repository.Repository{Root: t.TempDir()}, Runner: commandRunner}
	policyEngine.BeginExecution(time.Now().Add(-10*time.Millisecond), 2*time.Millisecond)
	policyEngine.executionPhase(t.Context(), "quality", func(ctx context.Context) {
		structured := policyEngine.Runner.(runner.StructuredRunner)
		_, _, _ = structured.RunStructured(ctx, policyEngine.Repository.Root, policy.Command{
			Name: "analyzer", Argv: []string{"tool", "check", "selected.py"}, Cwd: ".", TimeoutSeconds: 1,
		})
		policyEngine.Runner.(runner.CommandPhaseRecorder).RecordCommandPhases(ctx, policy.Command{Name: "analyzer"}, []runner.CommandPhase{{
			Name: "analysis", Duration: 2 * time.Millisecond,
		}})
	})
	report := policyEngine.AttachExecution(Report{
		RequestedSelection: &repository.RequestedSelection{Operands: []string{"selected.py"}, Expanded: []string{"selected.py"}},
		AnalysisContext:    []AnalysisContext{{Paths: []string{"selected.py", "dependency.py"}}},
		SourceDependencyGraph: &sourcegraph.Graph{
			Nodes: []sourcegraph.Node{{Path: "selected.py"}, {Path: "dependency.py"}},
			Edges: []sourcegraph.Edge{{Source: "selected.py", Target: "dependency.py"}},
		},
	})
	telemetry := requireExecutionTelemetry(t, report)
	assertExecutionPhases(t, telemetry)
	assertExecutionCommand(t, telemetry)
	assertExecutionScope(t, telemetry)
	assertExecutionCaches(t, telemetry)
}

func requireExecutionTelemetry(t *testing.T, report Report) *ExecutionTelemetry {
	t.Helper()
	if report.Execution == nil {
		t.Fatal("execution telemetry is absent")
	}
	return report.Execution
}

func assertExecutionPhases(t *testing.T, telemetry *ExecutionTelemetry) {
	t.Helper()
	if telemetry.EvaluationDurationMilliseconds < 10 ||
		!slices.ContainsFunc(telemetry.Phases, func(phase ExecutionPhase) bool { return phase.Name == "repository-open" }) ||
		!slices.ContainsFunc(telemetry.Phases, func(phase ExecutionPhase) bool { return phase.Name == "quality" }) {
		t.Fatalf("execution = %+v", telemetry)
	}
}

func assertExecutionCommand(t *testing.T, telemetry *ExecutionTelemetry) {
	t.Helper()
	if len(telemetry.Commands) != 1 {
		t.Fatalf("commands = %+v", telemetry.Commands)
	}
	command := telemetry.Commands[0]
	if command.Phase != "quality" || command.Name != "analyzer" || !slices.Equal(command.Argv, []string{"tool", "check", "selected.py"}) ||
		command.ExitStatus != 7 || command.ResourceWaitMillis != 3 || command.FailureCategory != string(runner.FailureCommandExit) ||
		len(command.Phases) != 1 || command.Phases[0].Name != "analysis" || command.Phases[0].DurationMilliseconds != 2 {
		t.Fatalf("command = %+v", command)
	}
}

func assertExecutionScope(t *testing.T, telemetry *ExecutionTelemetry) {
	t.Helper()
	if telemetry.Scope.RequestedOperands != 1 || telemetry.Scope.SelectedPaths != 1 || telemetry.Scope.ContextPaths != 2 ||
		telemetry.Scope.GraphNodes != 2 || telemetry.Scope.GraphEdges != 1 {
		t.Fatalf("scope = %+v", telemetry.Scope)
	}
}

func assertExecutionCaches(t *testing.T, telemetry *ExecutionTelemetry) {
	t.Helper()
	if len(telemetry.Caches) != 3 || telemetry.Caches[0].Name != "python-project-inventory" ||
		telemetry.Caches[1].Name != "repository-path-facts" || telemetry.Caches[2].Name != "glob-patterns" {
		t.Fatalf("caches = %+v", telemetry.Caches)
	}
}

type telemetryTestRunner struct{}

func (*telemetryTestRunner) Run(context.Context, string, policy.Command) error {
	return errors.New("failed")
}

func (*telemetryTestRunner) RunStructured(context.Context, string, policy.Command) (runner.Result, runner.Output, error) {
	return runner.Result{ExitStatus: 7, ResourceWait: 3 * time.Millisecond, FailureCategory: runner.FailureCommandExit}, runner.Output{}, errors.New("failed")
}
