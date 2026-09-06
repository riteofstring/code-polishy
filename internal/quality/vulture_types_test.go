package quality

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPythonVultureTypedDictReadsPreserveOnlyTheirExactFields(t *testing.T) {
	sources := map[string]string{
		"src/models.py": `from typing import TypedDict
class Request(TypedDict):
    name: str
    unused: str
class Other(TypedDict):
    name: str
class Child(Request):
    extra: int
`,
		"src/contracts.py": "from models import Child as Payload\n",
		"src/service.py":   "from contracts import Payload\ndef handle(payload: Payload):\n    alias = payload\n    return alias[\"name\"]\n",
	}
	_, _, response, _ := runTypedDictVulture(t, sources)
	if response.Error != "" || len(response.Problems) != 0 {
		t.Fatalf("TypedDict analysis = %+v", response)
	}
	if slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool {
		return diagnostic.Path == "src/models.py" && diagnostic.Name == "name" && diagnostic.Line == 3
	}) {
		t.Fatalf("proven TypedDict field was reported dead: %+v", response.Diagnostics)
	}
	for _, field := range []struct {
		name string
		line int
	}{{"unused", 4}, {"name", 6}, {"extra", 8}} {
		if !slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool {
			return diagnostic.Path == "src/models.py" && diagnostic.Name == field.name && diagnostic.Line == field.line
		}) {
			t.Fatalf("unrelated %s at line %d was hidden: %+v", field.name, field.line, response.Diagnostics)
		}
	}
}

func TestPythonVultureTypedDictUnknownReceiversAndDictionaryMethodsDoNotPreserveKeys(t *testing.T) {
	source := `from typing import Any, TypedDict
class Payload(TypedDict):
    key: str
def read(value: Payload | None, unknown: Any, untyped):
    return value["key"], unknown["key"], untyped["key"]
def methods(value: Payload, dynamic_key):
    return value.get("key"), value.pop("key"), value[dynamic_key]
`
	_, _, response, _ := runTypedDictVulture(t, map[string]string{"src/source.py": source})
	if response.Error != "" || !slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool {
		return diagnostic.Name == "key" && diagnostic.Path == "src/source.py" && diagnostic.Line == 3
	}) {
		t.Fatalf("unproven key acquired reachability: %+v", response)
	}
}

func TestPythonVultureTypedDictInvalidFactsProduceOneProjectCoverageFailure(t *testing.T) {
	sources := map[string]string{
		"src/source.py": "from typing import TypedDict\nclass Payload(TypedDict):\n    key: str\n    key: int\ndef read(value: Payload):\n    return value[\"key\"]\n",
		"src/other.py":  "def unused(): pass\n",
	}
	repo, project, response, output := runTypedDictVulture(t, sources)
	if !strings.Contains(response.FactsError, "duplicate keys") || response.Error == "" {
		t.Fatalf("invalid type facts = %+v", response)
	}
	findings := pythonVultureFindings(repo, project, output)
	if len(findings) != 1 || findings[0].Check != "architecture.pythonFactsCoverage" || findings[0].Path != "pyproject.toml" {
		t.Fatalf("invalid fact set produced misleading per-file findings: %+v", findings)
	}
}

func TestPythonVultureProgramFitsTheSupportedProcessArgumentBoundary(t *testing.T) {
	repo, project, response, _ := runTypedDictVulture(t, map[string]string{"src/source.py": "value = 1\n"})
	command, err := pythonVultureCommand(repo, project)
	if err != nil {
		t.Fatal(err)
	}
	if arguments := strings.Join(command.Argv, "\x00"); len(arguments) > 30000 || strings.ContainsAny(command.Argv[4], "\r\n\x00") {
		t.Fatalf("isolated Vulture command exceeds its portable argument boundary: %d bytes", len(arguments))
	}
	if response.Error != "" || !slices.ContainsFunc(response.Diagnostics, func(diagnostic pythonVultureDiagnostic) bool { return diagnostic.Name == "value" }) {
		t.Fatalf("isolated program did not execute Vulture: %+v", response)
	}
}

func TestPythonVultureOversizedSourceFailsTheProjectBeforeDeadCodeAnalysis(t *testing.T) {
	source := "# " + strings.Repeat("x", 2*1024*1024) + "\n"
	repo, project, response, output := runTypedDictVulture(t, map[string]string{"src/large.py": source, "src/other.py": "unused = 1\n"})
	if !strings.Contains(response.FactsError, "per-source byte limit") || len(response.Diagnostics) != 0 {
		t.Fatalf("source boundary failure = %+v", response)
	}
	findings := pythonVultureFindings(repo, project, output)
	if len(findings) != 1 || findings[0].Check != "architecture.pythonFactsCoverage" || findings[0].Path != "pyproject.toml" {
		t.Fatalf("unbounded project claimed dead-code evidence: %+v", findings)
	}
}

func TestPythonVultureOversizedCallFactsWithholdProjectDiagnostics(t *testing.T) {
	source := "def invoke():\n    return print('" + strings.Repeat("x", 65537) + "')\n"
	repo, project, response, output := runTypedDictVulture(t, map[string]string{"src/large.py": source, "src/other.py": "unused = 1\n"})
	if !strings.Contains(response.FactsError, "call argument exceeds") || len(response.Diagnostics) != 0 {
		t.Fatalf("call fact boundary failure = %+v", response)
	}
	findings := pythonVultureFindings(repo, project, output)
	if len(findings) != 1 || findings[0].Check != "architecture.pythonFactsCoverage" || findings[0].Path != "pyproject.toml" {
		t.Fatalf("incomplete consumer facts produced derivative findings: %+v", findings)
	}
}

func runTypedDictVulture(t *testing.T, sources map[string]string) (repository.Repository, repository.PythonProject, pythonVultureResponse, []byte) {
	t.Helper()
	return runContractVulture(t, sources, nil)
}

func runContractVulture(t *testing.T, sources map[string]string, contracts []policy.PythonContract) (repository.Repository, repository.PythonProject, pythonVultureResponse, []byte) {
	t.Helper()
	repo := pythonQualityRepository(t)
	repo.Config.Scope.PythonContracts = contracts
	repo.PolicyRoot = pythonVulturePolicyRoot(t)
	if !pythonVultureRuntimeInstalled(t, repo) {
		t.Fatal("TypedDict tests require the policy-owned CPython and Vulture")
	}
	writeQualityFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	for path, source := range sources {
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
	if err != nil {
		t.Fatal(err)
	}
	return repo, project, response, output.Stdout
}
