package quality

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
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
	request := pythonVultureRequest{}
	if err := json.Unmarshal(command.Stdin, &request); err != nil {
		return "", err
	}
	covered := make([]string, 0, len(request.Files))
	for _, file := range request.Files {
		covered = append(covered, file.Path)
	}
	resolved := make([]string, 0, len(request.References))
	for _, reference := range request.References {
		resolved = append(resolved, reference.ID)
	}
	output, err := json.Marshal(map[string]any{
		"protocol": pythonVultureProtocolVersion, "tool_version": pythonVultureVersion, "covered": covered,
		"diagnostics": []pythonVultureDiagnostic{}, "resolved": resolved, "problems": []pythonVultureProblem{}, "error": "",
	})
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func TestPythonQualitySealsRuffBaselineAndKeepsTargetRulesAdditive(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n\n[tool.ruff.lint]\nignore = [\"F401\"]\nselect = [\"I\"]\n")
	writeQualityFile(t, repo.Root, "src/app.py", "import unused\n")
	runner := &pythonQualityRunner{outputs: map[string]string{
		"policy-ruff-baseline-coverage-root": "src/app.py\n",
		"policy-ruff-baseline-root":          `[{"code":"F401","filename":"src/app.py","location":{"row":1,"column":1},"message":"unused import"}]`,
		"policy-ruff-complexity-root":        `[]`,
		"policy-ruff-target-root":            `[{"code":"F401","filename":"src/app.py","location":{"row":1,"column":1},"message":"unused import"},{"code":"I001","filename":"src/app.py","location":{"row":1,"column":1},"message":"import block is un-sorted"}]`,
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

func pythonQualityAssertRuffFindings(t *testing.T, findings []policy.Finding) {
	t.Helper()
	if len(findings) != 2 || findings[0].Check != "quality.lint" || findings[1].Check != "quality.lint" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Subject != "F401" || findings[1].Subject != "I001" {
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
		!slices.Contains(baseline.Argv, "--ignore-noqa") || !slices.Contains(baseline.Argv, "E4,E7,E9,F") ||
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
		len(vulture.Argv) != 4 || vulture.Argv[0] != repo.PythonTool() || vulture.Argv[1] != "-I" || vulture.Argv[2] != "-c" ||
		strings.Contains(strings.Join(vulture.Argv, "\x00"), ".venv") || strings.ContainsAny(vulture.Argv[3], "\x00\r\n") {
		t.Fatalf("Vulture command = %+v", vulture)
	}
	pythonQualityAssertVultureRequest(t, vulture.Stdin)
}

func pythonQualityAssertVultureRequest(t *testing.T, data []byte) {
	t.Helper()
	request := pythonVultureRequest{}
	if err := json.Unmarshal(data, &request); err != nil || request.Protocol != pythonVultureProtocolVersion ||
		request.ToolVersion != pythonVultureVersion || !slices.EqualFunc(request.Files, []pythonVultureFile{{Path: "src/app.py", Module: "app"}}, func(left, right pythonVultureFile) bool {
		return left == right
	}) {
		t.Fatalf("Vulture request = %+v, error = %v", request, err)
	}
}

func pythonQualityAssertCommandPlan(t *testing.T, repo repository.Repository, commands []policy.Command) {
	t.Helper()
	if planned := pythonQualityCommands(repo, []string{"src/app.py"}); !slices.EqualFunc(planned, commands, samePythonQualityCommand) {
		t.Fatalf("planned commands = %+v, run commands = %+v", planned, commands)
	}
}

func TestPythonQualityEmitsOneExactTyFindingPerDiagnostic(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\ndependencies = [\"example==1.0\"]\n")
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
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
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
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
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
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
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
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
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
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\ndependencies = [\"example==1.0\"]\n")
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
		{name: "escaping ty", output: `[{"check_name":"invalid-assignment","description":"wrong","location":{"path":"../../outside.py","lines":{"begin":1}}}]`, check: "quality.typecheckCoverage", command: "policy-ty-typecheck-root"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			repo := pythonQualityRepository(t)
			manifest := "[project]\nname = \"example\"\nversion = \"0\"\n"
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
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
	writeQualityFile(t, repo.Root, "src/first.py", "def first():\n    return 1\n")
	writeQualityFile(t, repo.Root, "src/second.py", "def second():\n    return 2\n")
	project := pythonVultureProject(t, repo)
	command, err := pythonVultureCommand(repo, project)
	if err != nil {
		t.Fatal(err)
	}
	request := pythonVultureRequest{}
	if err := json.Unmarshal(command.Stdin, &request); err != nil {
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
		Project: "pyproject.toml", Module: "app", Symbol: "handler",
	}}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
	writeQualityFile(t, repo.Root, "app.py", "def handler():\n    return 1\n")
	project := pythonVultureProject(t, repo)
	references, _ := pythonVultureReferences(repo, project)
	if len(references) != 1 {
		t.Fatalf("references = %+v", references)
	}
	findings := pythonVultureFindings(repo, project, pythonVultureOutput(t, project.Files, nil, nil, []pythonVultureProblem{{
		ID: references[0].ID, Message: "symbol is stale or ambiguous",
	}}, ""))
	if len(findings) != 1 || findings[0].Check != "policy.pythonDynamicReference" || findings[0].Path != policy.ConfigFilename ||
		findings[0].Subject != references[0].ID {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonVultureValidatesUnknownAndUnselectedReferenceProjects(t *testing.T) {
	t.Parallel()
	t.Run("unknown project", func(t *testing.T) {
		repo := pythonQualityRepository(t)
		repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{
			Project: "apps/missing/pyproject.toml", Module: "missing", Symbol: "handler",
		}}
		writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
		writeQualityFile(t, repo.Root, "src/app.py", "value = 1\n")
		plan := pythonQualityPlanFor(repo, []string{"src/app.py"})
		if !slices.ContainsFunc(plan.findings, func(finding policy.Finding) bool {
			return finding.Check == "policy.pythonDynamicReference" && finding.Subject == "config:apps/missing/pyproject.toml:missing:handler"
		}) {
			t.Fatalf("plan = %+v", plan)
		}
	})
	t.Run("unselected project", func(t *testing.T) {
		repo := pythonQualityRepository(t)
		repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{
			Project: "apps/other/pyproject.toml", Module: "other", Symbol: "missing",
		}}
		writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"root\"\nversion = \"0\"\n")
		writeQualityFile(t, repo.Root, "src/app.py", "value = 1\n")
		writeQualityFile(t, repo.Root, "apps/other/pyproject.toml", "[project]\nname = \"other\"\nversion = \"0\"\n")
		writeQualityFile(t, repo.Root, "apps/other/other.py", "def handler():\n    return 1\n")
		plan := pythonQualityPlanFor(repo, []string{"src/app.py"})
		project := slices.IndexFunc(plan.projects, func(project pythonQualityProject) bool {
			return project.project.Manifest == "apps/other/pyproject.toml"
		})
		if project < 0 || len(plan.projects[project].commands) != 1 || plan.projects[project].commands[0].kind != pythonVultureQualityKind {
			t.Fatalf("plan = %+v", plan)
		}
		references, _ := pythonVultureReferences(repo, plan.projects[project].project)
		findings := pythonQualityOutputFindings(repo, plan.projects[project], pythonVultureQualityKind, pythonVultureOutput(t,
			plan.projects[project].project.Files, nil, nil, []pythonVultureProblem{{ID: references[0].ID, Message: "symbol is stale or ambiguous"}}, ""))
		if len(findings) != 1 || findings[0].Check != "policy.pythonDynamicReference" || findings[0].Path != policy.ConfigFilename {
			t.Fatalf("findings = %+v", findings)
		}
	})
}

func TestPythonVultureValidatesReferencesWithoutSelectedPythonSource(t *testing.T) {
	t.Parallel()
	t.Run("configured reference with no runtime source", func(t *testing.T) {
		repo := pythonQualityRepository(t)
		repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{
			Project: "pyproject.toml", Module: "app", Symbol: "handler",
		}}
		writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
		plan := pythonQualityPlanFor(repo, nil)
		if len(plan.projects) != 0 || !slices.ContainsFunc(plan.findings, func(finding policy.Finding) bool {
			return finding.Check == "policy.pythonDynamicReference" && finding.Path == policy.ConfigFilename &&
				finding.Subject == "config:pyproject.toml:app:handler"
		}) {
			t.Fatalf("plan = %+v", plan)
		}
	})
	t.Run("inferred reference with only its manifest selected", func(t *testing.T) {
		repo := pythonQualityRepository(t)
		writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n\n[project.scripts]\nexample = \"app:handler\"\n")
		plan := pythonQualityPlanFor(repo, []string{"pyproject.toml"})
		if len(plan.projects) != 0 || !slices.ContainsFunc(plan.findings, func(finding policy.Finding) bool {
			return finding.Check == "policy.pythonDynamicReference" && finding.Path == "pyproject.toml" &&
				strings.HasPrefix(finding.Subject, "manifest:pyproject.toml:project.scripts:example:app:handler")
		}) {
			t.Fatalf("plan = %+v", plan)
		}
	})
	t.Run("no Python and no reference work", func(t *testing.T) {
		repo := pythonQualityRepository(t)
		writeQualityFile(t, repo.Root, "README.md", "# example\n")
		if plan := pythonQualityPlanFor(repo, []string{"README.md"}); len(plan.projects) != 0 || len(plan.findings) != 0 {
			t.Fatalf("plan = %+v", plan)
		}
	})
}

func TestPythonVultureCommandFailureCoversFullProjectOnce(t *testing.T) {
	t.Parallel()
	repo := pythonQualityRepository(t)
	repo.PolicyRoot = ""
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
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
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\n")
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
	writeQualityFile(t, repo.Root, "pyproject.toml", "project = { name = \"example\", version = \"0\", scripts = { example = \"app:handler\" } }\n")
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
		"diagnostics": diagnostics, "resolved": resolved, "problems": problems, "error": analysisError,
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
		Name: "probe-vulture", Argv: []string{repo.PythonTool(), "-I", "-c", "import importlib.metadata;print(importlib.metadata.version('vulture'))"},
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
