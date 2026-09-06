package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDeclaredPythonExternalPluginProducesComposition(t *testing.T) {
	repo, runner := externalArchitectureFixture(t, false)
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, runner)
	if analysis.Graph == nil || len(analysis.Findings) != 0 || len(analysis.Graph.External) != 1 {
		t.Fatalf("declared external composition was not retained: %+v", analysis)
	}
	edge := analysis.Graph.External[0]
	if edge.Dependency.Distribution != "plug-dist" || edge.Dependency.Namespace != "third_party.plugins" || edge.Contract.Protocol != "app.contracts.Contract" {
		t.Fatalf("external declaration lost its dependency contract: %+v", edge)
	}
}

func TestDeclaredPythonExternalPluginRejectsStaleSourceAndDependencyEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, *repository.Repository){
		"source": func(_ *testing.T, repo *repository.Repository) {
			repo.Config.Scope.PythonExternalPluginImports[0].Consumer.SourceSHA256 = strings.Repeat("0", 64)
		},
		"dependency": func(t *testing.T, repo *repository.Repository) {
			writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = 'example'\nversion = '0'\nrequires-python = '==3.12.*'\ndependencies = []\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, runner := externalArchitectureFixture(t, false)
			mutate(t, &repo)
			analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, runner)
			if analysis.Graph != nil || len(analysis.Findings) == 0 {
				t.Fatalf("stale external declaration acquired graph evidence: %+v", analysis)
			}
		})
	}
}

func TestPythonExternalPluginSelectionRetainsOnlyItsConsumerProject(t *testing.T) {
	repo, _ := externalArchitectureFixture(t, false)
	for _, selected := range [][]string{nil, {"AGENTS.md"}, {"README.md"}, {"other.go"}} {
		if commands := PythonGraphCommands(repo, selected); len(commands) != 0 {
			t.Fatalf("unrelated selection started plug-in analysis: %+v", commands)
		}
	}
	for _, selected := range [][]string{{"src/app/loader.py"}, {"uv.lock"}, {"pyproject.toml"}, {policy.ConfigFilename}} {
		if commands := PythonGraphCommands(repo, selected); len(commands) != 2 {
			t.Fatalf("selected plug-in input did not select its project: %+v", commands)
		}
	}
}

func externalArchitectureFixture(t *testing.T, _ bool) (repository.Repository, *pythonGraphRunner) {
	t.Helper()
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"src/app/**"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = 'example'\nversion = '0'\nrequires-python = '==3.12.*'\ndependencies = ['plug-dist==1.0']\n")
	writeArchitectureFile(t, repo.Root, "uv.lock", "version = 1\n[[package]]\nname = 'plug-dist'\nversion = '1.0.0'\nsource = {registry = 'https://pypi.org/simple'}\n")
	writeArchitectureFile(t, repo.Root, "src/app/contracts.py", "from typing import Protocol\nclass Contract(Protocol):\n    def run(self): ...\n")
	source := "from pkgutil import resolve_name\nfrom app.contracts import Contract\ndef load():\n    plugin = resolve_name('third_party.plugins:Plugin')\n    if not isinstance(plugin, Contract):\n        raise TypeError\n    return plugin\n"
	writeArchitectureFile(t, repo.Root, "src/app/loader.py", source)
	digest := sha256.Sum256([]byte(source))
	repo.Config.Scope.PythonExternalPluginImports = []policy.PythonExternalPluginImport{{Project: "pyproject.toml", Distribution: "plug-dist", Namespace: "third_party.plugins", InputGrammar: "python-module-object/v1", Consumer: policy.PythonDynamicConsumer{Kind: "callsite", Importer: "src/app/loader.py", Module: "app.loader", Callable: "load", Site: policy.PythonSourceLocation{Line: 4, Column: 14}, Callee: "pkgutil.resolve_name", Shape: "module-object-call/v1", Argument: "'third_party.plugins:Plugin'", SourceSHA256: hex.EncodeToString(digest[:])}, Check: policy.PythonRuntimeCheck{Kind: "isinstance", Protocol: "app.contracts.Contract", Site: policy.PythonSourceLocation{Line: 5, Column: 12}}}}
	return repo, &pythonGraphRunner{outputs: map[string]string{".": `{"src/app/loader.py":["src/app/contracts.py"]}`}}
}
