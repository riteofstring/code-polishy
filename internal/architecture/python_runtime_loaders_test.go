package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const runtimeLoaderExample = `import importlib
import re
from typing import Protocol, runtime_checkable

@runtime_checkable
class Contract(Protocol):
    def run(self) -> None: ...

TARGET = re.compile(r"[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*:[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*")

def load(target: str) -> Contract:
    if TARGET.fullmatch(target) is None:
        raise ValueError("invalid target")
    module_name, object_path = target.split(":", maxsplit=1)
    loaded: object = importlib.import_module(module_name)
    for attribute in object_path.split("."):
        loaded = getattr(loaded, attribute)
    if not isinstance(loaded, Contract):
        raise TypeError("invalid contract")
    return loaded
`

func TestPythonRuntimeLoaderIsASelectedDeclarationWithoutProjectFactExpansion(t *testing.T) {
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"src/**"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "src/loader.py", runtimeLoaderExample)
	for index := range 220 {
		writeArchitectureFile(t, repo.Root, fmt.Sprintf("src/context_%03d.py", index), "value = 1\n")
	}
	declaration := runtimeLoaderDeclaration(runtimeLoaderExample)
	declaration.Consumer.Importer = "src/loader.py"
	declaration.Consumer.Module = "loader"
	repo.Config.Scope.PythonRuntimeLoaders = []policy.PythonRuntimeLoader{declaration}
	runner := &pythonGraphRunner{outputs: map[string]string{".": `{"src/loader.py":[]}`}}
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/loader.py"}, runner)
	if analysis.Graph == nil || len(analysis.Graph.Nodes) != 1 || len(analysis.Graph.Edges) != 0 {
		t.Fatalf("focused runtime loader expanded its project graph: %+v", analysis)
	}
	if len(analysis.Findings) != 1 || analysis.Findings[0].Severity != policy.FindingInformation || analysis.Findings[0].Subject != "runtime-loader" {
		t.Fatalf("runtime loader declaration was not reported independently: %+v", analysis.Findings)
	}
	if len(runner.commands) != 2 || !slices.Equal(runner.commands[0].Paths, []string{"src/loader.py"}) || !slices.Equal(runner.commands[1].Paths, []string{"src/loader.py"}) {
		t.Fatalf("focused runtime loader command expanded beyond its consumer: %+v", runner.commands)
	}
}

func TestPythonRuntimeLoaderDeclarationRejectsStaleSourceEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*policy.PythonRuntimeLoader){
		"digest":   func(value *policy.PythonRuntimeLoader) { value.Consumer.SourceSHA256 = strings.Repeat("a", 64) },
		"location": func(value *policy.PythonRuntimeLoader) { value.Consumer.Site.Line = 1000 },
		"module":   func(value *policy.PythonRuntimeLoader) { value.Consumer.Module = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"src/**"}}})
			writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\n")
			writeArchitectureFile(t, repo.Root, "src/loader.py", runtimeLoaderExample)
			declaration := runtimeLoaderDeclaration(runtimeLoaderExample)
			declaration.Consumer.Importer = "src/loader.py"
			declaration.Consumer.Module = "loader"
			mutate(&declaration)
			repo.Config.Scope.PythonRuntimeLoaders = []policy.PythonRuntimeLoader{declaration}
			runner := &pythonGraphRunner{outputs: map[string]string{".": `{"src/loader.py":[]}`}}
			findings := CheckWithRunner(t.Context(), repo, []string{"src/loader.py"}, runner)
			if len(findings) != 1 || findings[0].Severity == policy.FindingInformation || findings[0].Subject != "runtime-loader" {
				t.Fatalf("stale runtime declaration passed: %+v", findings)
			}
		})
	}
}

func runtimeLoaderDeclaration(source string) policy.PythonRuntimeLoader {
	digest := sha256.Sum256([]byte(source))
	return policy.PythonRuntimeLoader{Project: "pyproject.toml", InputGrammar: "ascii-module-object/v1", Reason: "Operators select installed adapters at startup.", Consumer: policy.PythonDynamicConsumer{Kind: "callsite", Importer: "src/app/loader.py", Module: "app.loader", Callable: "load", Site: policy.PythonSourceLocation{Line: 15, Column: 22}, Callee: "importlib.import_module", Shape: "call", Argument: "module_name", SourceSHA256: hex.EncodeToString(digest[:])}, Check: policy.PythonRuntimeCheck{Kind: "isinstance", Protocol: "app.loader.Contract", Site: policy.PythonSourceLocation{Line: 18, Column: 12}}}
}
