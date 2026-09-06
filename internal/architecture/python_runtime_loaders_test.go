package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
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

func TestPythonRuntimeLoaderBoundaryAndKnownEdges(t *testing.T) {
	repo := pythonArchitectureRepository(t, []policy.Module{
		{Name: "plugins", Paths: []string{"src/app/plugins/**"}},
		{Name: "application", Paths: []string{"src/app/loader.py", "src/app/main.py", "src/app/exports.py"}},
	})
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo.PolicyRoot = root
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "src/app/loader.py", runtimeLoaderExample)
	writeArchitectureFile(t, repo.Root, "src/app/main.py", "from app.exports import load\nlocal = load(\"app.plugins.worker:registry.primary\")\nexternal = load(\"vendor_package.worker:instance\")\nrejected = load(\"not-a-target\")\n")
	writeArchitectureFile(t, repo.Root, "src/app/exports.py", "from app.loader import load\n")
	writeArchitectureFile(t, repo.Root, "src/app/plugins/worker.py", "raise RuntimeError(\"analysis must not execute this module\")\nregistry = object()\n")
	repo.Config.Scope.PythonRuntimeLoaders = []policy.PythonRuntimeLoader{runtimeLoaderDeclaration(runtimeLoaderExample)}
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py", "src/app/main.py"}, runner.OSRunner{})
	information, dependency := 0, 0
	for _, finding := range analysis.Findings {
		if finding.Severity == policy.FindingInformation {
			information++
			continue
		}
		if finding.Check == "architecture.moduleDependency" {
			dependency++
			continue
		}
		t.Fatalf("unexpected findings: %+v", analysis.Findings)
	}
	if information != 1 || dependency != 1 {
		t.Fatalf("runtime boundary lost delegation or local policy: %+v", analysis.Findings)
	}
	assertPythonModuleImportEdge(t, analysis.Graph, "src/app/main.py", "src/app/plugins/worker.py", "proven-dynamic")
	repo.Config.Modules[1].DependsOn = []string{"plugins"}
	for _, finding := range Check(t.Context(), repo, []string{"src/app/main.py"}) {
		if finding.Severity != policy.FindingInformation {
			t.Fatalf("authorized edge failed: %+v", finding)
		}
	}
}

func TestPythonRuntimeLoaderRejectsChangedEvidence(t *testing.T) {
	for name, change := range map[string]func(string) string{
		"unguarded": func(s string) string {
			return strings.Replace(s, "TARGET.fullmatch(target)", "TARGET.match(target)", 1)
		},
		"different value": func(s string) string {
			return strings.Replace(s, "isinstance(loaded, Contract)", "isinstance(target, Contract)", 1)
		},
		"shadowed import": func(s string) string {
			return strings.Replace(s, "import importlib", "from vendor import importlib", 1)
		},
		"shadowed getattr":    func(s string) string { return s + "\ngetattr = lambda value, name: value\n" },
		"unchecked return":    func(s string) string { return strings.Replace(s, "return loaded", "return target", 1) },
		"nonruntime protocol": func(s string) string { return strings.Replace(s, "@runtime_checkable", "", 1) },
	} {
		t.Run(name, func(t *testing.T) {
			repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"src/app/**"}}})
			root, err := filepath.Abs("../..")
			if err != nil {
				t.Fatal(err)
			}
			repo.PolicyRoot = root
			source := change(runtimeLoaderExample)
			writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\n")
			writeArchitectureFile(t, repo.Root, "src/app/loader.py", source)
			repo.Config.Scope.PythonRuntimeLoaders = []policy.PythonRuntimeLoader{runtimeLoaderDeclaration(source)}
			findings := Check(t.Context(), repo, []string{"src/app/loader.py"})
			failed := false
			for _, finding := range findings {
				if finding.Check == "architecture.importCoverage" && finding.Severity != policy.FindingInformation {
					failed = true
				}
			}
			if !failed {
				t.Fatalf("invalid loader accepted: %+v", findings)
			}
		})
	}
}

func runtimeLoaderDeclaration(source string) policy.PythonRuntimeLoader {
	digest := sha256.Sum256([]byte(source))
	return policy.PythonRuntimeLoader{Project: "pyproject.toml", InputGrammar: "ascii-module-object/v1", Reason: "Operators select installed adapters at startup.", Consumer: policy.PythonDynamicConsumer{Kind: "callsite", Importer: "src/app/loader.py", Module: "app.loader", Callable: "load", Site: policy.PythonSourceLocation{Line: 15, Column: 22}, Callee: "importlib.import_module", Shape: "call", Argument: "module_name", SourceSHA256: hex.EncodeToString(digest[:])}, Check: policy.PythonRuntimeCheck{Kind: "isinstance", Protocol: "app.loader.Contract", Site: policy.PythonSourceLocation{Line: 18, Column: 12}}}
}
