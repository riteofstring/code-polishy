package engine

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestMergeGateRunsDeadcodeOnlyForTargetsThatBuildAProgram(t *testing.T) {
	root := contentRepository(t, nil)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "content/go.mod", "module example.test/content\n\ngo 1.26\n", 0o600)
	writeEngineFile(t, root, "content/main_linux.go", "package main\n\nfunc main() {\n\thelper()\n}\n", 0o600)
	writeEngineFile(t, root, "content/helper.go", "package main\n\nfunc helper() {}\n", 0o600)
	commitEngineCandidate(t, root, "add linux program")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &goReachabilityGateRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil || HasFindings(report) || report.GateRunPolicy == nil || report.GateRunPolicy.Status != "passed" {
		t.Fatalf("merge gate report = %+v, error = %v", report, err)
	}
	if len(commandRunner.analyzed) != 2 {
		t.Fatalf("analyzed deadcode targets = %v", commandRunner.analyzed)
	}
	for _, name := range commandRunner.analyzed {
		outcome := readGateCommandOutcome(t, root, report.GateRunPolicy.ReportPath, name)
		if outcome.Status != gaterun.Passed || len(outcome.Attempts) != 1 {
			t.Fatalf("analyzed target outcome %s = %+v", name, outcome)
		}
	}
}

type goReachabilityGateRunner struct {
	analyzed []string
}

func (commandRunner *goReachabilityGateRunner) Execute(_ context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	command := invocation.Command
	if len(command.Argv) > 0 && filepath.Base(command.Argv[0]) == "osv-scanner" {
		return runner.Result{}, runner.Output{Stdout: []byte(`{"results":[]}`)}, nil
	}
	if len(command.Argv) == 0 || filepath.Base(command.Argv[0]) != "deadcode" {
		return runner.Result{}, runner.Output{}, nil
	}
	if slices.Contains(command.EnvironmentOverrides, "GOOS=linux") {
		commandRunner.analyzed = append(commandRunner.analyzed, command.Name)
		return runner.Result{}, runner.Output{Stdout: []byte("null")}, nil
	}
	output := runner.Output{Stderr: []byte("deadcode: no packages\n")}
	result := runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}
	return result, output, errors.New(strings.TrimSpace(string(output.Stderr)))
}
