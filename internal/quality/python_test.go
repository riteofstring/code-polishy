package quality

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type pythonQualityRunner struct {
	commands []policy.Command
	outputs  map[string]string
	errors   map[string]error
}

func (qualityRunner *pythonQualityRunner) Run(_ context.Context, _ string, command policy.Command) error {
	if qualityRunner.errors == nil {
		return nil
	}
	return qualityRunner.errors[command.Name]
}

func (qualityRunner *pythonQualityRunner) RunStructured(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	qualityRunner.commands = append(qualityRunner.commands, command)
	if qualityRunner.errors != nil && qualityRunner.errors[command.Name] != nil {
		return runner.Result{}, runner.Output{}, qualityRunner.errors[command.Name]
	}
	output, found := qualityRunner.outputs[command.Name]
	if !found && strings.HasPrefix(command.Name, "policy-vulture-dead-code-") {
		var err error
		output, err = pythonVultureCleanOutput(command)
		if err != nil {
			return runner.Result{}, runner.Output{}, err
		}
	}
	return runner.Result{}, runner.Output{Stdout: []byte(output)}, nil
}

func pythonVultureCleanOutput(command policy.Command) (string, error) {
	request, err := pythonVultureInputRequest(command.Stdin)
	if err != nil {
		return "", err
	}
	covered := make([]string, 0, len(request.Files))
	for _, file := range request.Files {
		covered = append(covered, file.Path)
	}
	resolved := make([]string, 0, len(request.References)+len(request.Backends)+len(request.Attributes))
	for _, reference := range request.References {
		resolved = append(resolved, reference.ID)
	}
	for _, backend := range request.Backends {
		resolved = append(resolved, backend.ID)
	}
	for _, attribute := range request.Attributes {
		resolved = append(resolved, attribute.ID)
	}
	output, err := json.Marshal(map[string]any{
		"protocol": pythonVultureProtocolVersion, "tool_version": pythonVultureVersion, "covered": covered,
		"diagnostics": []pythonVultureDiagnostic{}, "resolved": resolved, "problems": []pythonVultureProblem{}, "error": "", "facts_error": "", "reachability": []pythonfacts.ReachabilityEvidence{},
	})
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func pythonVultureInputRequest(data []byte) (pythonVultureRequest, error) {
	header, payload, found := bytes.Cut(data, []byte{'\n'})
	var program string
	if !found || json.Unmarshal(header, &program) != nil || program != pythonVultureProgram {
		return pythonVultureRequest{}, fmt.Errorf("Vulture input omits its exact policy program")
	}
	var request pythonVultureRequest
	err := json.Unmarshal(payload, &request)
	return request, err
}

func TestPythonQualitySealsRuffBaselineAndKeepsTargetRulesAdditive(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n\n[tool.ruff.lint]\nignore = [\"F401\"]\nselect = [\"ANN\"]\n")
	writeQualityFile(t, repo.Root, "src/app.py", "import unused\n")
	runner := &pythonQualityRunner{outputs: map[string]string{
		"policy-ruff-baseline-coverage-root": "src/app.py\n",
		"policy-ruff-baseline-root":          `[{"code":"B006","filename":"src/app.py","location":{"row":1,"column":1},"message":"mutable default"},{"code":"F401","filename":"src/app.py","location":{"row":1,"column":1},"message":"unused import"}]`,
		"policy-ruff-complexity-root":        `[]`,
		"policy-ruff-target-root":            `[{"code":"ANN201","filename":"src/app.py","location":{"row":1,"column":1},"message":"missing return type"}]`,
		"policy-ty-typecheck-root":           `[]`,
	}}

	findings := PythonQualityFindings(t.Context(), repo, []string{"src/app.py"}, runner)
	pythonQualityAssertRuffFindings(t, findings)
	baseline := pythonQualityRequiredCommand(t, runner.commands, "policy-ruff-baseline-root")
	pythonQualityAssertRuffBaselineCommand(t, baseline)
	target := pythonQualityRequiredCommand(t, runner.commands, "policy-ruff-target-root")
	pythonQualityAssertRuffTargetCommand(t, target)
	vulture := pythonQualityRequiredCommand(t, runner.commands, "policy-vulture-dead-code-root")
	pythonQualityAssertVultureCommand(t, repo, vulture)
	pythonQualityAssertCommandPlan(t, repo, runner.commands)
}

func TestPythonQualitySharesInTreeBackendSourceRootsWithManagedTools(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	repo.Config.Modules[0].Paths = append(repo.Config.Modules[0].Paths, "packages/**")
	writeQualityFile(t, repo.Root, "pyproject.toml", `[build-system]
requires = []
build-backend = "setta_build_backend"
backend-path = ["packages/setta-runtime"]

[project]
name = "setta"
requires-python = ">=3.12"
dependencies = []
`)
	writeQualityFile(t, repo.Root, "packages/setta-runtime/setta_build_backend.py", "def build_wheel():\n    return 'setta.whl'\n")
	writeQualityFile(t, repo.Root, "packages/setta-runtime/src/setta/runtime.py", "value = 1\n")
	writeQualityFile(t, repo.Root, "packages/setta-runtime/tests/test_runtime.py", "from setta import runtime\n")
	selected := []string{
		"packages/setta-runtime/setta_build_backend.py",
		"packages/setta-runtime/src/setta/runtime.py",
		"packages/setta-runtime/tests/test_runtime.py",
	}
	plan := pythonQualityPlanFor(repo, selected)
	if len(plan.findings) != 0 || len(plan.projects) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	commands := plan.projects[0].commands
	ty := pythonQualityRequiredCommand(t, pythonQualityCommandValues(commands), "policy-ty-typecheck-root")
	joinedTy := strings.Join(ty.Argv, "\x00")
	for _, path := range []string{"packages/setta-runtime", "packages/setta-runtime/src"} {
		if !strings.Contains(joinedTy, "--extra-search-path\x00"+path) {
			t.Fatalf("ty command lacks source root %q: %+v", path, ty)
		}
	}
	ruff := pythonQualityRequiredCommand(t, pythonQualityCommandValues(commands), "policy-ruff-baseline-root")
	if !strings.Contains(strings.Join(ruff.Argv, "\x00"), `src = [".", "packages/setta-runtime", "packages/setta-runtime/src"]`) {
		t.Fatalf("Ruff command = %+v", ruff)
	}
	vulture := pythonQualityRequiredCommand(t, pythonQualityCommandValues(commands), "policy-vulture-dead-code-root")
	request, err := pythonVultureInputRequest(vulture.Stdin)
	if err != nil || len(request.Backends) != 1 ||
		request.Backends[0].Module != "setta_build_backend" {
		t.Fatalf("Vulture request = %+v, error = %v", request, err)
	}
}

func TestPythonQualityReportsUnsupportedLayoutOnceAndStopsDependentTools(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	repo.Config.Modules[0].Paths = append(repo.Config.Modules[0].Paths, "packages/**")
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"setta\"\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	writeQualityFile(t, repo.Root, "packages/setta-runtime/src/setta/first.py", "value = 1\n")
	writeQualityFile(t, repo.Root, "packages/setta-runtime/src/setta/second.py", "value = 2\n")
	plan := pythonQualityPlanFor(repo, []string{
		"packages/setta-runtime/src/setta/first.py", "packages/setta-runtime/src/setta/second.py",
	})
	if len(plan.projects) != 0 || len(plan.findings) != 1 || plan.findings[0].Check != "policy.pythonProject" ||
		!strings.Contains(plan.findings[0].Message, "Python project layout is unsupported") {
		t.Fatalf("plan = %+v", plan)
	}
}

func pythonQualityCommandValues(commands []pythonQualityCommand) []policy.Command {
	result := make([]policy.Command, 0, len(commands))
	for _, command := range commands {
		result = append(result, command.command)
	}
	return result
}

func pythonQualityAssertRuffFindings(t *testing.T, findings []policy.Finding) {
	t.Helper()
	if len(findings) != 3 || findings[0].Check != "quality.lint" || findings[1].Check != "quality.lint" || findings[2].Check != "quality.lint" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Subject != "ANN201" || findings[1].Subject != "B006" || findings[2].Subject != "F401" {
		t.Fatalf("findings = %+v", findings)
	}
}

func pythonQualityRequiredCommand(t *testing.T, commands []policy.Command, name string) policy.Command {
	t.Helper()
	command, found := pythonQualityCommandNamed(commands, name)
	if !found {
		t.Fatalf("command %q not found in %+v", name, commands)
	}
	return command
}

func pythonQualityAssertRuffBaselineCommand(t *testing.T, baseline policy.Command) {
	t.Helper()
	if !baseline.SealedEnvironment || !slices.Contains(baseline.Argv, "--isolated") ||
		!slices.Contains(baseline.Argv, "--ignore-noqa") || !slices.Contains(baseline.Argv, pythonRuffBaselineSelection) ||
		!slices.Equal(baseline.Provides, []string{"lint"}) {
		t.Fatalf("baseline command = %+v", baseline)
	}
}

func pythonQualityAssertRuffTargetCommand(t *testing.T, target policy.Command) {
	t.Helper()
	if slices.Contains(target.Argv, "--isolated") || !slices.Contains(target.Argv, "--no-force-exclude") {
		t.Fatalf("target command = %+v", target)
	}
}

func pythonQualityAssertVultureCommand(t *testing.T, repo repository.Repository, vulture policy.Command) {
	t.Helper()
	if !vulture.SealedEnvironment || !slices.Equal(vulture.Provides, []string{"dead-code"}) ||
		!slices.Equal(vulture.Argv, repo.PythonCommand(pythonfacts.ProgramBootstrap)) ||
		strings.Contains(strings.Join(vulture.Argv, "\x00"), ".venv") ||
		strings.ContainsAny(vulture.Argv[4], "\x00\r\n") {
		t.Fatalf("Vulture command = %+v", vulture)
	}
	pythonQualityAssertVultureRequest(t, vulture.Stdin)
}

func pythonQualityAssertVultureRequest(t *testing.T, data []byte) {
	t.Helper()
	request, err := pythonVultureInputRequest(data)
	if err != nil || request.Protocol != pythonVultureProtocolVersion ||
		request.ToolVersion != pythonVultureVersion || !slices.EqualFunc(request.Files, []pythonVultureFile{{Path: "src/app.py", Module: "app"}}, func(left, right pythonVultureFile) bool {
		return left == right
	}) || len(request.Backends) != 0 || len(request.Attributes) != 0 {
		t.Fatalf("Vulture request = %+v, error = %v", request, err)
	}
}

func pythonQualityAssertCommandPlan(t *testing.T, repo repository.Repository, commands []policy.Command) {
	t.Helper()
	if planned := pythonQualityCommands(repo, []string{"src/app.py"}); !slices.EqualFunc(planned, commands, samePythonQualityCommand) {
		t.Fatalf("planned commands = %+v, run commands = %+v", planned, commands)
	}
	ruffCommands := 0
	for _, command := range commands {
		if !strings.HasPrefix(command.Name, "policy-ruff-") {
			continue
		}
		ruffCommands++
		arguments := strings.Join(command.Argv, "\x00")
		for _, expected := range []string{
			"--target-version\x00py312",
			"--config\x00line-length = 88",
			"--config\x00lint.pycodestyle.max-line-length = 88",
			`--config` + "\x00" + `src = [".", "src"]`,
		} {
			if !strings.Contains(arguments, expected) {
				t.Fatalf("Ruff command lacks %q: %+v", expected, command)
			}
		}
	}
	if ruffCommands != 4 {
		t.Fatalf("Ruff commands = %d in %+v", ruffCommands, commands)
	}
}

func TestPythonQualityEmitsOneExactTyFindingPerDiagnostic(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\ndependencies = [\"example==1.0\"]\n")
	writeQualityFile(t, repo.Root, "src/app.py", "first = 1\nsecond = 2\n")
	writePythonVenv(t, repo.Root, ".venv")
	absoluteSource := filepath.ToSlash(filepath.Join(repo.Root, "src", "app.py"))
	runner := &pythonQualityRunner{outputs: map[string]string{
		"policy-ruff-baseline-coverage-root": "src/app.py\n",
		"policy-ruff-baseline-root":          `[]`,
		"policy-ruff-complexity-root":        `[]`,
		"policy-ruff-target-root":            `[]`,
		"policy-ty-typecheck-root": `[
  {"check_name":"invalid-assignment","description":"invalid-assignment: first is wrong","location":{"path":"` + absoluteSource + `","positions":{"begin":{"line":1,"column":4}}}},
  {"check_name":"invalid-assignment","description":"invalid-assignment: second is wrong","location":{"path":"` + absoluteSource + `","positions":{"begin":{"line":2,"column":5}}}}
]`,
	}}

	findings := PythonQualityFindings(t.Context(), repo, []string{"src/app.py"}, runner)
	typeFindings := pythonQualityFindingsForCheck(findings, "quality.typecheck")
	if len(typeFindings) != 2 || typeFindings[0].Subject == typeFindings[1].Subject ||
		!strings.HasPrefix(typeFindings[0].Subject, "invalid-assignment:") {
		t.Fatalf("type findings = %+v", typeFindings)
	}
	kept, suppressed := policy.ApplyExceptions(typeFindings, []policy.Exception{{
		ID: "first", Check: "quality.typecheck", Path: "src/app.py", Subject: typeFindings[0].Subject,
		Reason: "tracked", Owner: "quality", Expires: policy.Date{Time: time.Now().AddDate(0, 0, 1)},
	}}, time.Now())
	if len(kept) != 1 || len(suppressed) != 1 || kept[0].Subject == typeFindings[0].Subject {
		t.Fatalf("kept = %+v, suppressed = %+v", kept, suppressed)
	}
	typecheck, found := pythonQualityCommandNamed(runner.commands, "policy-ty-typecheck-root")
	if !found || !typecheck.SealedEnvironment || len(typecheck.Environment) != 0 ||
		!slices.Contains(typecheck.Argv, "--python") || !slices.Contains(typecheck.Argv, ".venv") {
		t.Fatalf("ty command = %+v", typecheck)
	}
}

func TestParsePythonTyDiagnosticsAcceptsGitLabPositionsOutput(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "src/app.py", "value: int = \"wrong\"\n")
	inventory := repo.PythonProjectInventory([]string{"pyproject.toml", "src/app.py"})
	if len(inventory.Projects) != 1 {
		t.Fatalf("projects = %+v", inventory.Projects)
	}
	diagnostics, err := parsePythonTyDiagnostics(repo, inventory.Projects[0], []string{"src/app.py"}, []byte(`[
  {
    "check_name": "invalid-assignment",
    "description": "invalid-assignment: Object of type `+"`Literal[\\\"wrong\\\"]`"+` is not assignable to `+"`int`"+`",
    "severity": "major",
    "fingerprint": "f9fbeb0b18706d39",
    "location": {
      "path": "src/app.py",
      "positions": {
        "begin": {"line": 1, "column": 14},
        "end": {"line": 1, "column": 21}
      }
    }
  }
]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Rule != "invalid-assignment" || diagnostics[0].Path != "src/app.py" ||
		diagnostics[0].Line != 1 || diagnostics[0].Column != 14 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestPythonQualityReportsPathOnlyTyDiagnosticWithExactIdentity(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "src/app.py", "value = 1\n")
	runner := &pythonQualityRunner{outputs: map[string]string{
		"policy-ruff-baseline-coverage-root": "src/app.py\n",
		"policy-ruff-baseline-root":          `[]`,
		"policy-ruff-complexity-root":        `[]`,
		"policy-ruff-target-root":            `[]`,
		"policy-ty-typecheck-root":           `[{"check_name":"unsupported-operand","description":"operation is unsupported","location":{"path":"src/app.py"}}]`,
	}}

	findings := pythonQualityFindingsForCheck(PythonQualityFindings(t.Context(), repo, []string{"src/app.py"}, runner), "quality.typecheck")
	if len(findings) != 1 || findings[0].Line != 0 || findings[0].Column != 0 || findings[0].Message != "operation is unsupported" ||
		findings[0].Subject != pythonTySubject(pythonTyDiagnostic{Rule: "unsupported-operand", Path: "src/app.py", Message: "operation is unsupported"}) {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestParsePythonTyDiagnosticsRejectsColumnWithoutLine(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "src/app.py", "value = 1\n")
	inventory := repo.PythonProjectInventory([]string{"pyproject.toml", "src/app.py"})
	if len(inventory.Projects) != 1 {
		t.Fatalf("projects = %+v", inventory.Projects)
	}
	_, err := parsePythonTyDiagnostics(repo, inventory.Projects[0], []string{"src/app.py"}, []byte(`[
  {"check_name":"unsupported-operand","description":"operation is unsupported","location":{"path":"src/app.py","positions":{"begin":{"column":1}}}}
]`))
	if err == nil || !strings.Contains(err.Error(), "invalid location") {
		t.Fatalf("err = %v", err)
	}
}

func TestParsePythonRuffDiagnosticsAcceptsPhysicalContainedPath(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	if physicalTemporaryDirectory, err := filepath.EvalSymlinks("/tmp"); err == nil && filepath.Clean(physicalTemporaryDirectory) != "/tmp" {
		root, err := os.MkdirTemp("/tmp", "code-polishy-python-quality-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
		repo.Root = root
	}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "src/app.py", "import unused\n")
	inventory := repo.PythonProjectInventory([]string{"pyproject.toml", "src/app.py"})
	if len(inventory.Projects) != 1 {
		t.Fatalf("projects = %+v", inventory.Projects)
	}
	physicalPath, err := filepath.EvalSymlinks(filepath.Join(repo.Root, "src", "app.py"))
	if err != nil {
		t.Fatal(err)
	}
	output := []byte(`[{"code":"F401","filename":` + strconv.Quote(filepath.ToSlash(physicalPath)) + `,"location":{"row":1,"column":8},"message":"unused import"}]`)
	diagnostics, err := parsePythonRuffDiagnostics(repo, inventory.Projects[0], []string{"src/app.py"}, output)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || diagnostics[0].Path != "src/app.py" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestPythonQualityReportsOneMissingEnvironmentFindingWithoutTyCascade(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\ndependencies = [\"example==1.0\"]\n")
	writeQualityFile(t, repo.Root, "src/app.py", "value = 1\n")
	runner := &pythonQualityRunner{outputs: map[string]string{
		"policy-ruff-baseline-coverage-root": "src/app.py\n",
		"policy-ruff-baseline-root":          `[]`,
		"policy-ruff-complexity-root":        `[]`,
		"policy-ruff-target-root":            `[]`,
	}}

	findings := PythonQualityFindings(t.Context(), repo, []string{"src/app.py"}, runner)
	if len(findings) != 1 || findings[0].Check != "quality.typecheckCoverage" || findings[0].Path != "pyproject.toml" ||
		!strings.Contains(findings[0].Message, ".venv") {
		t.Fatalf("findings = %+v", findings)
	}
	if _, found := pythonQualityCommandNamed(runner.commands, "policy-ty-typecheck-root"); found {
		t.Fatalf("ty ran without an explicit environment: %+v", runner.commands)
	}
}

func TestPythonQualityRejectsMalformedAndEscapingStructuredOutput(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		output  string
		check   string
		command string
	}{
		{name: "omitted ruff", output: "", check: "quality.lintCoverage", command: "policy-ruff-baseline-coverage-root"},
		{name: "malformed ruff", output: "not JSON", check: "quality.lintCoverage", command: "policy-ruff-baseline-root"},
		{name: "unexpected ruff family", output: `[{"code":"BLE001","filename":"src/app.py","location":{"row":1,"column":1},"message":"blind exception"}]`, check: "quality.lintCoverage", command: "policy-ruff-baseline-root"},
		{name: "escaping ty", output: `[{"check_name":"invalid-assignment","description":"wrong","location":{"path":"../../outside.py","lines":{"begin":1}}}]`, check: "quality.typecheckCoverage", command: "policy-ty-typecheck-root"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			repo := pythonQualityRepository(t)
			manifest := "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n"
			if test.command == "policy-ty-typecheck-root" {
				manifest += "dependencies = [\"example==1.0\"]\n"
				writePythonVenv(t, repo.Root, ".venv")
			}
			writeQualityFile(t, repo.Root, "pyproject.toml", manifest)
			writeQualityFile(t, repo.Root, "src/app.py", "value = 1\n")
			runner := &pythonQualityRunner{outputs: map[string]string{
				"policy-ruff-baseline-coverage-root": "src/app.py\n",
				"policy-ruff-baseline-root":          `[]`,
				"policy-ruff-complexity-root":        `[]`,
				"policy-ruff-target-root":            `[]`,
				"policy-ty-typecheck-root":           `[]`,
			}}
			runner.outputs[test.command] = test.output
			findings := PythonQualityFindings(t.Context(), repo, []string{"src/app.py"}, runner)
			if !slices.ContainsFunc(findings, func(finding policy.Finding) bool { return finding.Check == test.check }) {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestPythonQualityUsesNestedProjectAndPython312Inventory(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"root\"\nversion = \"0\"\nrequires-python = \">=3.12\"\n")
	writeQualityFile(t, repo.Root, "src/root.py", "value = 1\n")
	writeQualityFile(t, repo.Root, "apps/worker/pyproject.toml", "[project]\nname = \"worker\"\nversion = \"0\"\nrequires-python = \">=3.12\"\ndependencies = [\"example==1.0\"]\n")
	writeQualityFile(t, repo.Root, "apps/worker/src/worker.py", "value = 1\n")
	writePythonVenv(t, repo.Root, "apps/worker/.venv")

	commands := pythonQualityCommands(repo, []string{"src/root.py", "apps/worker/src/worker.py"})
	if len(commands) != 12 {
		t.Fatalf("commands = %+v", commands)
	}
	workerTy, found := pythonQualityCommandNamed(commands, "policy-ty-typecheck-"+pythonQualityProjectName("apps/worker"))
	if !found || workerTy.Cwd != "apps/worker" || !slices.Contains(workerTy.Argv, ".venv") ||
		!slices.Contains(workerTy.Argv, "src/worker.py") {
		t.Fatalf("worker ty command = %+v", workerTy)
	}
	rootTy, found := pythonQualityCommandNamed(commands, "policy-ty-typecheck-root")
	if !found || rootTy.Cwd != "." || !slices.Contains(rootTy.Argv, "src/root.py") || slices.Contains(rootTy.Argv, "--python") {
		t.Fatalf("root ty command = %+v", rootTy)
	}
}

func TestPythonVultureCoversTheWholeSelectedProjectAndFailsClosed(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "src/first.py", "def first():\n    return 1\n")
	writeQualityFile(t, repo.Root, "src/second.py", "def second():\n    return 2\n")
	project := pythonVultureProject(t, repo)
	command, err := pythonVultureCommand(repo, project)
	if err != nil {
		t.Fatal(err)
	}
	request, err := pythonVultureInputRequest(command.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{request.Files[0].Path, request.Files[1].Path}; !slices.Equal(got, []string{"src/first.py", "src/second.py"}) {
		t.Fatalf("Vulture files = %v", got)
	}
	findings := pythonVultureFindings(repo, project, pythonVultureOutput(t, []string{"src/first.py"}, nil, nil, nil, ""))
	coverage := pythonQualityFindingsForCheck(findings, "quality.deadCodeCoverage")
	if len(coverage) != 2 || coverage[0].Path != "src/first.py" || coverage[1].Path != "src/second.py" {
		t.Fatalf("coverage findings = %+v", findings)
	}
}

func TestPythonVultureDynamicReferenceProblemsArePolicyFindings(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{
		Kind: "target", Project: "pyproject.toml", Target: &policy.PythonDynamicTarget{Module: "app", Symbol: "handler"},
	}}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "app.py", "def handler():\n    return 1\n")
	project := pythonVultureProject(t, repo)
	references, _ := pythonVultureReferences(repo, project)
	if len(references) != 1 {
		t.Fatalf("references = %+v", references)
	}
	findings := pythonVultureFindings(repo, project, pythonVultureOutput(t, project.Files, nil, nil, []pythonVultureProblem{{
		ID: references[0].ID, Message: "symbol is stale or ambiguous",
	}}, ""))
	if len(findings) != 1 || findings[0].Check != "policy.pythonReachability" || findings[0].Path != policy.ConfigFilename ||
		findings[0].Subject != references[0].ID {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonVultureCommandFailureCoversFullProjectOnce(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = ""
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "src/first.py", "value = 1\n")
	writeQualityFile(t, repo.Root, "src/second.py", "value = 2\n")
	plan := pythonQualityPlanFor(repo, []string{"src/first.py"})
	coverage := pythonQualityFindingsForCheck(plan.findings, "quality.deadCodeCoverage")
	if len(coverage) != 2 || coverage[0].Path != "src/first.py" || coverage[1].Path != "src/second.py" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPythonVultureFindingsUseExactHashedSubjects(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeQualityFile(t, repo.Root, "src/left.py", "def handler():\n    return 1\n")
	writeQualityFile(t, repo.Root, "src/right.py", "def handler():\n    return 2\n")
	project := pythonVultureProject(t, repo)
	findings := pythonVultureFindings(repo, project, pythonVultureOutput(t, project.Files, []pythonVultureDiagnostic{
		{Path: "src/left.py", Line: 1, End: 2, Name: "handler", Kind: "function", Confidence: 60, Message: "unused function 'handler'"},
		{Path: "src/right.py", Line: 1, End: 2, Name: "handler", Kind: "function", Confidence: 60, Message: "unused function 'handler'"},
	}, nil, nil, ""))
	if len(findings) != 2 || findings[0].Subject == findings[1].Subject {
		t.Fatalf("findings = %+v", findings)
	}
	kept, suppressed := policy.ApplyExceptions(findings, []policy.Exception{{
		ID: "left-handler", Check: "quality.deadCode", Path: "src/left.py", Subject: findings[0].Subject,
		Reason: "tracked", Owner: "quality", Expires: policy.Date{Time: time.Now().AddDate(0, 0, 1)},
	}}, time.Now())
	if len(kept) != 1 || len(suppressed) != 1 || kept[0].Path != "src/right.py" {
		t.Fatalf("kept = %+v, suppressed = %+v", kept, suppressed)
	}
}

func TestPythonVultureAdapterResolvesExactSameNamedReferencesWhenInstalled(t *testing.T) {
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	if !pythonVultureRuntimeInstalled(t, repo) {
		t.Skip("policy CPython with Vulture is not installed")
	}
	writeQualityFile(t, repo.Root, "pyproject.toml", "project = { name = \"example\", version = \"0\", requires-python = \"==3.12.*\", scripts = { example = \"app:handler\" } }\n")
	writeQualityFile(t, repo.Root, "src/app/__init__.py", "from .left import handler\n")
	writeQualityFile(t, repo.Root, "src/app/left.py", "def handler(): # noqa\n    return 1\n\ndef still_dead(): # noqa\n    return 3\n")
	writeQualityFile(t, repo.Root, "src/app/right.py", "def handler():\n    return 2\n")
	project := pythonVultureProject(t, repo)
	command, err := pythonVultureCommand(repo, project)
	if err != nil {
		t.Fatal(err)
	}
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, command)
	if err != nil {
		t.Fatal(err)
	}
	findings := pythonVultureFindings(repo, project, output.Stdout)
	if slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.deadCodeCoverage" || finding.Check == "policy.pythonDynamicReference"
	}) {
		t.Fatalf("findings = %+v", findings)
	}
	left := slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.deadCode" && finding.Path == "src/app/left.py" && strings.Contains(finding.Message, "handler")
	})
	reexport := slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.deadCode" && finding.Path == "src/app/__init__.py" && strings.Contains(finding.Message, "handler")
	})
	noqa := slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.deadCode" && finding.Path == "src/app/left.py" && strings.Contains(finding.Message, "still_dead")
	})
	right := slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.deadCode" && finding.Path == "src/app/right.py" && strings.Contains(finding.Message, "handler")
	})
	if left || reexport || !noqa || !right {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonVultureAdapterUsesVersionMatchedDynamicContractsWhenInstalled(t *testing.T) {
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	if !pythonVultureRuntimeInstalled(t, repo) {
		t.Skip("policy CPython with Vulture is not installed")
	}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	writeQualityFile(t, repo.Root, "src/contracts.py", `import ast
import ctypes
import html.parser
import unittest
import urllib.request
import zipfile
from unittest import mock

class ExampleCase(unittest.TestCase):
    def setUp(self):
        self.value = 1

class ExampleVisitor(ast.NodeVisitor):
    def visit_FunctionDef(self, node):
        return node
    def visit_Match(self, node):
        return node

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *arguments, **keywords):
        return None

class MarkupParser(html.parser.HTMLParser):
    def handle_starttag(self, tag, attributes):
        return None

class Context:
    def __exit__(self, exc_type, exc_value, traceback):
        return None

def zip_metadata():
    info = zipfile.ZipInfo("entry")
    info.compress_type = zipfile.ZIP_DEFLATED
    return info

def chained(error):
    problem = RuntimeError()
    problem.__cause__ = error
    return problem

library = ctypes.CDLL(None)
library.printf.argtypes = []
library.printf.restype = ctypes.c_int
patched = mock.Mock()
patched.side_effect = RuntimeError
`)
	project := pythonVultureProject(t, repo)
	command, err := pythonVultureCommand(repo, project)
	if err != nil {
		t.Fatal(err)
	}
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, command)
	if err != nil {
		t.Fatal(err)
	}
	response, err := parsePythonVultureResponse(output.Stdout)
	if err != nil || response.Error != "" {
		t.Fatalf("response = %+v, error = %v", response, err)
	}
	for _, diagnostic := range response.Diagnostics {
		if slices.Contains([]string{
			"setUp", "visit_FunctionDef", "visit_Match", "redirect_request", "handle_starttag", "exc_type", "exc_value", "traceback",
			"compress_type", "__cause__", "argtypes", "restype", "side_effect",
		}, diagnostic.Name) {
			t.Fatalf("standard dynamic contract reported dead: %+v", diagnostic)
		}
	}
}

func TestPythonVultureAdapterInfersExactPydanticReachabilityWhenInstalled(t *testing.T) {
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	if !pythonVultureRuntimeInstalled(t, repo) {
		t.Skip("policy CPython with Vulture is not installed")
	}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	writeQualityFile(t, repo.Root, "src/models/__init__.py", "from .base import CoreModel as ExportedModel\n")
	writeQualityFile(t, repo.Root, "src/models/base.py", `import pydantic as pd
from pydantic import computed_field as derived_field
from pydantic import field_validator as validate_field
from pydantic import model_serializer, model_validator
from typing import ClassVar

class CoreModel(pd.BaseModel):
    model_name: str
    model_count: int = pd.Field(default=0)
    _private_value: str = pd.PrivateAttr(default="")
    model_config = pd.ConfigDict(extra="forbid")
    class_constant: ClassVar[int] = 1

    @validate_field("model_name")
    @classmethod
    def normalize_name(cls, value: str) -> str:
        return value

    @model_validator(mode="after")
    def populate_count(self):
        return self

    @pd.field_serializer("model_name")
    def serialize_name(self, value: str) -> str:
        return value

    @model_serializer
    def serialize_model(self):
        return {"model_name": self.model_name}

    @derived_field
    @property
    def display_name(self) -> str:
        return self.model_name

    def unused_model_method(self) -> None:
        return None
`)
	writeQualityFile(t, repo.Root, "src/models/child.py", `from models import ExportedModel
from pydantic import field_validator as validate

class ChildModel(ExportedModel):
    inherited_value: int

    @validate("inherited_value")
    @classmethod
    def validate_inherited(cls, value: int) -> int:
        return value
`)
	writeQualityFile(t, repo.Root, "src/settings.py", `from pydantic_settings import BaseSettings as SettingsBase

class Settings(SettingsBase):
    api_key: str
`)
	writeQualityFile(t, repo.Root, "src/legacy.py", `from pydantic.v1 import BaseModel
from pydantic.v1 import root_validator as validate_model_v1
from pydantic.v1 import validator as validate_field_v1

class LegacyModel(BaseModel):
    legacy_value: int

    @validate_field_v1("legacy_value")
    def validate_legacy_value(cls, value: int) -> int:
        return value

    @validate_model_v1
    def validate_legacy_model(cls, values):
        return values
`)
	writeQualityFile(t, repo.Root, "src/lookalike.py", `class BaseModel:
    pass

class Lookalike(BaseModel):
    fake_field: str

    def unused_lookalike_method(self) -> None:
        return None
`)
	project := pythonVultureProject(t, repo)
	command, err := pythonVultureCommand(repo, project)
	if err != nil {
		t.Fatal(err)
	}
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, command)
	if err != nil {
		t.Fatal(err)
	}
	response, err := parsePythonVultureResponse(output.Stdout)
	if err != nil || response.Error != "" {
		t.Fatalf("response = %+v, error = %v", response, err)
	}
	live := []string{
		"model_name", "model_count", "_private_value", "model_config", "normalize_name", "populate_count",
		"serialize_name", "serialize_model", "display_name", "inherited_value", "validate_inherited", "api_key",
		"legacy_value", "validate_legacy_value", "validate_legacy_model",
	}
	for _, diagnostic := range response.Diagnostics {
		if slices.Contains(live, diagnostic.Name) {
			t.Fatalf("Pydantic declaration reported dead: %+v", diagnostic)
		}
	}
	for _, dead := range []string{"class_constant", "unused_model_method", "fake_field", "unused_lookalike_method"} {
		if !slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool { return diagnostic.Name == dead }) {
			t.Fatalf("unrelated declaration %q was hidden: %+v", dead, response.Diagnostics)
		}
	}
}

func TestPythonVultureAdapterKeepsReportedPydanticModelShapesWhenInstalled(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		live  string
	}{
		{name: "direct BaseModel", live: "direct_field", files: map[string]string{
			"src/reported.py": "from pydantic import BaseModel\n\nclass DirectModel(BaseModel):\n    direct_field: str\n",
		}},
		{name: "inherited BaseModel reexport", live: "inherited_field", files: map[string]string{
			"src/models/base.py":     "from pydantic import BaseModel\n\nclass ProjectModel(BaseModel):\n    pass\n",
			"src/models/__init__.py": "from .base import ProjectModel as ModelBase\n",
			"src/reported.py":        "from models import ModelBase\n\nclass InheritedModel(ModelBase):\n    inherited_field: int\n",
		}},
		{name: "multiline annotated field", live: "annotated_field", files: map[string]string{
			"src/reported.py": "from typing import Annotated\nfrom pydantic import BaseModel, Field\n\nclass AnnotatedModel(BaseModel):\n    annotated_field: Annotated[\n        str,\n        Field(min_length=1),\n    ]\n",
		}},
		{name: "multiline model config", live: "model_config", files: map[string]string{
			"src/reported.py": "from pydantic import BaseModel, ConfigDict\n\nclass ConfiguredModel(BaseModel):\n    model_config = ConfigDict(\n        extra=\"forbid\",\n    )\n",
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repo := pythonQualityRepository(t)
			repo.PolicyRoot = pythonVulturePolicyRoot(t)
			if !pythonVultureRuntimeInstalled(t, repo) {
				t.Skip("policy CPython with Vulture is not installed")
			}
			writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
			for path, source := range test.files {
				writeQualityFile(t, repo.Root, path, source)
			}
			project := pythonVultureProject(t, repo)
			command, err := pythonVultureCommand(repo, project)
			if err != nil {
				t.Fatal(err)
			}
			_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, command)
			if err != nil {
				t.Fatal(err)
			}
			response, err := parsePythonVultureResponse(output.Stdout)
			if err != nil || response.Error != "" {
				t.Fatalf("response = %+v, error = %v", response, err)
			}
			if slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool { return diagnostic.Name == test.live }) {
				t.Fatalf("reported Pydantic declaration %q was dead: %+v", test.live, response.Diagnostics)
			}
		})
	}
}

func TestPythonVultureAdapterInfersInTreeBuildBackendHooksWhenInstalled(t *testing.T) {
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	if !pythonVultureRuntimeInstalled(t, repo) {
		t.Skip("policy CPython with Vulture is not installed")
	}
	writeQualityFile(t, repo.Root, "pyproject.toml", `[build-system]
requires = []
build-backend = "build_backend"
backend-path = ["backend"]

[project]
name = "example"
requires-python = "==3.12.*"
dependencies = []
`)
	writeQualityFile(t, repo.Root, "backend/build_backend.py", `def build_wheel(wheel_directory, config_settings=None, metadata_directory=None):
    return "example.whl"

def build_sdist(sdist_directory, config_settings=None):
    return "example.tar.gz"

def unused_helper():
    return 1
`)
	project := pythonVultureProject(t, repo)
	command, err := pythonVultureCommand(repo, project)
	if err != nil {
		t.Fatal(err)
	}
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, command)
	if err != nil {
		t.Fatal(err)
	}
	findings := pythonVultureFindings(repo, project, output.Stdout)
	for _, hook := range []string{"build_wheel", "build_sdist"} {
		if slices.ContainsFunc(findings, func(finding policy.Finding) bool { return strings.Contains(finding.Message, hook) }) {
			t.Fatalf("build hook %s reported dead: %+v", hook, findings)
		}
	}
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Check == "quality.deadCode" && strings.Contains(finding.Message, "unused_helper")
	}) {
		t.Fatalf("unused helper was not reported: %+v", findings)
	}
}

func pythonVultureProject(t *testing.T, repo repository.Repository) repository.PythonProject {
	t.Helper()
	files, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	inventory := repo.PythonProjectInventory(files)
	if len(inventory.Projects) != 1 || len(inventory.Problems) != 0 {
		t.Fatalf("inventory = %+v", inventory)
	}
	return inventory.Projects[0]
}

func pythonVultureOutput(t *testing.T, covered []string, diagnostics []pythonVultureDiagnostic, resolved []string, problems []pythonVultureProblem, analysisError string) []byte {
	t.Helper()
	if covered == nil {
		covered = []string{}
	}
	if diagnostics == nil {
		diagnostics = []pythonVultureDiagnostic{}
	}
	if resolved == nil {
		resolved = []string{}
	}
	if problems == nil {
		problems = []pythonVultureProblem{}
	}
	output, err := json.Marshal(map[string]any{
		"protocol": pythonVultureProtocolVersion, "tool_version": pythonVultureVersion, "covered": covered,
		"diagnostics": diagnostics, "resolved": resolved, "problems": problems, "error": analysisError, "facts_error": "", "reachability": []pythonfacts.ReachabilityEvidence{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func pythonVulturePolicyRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if info, statErr := os.Stat(filepath.Join(directory, "go.mod")); statErr == nil && info.Mode().IsRegular() {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("policy root is unavailable")
		}
		directory = parent
	}
}

func pythonVultureRuntimeInstalled(t *testing.T, repo repository.Repository) bool {
	t.Helper()
	info, err := os.Stat(repo.PythonTool())
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return false
	}
	command := policy.Command{
		Name: "probe-vulture", Argv: repo.PythonCommand("import importlib.metadata;print(importlib.metadata.version('vulture'))"),
		Cwd: ".", TimeoutSeconds: 30, SealedEnvironment: true,
	}
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, command)
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(output.Stdout)) != pythonVultureVersion {
		t.Fatalf("policy Vulture version = %q", output.Stdout)
	}
	return true
}

func TestPythonIsABuiltInArchitectureCapability(t *testing.T) {
	t.Parallel()
	repo := qualityRepository(t)
	repo.Config.Modules = []policy.Module{{Name: "application", Paths: []string{"src/**"}}}
	repo.Config.ModuleByName = map[string]int{"application": 0}
	repo.Config.Checks = []policy.Command{{
		Name: "target-python-architecture", Provides: []string{"architecture"}, Paths: []string{"src/**"}, RunOn: []string{"check", "gate"},
	}}
	writeQualityFile(t, repo.Root, "src/app.py", "value = 1\n")
	findings := CoverageFindings(repo, []string{"src/app.py"})
	if len(findings) != 1 || findings[0].Check != "policy.builtInCapability" || findings[0].Subject != "target-python-architecture:architecture" {
		t.Fatalf("findings = %+v", findings)
	}
}

func pythonQualityRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := qualityRepository(t)
	repo.PolicyRoot = t.TempDir()
	repo.Config.Modules = []policy.Module{{Name: "application", Paths: []string{"src/**", "apps/**"}}}
	repo.Config.ModuleByName = map[string]int{"application": 0}
	return repo
}

func writePythonVenv(t *testing.T, root, path string) {
	t.Helper()
	writeQualityFile(t, root, filepath.ToSlash(filepath.Join(path, "pyvenv.cfg")), "home = /python\n")
	writeQualityFile(t, root, filepath.ToSlash(filepath.Join(path, "bin", "python")), "#!/bin/sh\n")
	interpreter := filepath.Join(root, filepath.FromSlash(path), "bin", "python")
	if err := os.Chmod(interpreter, 0o700); err != nil {
		t.Fatal(err)
	}
}

func pythonQualityCommandNamed(commands []policy.Command, name string) (policy.Command, bool) {
	for _, command := range commands {
		if command.Name == name {
			return command, true
		}
	}
	return policy.Command{}, false
}

func pythonQualityFindingsForCheck(findings []policy.Finding, check string) []policy.Finding {
	result := []policy.Finding{}
	for _, finding := range findings {
		if finding.Check == check {
			result = append(result, finding)
		}
	}
	return result
}

func samePythonQualityCommand(left, right policy.Command) bool {
	return reflect.DeepEqual(left, right)
}

var _ runner.StructuredRunner = (*pythonQualityRunner)(nil)
