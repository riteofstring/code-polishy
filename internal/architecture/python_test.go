package architecture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type pythonGraphRunner struct {
	outputs  map[string]string
	commands []policy.Command
	err      error
}

func (graphRunner *pythonGraphRunner) Run(context.Context, string, policy.Command) error {
	return graphRunner.err
}

func (graphRunner *pythonGraphRunner) RunStructured(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	graphRunner.commands = append(graphRunner.commands, command)
	if graphRunner.err != nil {
		return runner.Result{}, runner.Output{}, graphRunner.err
	}
	output, found := graphRunner.outputs[command.Cwd]
	if !found {
		return runner.Result{}, runner.Output{}, errors.New("no graph output was configured")
	}
	return runner.Result{}, runner.Output{Stdout: []byte(output)}, nil
}

func TestPythonArchitectureRunsAnIsolatedPolicyGraphAndReportsForbiddenEdges(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{
		{Name: "domain", Paths: []string{"src/domain/**"}},
		{Name: "web", Paths: []string{"src/web/**"}},
	})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n\n[tool.ruff]\nexclude = [\"src/web/app.py\"]\n")
	writeArchitectureFile(t, repo.Root, "src/domain/__init__.py", "")
	writeArchitectureFile(t, repo.Root, "src/domain/model.py", "")
	writeArchitectureFile(t, repo.Root, "src/web/__init__.py", "")
	writeArchitectureFile(t, repo.Root, "src/web/app.py", "import domain.model\n")
	graphRunner := &pythonGraphRunner{outputs: map[string]string{
		".": `{"src/web/app.py":["src/domain/model.py"]}`,
	}}
	findings := CheckWithRunner(t.Context(), repo, []string{"src/web/app.py"}, graphRunner)
	assertPythonForbiddenImportFinding(t, findings)
	command := assertPythonGraphCommand(t, repo, graphRunner.commands)
	assertPythonGraphCommandIsIsolated(t, repo, command)
	assertPythonGraphPlan(t, repo, graphRunner.commands)
}

func assertPythonForbiddenImportFinding(t *testing.T, findings []policy.Finding) {
	t.Helper()
	if len(findings) != 1 || findings[0].Check != "architecture.moduleDependency" || findings[0].Path != "src/web/app.py" || findings[0].Subject != "domain" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Message != `module "web" imports module "domain" without declaring dependsOn` {
		t.Fatalf("message = %q", findings[0].Message)
	}
}

func assertPythonGraphCommand(t *testing.T, repo repository.Repository, commands []policy.Command) policy.Command {
	t.Helper()
	if len(commands) != 1 {
		t.Fatalf("commands = %+v", commands)
	}
	command := commands[0]
	checks := []struct {
		name  string
		valid bool
	}{
		{"working directory", command.Cwd == "."},
		{"sealed environment", command.SealedEnvironment},
		{"timeout", command.TimeoutSeconds == 900},
		{"policy Ruff", command.Argv[0] == repo.PolicyTool("ruff")},
	}
	for _, check := range checks {
		if !check.valid {
			t.Fatalf("%s command = %+v", check.name, command)
		}
	}
	return command
}

func assertPythonGraphCommandIsIsolated(t *testing.T, repo repository.Repository, command policy.Command) {
	t.Helper()
	arguments := strings.Join(command.Argv, "\x00")
	for _, flag := range []string{"--isolated", "--detect-string-imports", "--type-checking-imports"} {
		if !strings.Contains(arguments, flag) {
			t.Fatalf("missing %s in command = %+v", flag, command)
		}
	}
	for _, expected := range []string{
		"--target-version\x00py312",
		"--config\x00line-length = 88",
		"--config\x00lint.pycodestyle.max-line-length = 88",
		`--config` + "\x00" + `src = [".", "src"]`,
	} {
		if !strings.Contains(arguments, expected) {
			t.Fatalf("Ruff graph command lacks %q: %+v", expected, command)
		}
	}
	minimumDots := slicesIndex(command.Argv, "--min-dots")
	if minimumDots < 0 || minimumDots+1 >= len(command.Argv) || command.Argv[minimumDots+1] != "0" {
		t.Fatalf("string import detection = %+v", command.Argv)
	}
	if strings.Contains(arguments, "exclude") || !strings.Contains(arguments, "src/web/app.py") {
		t.Fatalf("target Ruff configuration changed the graph command: %+v", command.Argv)
	}
	if command.Argv[0] != repo.PolicyTool("ruff") {
		t.Fatalf("command = %+v", command)
	}
}

func assertPythonGraphPlan(t *testing.T, repo repository.Repository, commands []policy.Command) {
	t.Helper()
	planned := PythonGraphCommands(repo, []string{"src/web/app.py"})
	if !reflect.DeepEqual(planned, commands) {
		t.Fatalf("planned commands = %+v, executed commands = %+v", planned, commands)
	}
}

func TestPythonArchitectureCoversFlatNamespaceRelativeReexportStubTypeAndLiteralImports(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"**/*.py", "**/*.pyi"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "flat/domain.py", "")
	writeArchitectureFile(t, repo.Root, "flat/web.py", "from flat import domain\n")
	writeArchitectureFile(t, repo.Root, "namespace/model.py", "")
	writeArchitectureFile(t, repo.Root, "namespace/consumer.py", "from namespace import model\n")
	writeArchitectureFile(t, repo.Root, "pkg/__init__.py", "from .model import Thing\n")
	writeArchitectureFile(t, repo.Root, "pkg/model.py", "class Thing:\n    pass\n")
	writeArchitectureFile(t, repo.Root, "pkg/service.py", "from . import model\nif TYPE_CHECKING:\n    from . import model\nimport importlib\nimportlib.import_module(\"pkg.model\")\n__import__(\"pkg.model\")\n")
	writeArchitectureFile(t, repo.Root, "pkg/service.pyi", "from . import model\n")
	writeArchitectureFile(t, repo.Root, "pkg/sub/deep/service.py", "from ... import model\n")
	graphRunner := &pythonGraphRunner{outputs: map[string]string{
		".": `{
			"flat/web.py":["flat/domain.py"],
			"namespace/consumer.py":["namespace/model.py"],
			"pkg/__init__.py":["pkg/model.py"],
			"pkg/service.py":["pkg/model.py"],
			"pkg/service.pyi":["pkg/model.py"],
			"pkg/sub/deep/service.py":["pkg/model.py"]
		}`,
	}}
	selected := []string{"flat/web.py", "namespace/consumer.py", "pkg/__init__.py", "pkg/service.py", "pkg/service.pyi", "pkg/sub/deep/service.py"}
	if findings := CheckWithRunner(t.Context(), repo, selected, graphRunner); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonArchitectureScopesOverlappingImportsToEachNestedProject(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{
		{Name: "root-common", Paths: []string{"src/common/**"}},
		{Name: "root-app", Paths: []string{"src/root/**"}, DependsOn: []string{"root-common"}},
		{Name: "child-common", Paths: []string{"apps/child/src/common/**"}},
		{Name: "child-app", Paths: []string{"apps/child/src/child/**"}, DependsOn: []string{"child-common"}},
	})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"root\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "src/common/model.py", "")
	writeArchitectureFile(t, repo.Root, "src/root/app.py", "import common.model\n")
	writeArchitectureFile(t, repo.Root, "apps/child/pyproject.toml", "[project]\nname = \"child\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "apps/child/src/common/model.py", "")
	writeArchitectureFile(t, repo.Root, "apps/child/src/child/app.py", "import common.model\n")
	graphRunner := &pythonGraphRunner{outputs: map[string]string{
		".":          `{"src/root/app.py":["src/common/model.py"]}`,
		"apps/child": `{"src/child/app.py":["src/common/model.py"]}`,
	}}
	selected := []string{"src/root/app.py", "apps/child/src/child/app.py"}
	if findings := CheckWithRunner(t.Context(), repo, selected, graphRunner); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if len(graphRunner.commands) != 2 || graphRunner.commands[0].Cwd != "apps/child" || graphRunner.commands[1].Cwd != "." {
		t.Fatalf("commands = %+v", graphRunner.commands)
	}
}

func TestPythonArchitectureRejectsCrossProjectGraphTargets(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{
		{Name: "root-common", Paths: []string{"src/common/**"}},
		{Name: "child", Paths: []string{"apps/child/src/child/**"}, DependsOn: []string{"root-common"}},
	})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"root\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "src/common/model.py", "")
	writeArchitectureFile(t, repo.Root, "apps/child/pyproject.toml", "[project]\nname = \"child\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "apps/child/src/child/app.py", "import common.model\n")
	graphRunner := &pythonGraphRunner{outputs: map[string]string{
		"apps/child": `{"src/child/app.py":["../../src/common/model.py"]}`,
	}}
	findings := CheckWithRunner(t.Context(), repo, []string{"apps/child/src/child/app.py"}, graphRunner)
	if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" ||
		findings[0].Path != "apps/child/src/child/app.py" || !strings.Contains(findings[0].Message, "outside the selected Python project") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonArchitectureReportsGraphCoverageFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		source  string
		output  string
		message string
	}{
		{
			name:    "omitted",
			source:  "import domain.model\n",
			output:  "{}",
			message: "omitted this selected Python source",
		},
		{
			name:    "malformed",
			source:  "import domain.model\n",
			output:  "[]",
			message: "graph output is malformed",
		},
		{
			name:    "escaping target",
			source:  "import domain.model\n",
			output:  `{"src/web/app.py":["../../outside.py"]}`,
			message: "outside the repository",
		},
		{
			name:    "unresolved local-looking import",
			source:  "import domain.missing\n",
			output:  `{"src/web/app.py":[]}`,
			message: "cannot be resolved",
		},
		{
			name:    "nonliteral dynamic import",
			source:  "import importlib\nimportlib.import_module(module_name)\n",
			output:  `{"src/web/app.py":[]}`,
			message: "dynamic Python import cannot be proven",
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repo := pythonArchitectureRepository(t, []policy.Module{
				{Name: "domain", Paths: []string{"src/domain/**"}},
				{Name: "web", Paths: []string{"src/web/**"}},
			})
			writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
			writeArchitectureFile(t, repo.Root, "src/domain/model.py", "")
			writeArchitectureFile(t, repo.Root, "src/web/app.py", testCase.source)
			graphRunner := &pythonGraphRunner{outputs: map[string]string{".": testCase.output}}
			findings := CheckWithRunner(t.Context(), repo, []string{"src/web/app.py"}, graphRunner)
			if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" ||
				findings[0].Path != "src/web/app.py" || !strings.Contains(findings[0].Message, testCase.message) {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestPythonGraphFindingsRejectsOmittedSource(t *testing.T) {
	t.Parallel()
	findings := pythonGraphFindings(repository.Repository{}, repository.PythonProject{}, []string{"source.py"}, nil, nil, pythonGraph{})
	if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" || findings[0].Path != "source.py" || !strings.Contains(findings[0].Message, "omitted this selected Python source") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonArchitectureCoversAliasedDynamicImportsWithoutGuessing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		source   string
		output   string
		findings int
		message  string
	}{
		{
			name:     "module alias with nonliteral argument",
			source:   "import importlib as il\nil.import_module(module_name)\n",
			output:   `{"src/web/app.py":[]}`,
			findings: 1,
			message:  "dynamic Python import cannot be proven",
		},
		{
			name:     "function alias with nonliteral argument",
			source:   "from importlib import import_module as load\nload(module_name)\n",
			output:   `{"src/web/app.py":[]}`,
			findings: 1,
			message:  "dynamic Python import cannot be proven",
		},
		{
			name:     "nested module import binds importlib",
			source:   "import importlib.util\nimportlib.import_module(module_name)\n",
			output:   `{"src/web/app.py":[]}`,
			findings: 1,
			message:  "dynamic Python import cannot be proven",
		},
		{
			name:   "module alias with literal argument",
			source: "import importlib as il\nil.import_module(\"domain.model\")\n",
			output: `{"src/web/app.py":["src/domain/model.py"]}`,
		},
		{
			name:   "function alias with literal argument",
			source: "from importlib import import_module as load\nload(\"domain.model\")\n",
			output: `{"src/web/app.py":["src/domain/model.py"]}`,
		},
		{
			name:   "unrelated method",
			source: "loader.import_module(module_name)\n",
			output: `{"src/web/app.py":[]}`,
		},
		{
			name:   "nested module alias does not bind importlib",
			source: "import importlib.util as util\nutil.import_module(module_name)\n",
			output: `{"src/web/app.py":[]}`,
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repo := pythonArchitectureRepository(t, []policy.Module{
				{Name: "domain", Paths: []string{"src/domain/**"}},
				{Name: "web", Paths: []string{"src/web/**"}, DependsOn: []string{"domain"}},
			})
			writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
			writeArchitectureFile(t, repo.Root, "src/domain/model.py", "")
			writeArchitectureFile(t, repo.Root, "src/web/app.py", testCase.source)
			graphRunner := &pythonGraphRunner{outputs: map[string]string{".": testCase.output}}
			findings := CheckWithRunner(t.Context(), repo, []string{"src/web/app.py"}, graphRunner)
			if len(findings) != testCase.findings {
				t.Fatalf("findings = %+v", findings)
			}
			if testCase.message != "" && (findings[0].Check != "architecture.importCoverage" || !strings.Contains(findings[0].Message, testCase.message)) {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestPythonArchitectureReportsConflictingSourceRoots(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"**/*.py"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "shared.py", "")
	writeArchitectureFile(t, repo.Root, "src/shared.py", "")
	writeArchitectureFile(t, repo.Root, "src/web/app.py", "import shared\n")
	graphRunner := &pythonGraphRunner{outputs: map[string]string{
		".": `{"src/web/app.py":["src/shared.py"]}`,
	}}
	findings := CheckWithRunner(t.Context(), repo, []string{"src/web/app.py"}, graphRunner)
	if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" ||
		!strings.Contains(findings[0].Message, "conflicting source roots") {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonArchitectureDetectsUnresolvedImportsInValidStatementShapes(t *testing.T) {
	t.Parallel()
	cases := []string{
		"import domain.missing; marker = 1\n",
		"import domain.\\\nmissing\n",
		"from domain.missing import (\n    value,\n)\n",
		"from domain.missing \\\n    import value\n",
	}
	for _, source := range cases {
		source := source
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			repo := pythonArchitectureRepository(t, []policy.Module{
				{Name: "domain", Paths: []string{"src/domain/**"}},
				{Name: "web", Paths: []string{"src/web/**"}},
			})
			writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
			writeArchitectureFile(t, repo.Root, "src/domain/model.py", "")
			writeArchitectureFile(t, repo.Root, "src/web/app.py", source)
			graphRunner := &pythonGraphRunner{outputs: map[string]string{".": `{"src/web/app.py":[]}`}}
			findings := CheckWithRunner(t.Context(), repo, []string{"src/web/app.py"}, graphRunner)
			if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" ||
				!strings.Contains(findings[0].Message, "cannot be resolved") {
				t.Fatalf("findings = %+v", findings)
			}
		})
	}
}

func TestPythonArchitectureDoesNotTurnStandardLibraryOrThirdPartyImportsIntoModuleEdges(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"src/app/**"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "src/app/main.py", "import json\nimport requests\n")
	graphRunner := &pythonGraphRunner{outputs: map[string]string{".": `{"src/app/main.py":[]}`}}
	if findings := CheckWithRunner(t.Context(), repo, []string{"src/app/main.py"}, graphRunner); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonArchitectureReportsRuffToolFailure(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"app/**"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "app/main.py", "")
	graphRunner := &pythonGraphRunner{err: errors.New("Ruff did not start")}
	findings := CheckWithRunner(t.Context(), repo, []string{"app/main.py"}, graphRunner)
	if len(findings) != 1 || findings[0].Check != "policy.tool" || findings[0].Path != "pyproject.toml" || findings[0].Subject != "ruff" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonGraphAcceptsAbsoluteContainedPaths(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"app/**"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "app/main.py", "from . import helper\n")
	writeArchitectureFile(t, repo.Root, "app/helper.py", "")
	source := filepath.ToSlash(filepath.Join(repo.Root, "app", "main.py"))
	target := filepath.ToSlash(filepath.Join(repo.Root, "app", "helper.py"))
	graphRunner := &pythonGraphRunner{outputs: map[string]string{
		".": `{"` + source + `":["` + target + `"]}`,
	}}
	if findings := CheckWithRunner(t.Context(), repo, []string{"app/main.py"}, graphRunner); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestPythonArchitectureUsesPinnedRuffForOneComponentLiteralDynamicImports(t *testing.T) {
	t.Parallel()
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"src/**"}}})
	repo.PolicyRoot = policyRoot
	if _, err := os.Stat(repo.PolicyTool("ruff")); err != nil {
		t.Skip("the pinned Ruff executable is unavailable")
	}
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "src/model.py", "")
	writeArchitectureFile(t, repo.Root, "src/app.py", "__import__(\"model\")\n")
	if findings := Check(t.Context(), repo, []string{"src/app.py"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func pythonArchitectureRepository(t *testing.T, modules []policy.Module) repository.Repository {
	t.Helper()
	config := policy.Config{Modules: modules, ModuleByName: map[string]int{}}
	for index, module := range modules {
		config.ModuleByName[module.Name] = index
	}
	return repository.Repository{Root: t.TempDir(), PolicyRoot: t.TempDir(), Config: config}
}

func slicesIndex(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
