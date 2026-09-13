package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

type ownershipAnalysisGateRunner struct {
	commands []policy.Command
}

func (commandRunner *ownershipAnalysisGateRunner) Run(_ context.Context, _ string, command policy.Command) error {
	commandRunner.commands = append(commandRunner.commands, command)
	return nil
}

func (commandRunner *ownershipAnalysisGateRunner) RunWithOutput(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	commandRunner.commands = append(commandRunner.commands, command)
	graph := map[string][]string{}
	for _, path := range command.Paths {
		graph[path] = []string{}
	}
	data, err := json.Marshal(graph)
	return runner.Result{ExitStatus: 0}, runner.Output{Stdout: data}, err
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
		if strings.HasPrefix(command.Name, "ruff-graph-facts-v2-") {
			return
		}
	}
	t.Fatalf("planned commands omitted Python architecture graph: %+v", commands)
}

func TestMergeGatePlansDoctorOwnershipAnalysis(t *testing.T) {
	root := contentRepository(t, nil)
	configPath := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(data), `"tests": {"ownership":[]`, `"tests": {"paths":["tests/conftest.py"],"ownership":[]`, 1)
	if configured == string(data) {
		t.Fatal("content fixture test configuration was not updated")
	}
	writeEngineFile(t, root, policy.ConfigFilename, configured, 0o600)
	writeEngineFile(t, root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n", 0o600)
	writeEngineFile(t, root, "tests/conftest.py", "import example\n", 0o600)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &ownershipAnalysisGateRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range report.Findings {
		if finding.Check == "policy.testOwnership" && finding.Path == "tests/conftest.py" && finding.Subject == "unmapped" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("merge gate omitted the ownership finding: %+v", report.Findings)
	}
	if len(commandRunner.commands) != 2 {
		t.Fatalf("ownership analysis commands = %+v", commandRunner.commands)
	}
	for _, command := range commandRunner.commands {
		if !strings.HasPrefix(command.Name, "ruff-graph-facts-v2-") {
			t.Fatalf("unexpected ownership analysis command = %+v", command)
		}
	}
}

func TestMergeGatePlannedRunnerNamesCommandIdentityMismatch(t *testing.T) {
	expected := policy.Command{Name: "expected", Argv: []string{"tool", "expected.py"}, Cwd: ".", Paths: []string{"expected.py"}, TimeoutSeconds: 30}
	actual := policy.Command{Name: "actual", Argv: []string{"tool", "actual.py"}, Cwd: ".", Paths: []string{"actual.py"}, TimeoutSeconds: 30}
	commandRunner := &mergeGatePlannedRunner{
		root: "/repository", delegate: &plannedOutputRunner{},
		expected: []MergeGateExecutionCommand{{Command: expected}},
	}
	_, _, err := commandRunner.RunWithOutput(t.Context(), "/repository", actual)
	if err == nil || !strings.Contains(err.Error(), `command "actual"`) || !strings.Contains(err.Error(), `expected "expected"`) ||
		!strings.Contains(err.Error(), "metadata") || !strings.Contains(err.Error(), "arguments") || !strings.Contains(err.Error(), "selection") {
		t.Fatalf("mismatch error = %v", err)
	}
}
