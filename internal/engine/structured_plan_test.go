package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type plannedOutputRunner struct {
	command policy.Command
	output  runner.Output
}

func (commandRunner *plannedOutputRunner) Run(context.Context, string, policy.Command) error {
	return nil
}

func (commandRunner *plannedOutputRunner) RunWithOutput(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	commandRunner.command = command
	return runner.Result{ExitStatus: 0}, commandRunner.output, nil
}

func TestMergeGatePlannedRunnerCapturesStructuredAnalyzerOutput(t *testing.T) {
	t.Parallel()
	command := policy.Command{Name: "structured", Argv: []string{"tool", "--json"}, Cwd: ".", TimeoutSeconds: 30}
	delegate := &plannedOutputRunner{output: runner.Output{Stdout: []byte(`{"covered":[]}`)}}
	commandRunner := &mergeGatePlannedRunner{
		root: "/repository", delegate: delegate,
		expected: []MergeGateExecutionCommand{{Command: command}},
	}
	result, output, err := commandRunner.RunStructured(t.Context(), "/repository", command)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitStatus != 0 || string(output.Stdout) != `{"covered":[]}` || commandRunner.next != 1 || delegate.command.Name != command.Name {
		t.Fatalf("result = %+v output = %q next = %d command = %+v", result, output.Stdout, commandRunner.next, delegate.command)
	}
}

func TestPlannedPolicyChecksIncludePythonArchitectureGraph(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n", 0o600)
	writeEngineFile(t, root, "src/app.py", "import model\n", 0o600)
	writeEngineFile(t, root, "tools/ruff-version.txt", "0.16.0\n", 0o600)
	repo := repository.Repository{
		Root: root, PolicyRoot: root,
		Config: policy.Config{Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}}, ModuleByName: map[string]int{"application": 0}},
	}
	commands := plannedPolicyCheckCommands(repo, repository.Selection{Files: []string{"src/app.py"}}, "check")
	for _, command := range commands {
		if strings.HasPrefix(command.Name, "ruff-graph-facts-v1-") {
			return
		}
	}
	t.Fatalf("planned commands omitted Python architecture graph: %+v", commands)
}
