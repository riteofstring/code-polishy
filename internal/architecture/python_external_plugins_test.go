package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPythonExternalPluginProducesCompositionWithoutLocalPermission(t *testing.T) {
	repo, graphRunner := externalArchitectureFixture(t, false)
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner)
	if len(analysis.Findings) != 0 || analysis.Graph == nil || len(analysis.Graph.External) != 1 {
		t.Fatalf("external composition was not proven: %+v", analysis)
	}
	edge := analysis.Graph.External[0]
	if edge.Dependency.Distribution != "plug-dist" || edge.Dependency.Namespace != "third_party.plugins" || edge.Dependency.Version != "1.0.0" || edge.Contract.RuntimeType != "app.contracts.Contract" || edge.Contract.CheckKind != "issubclass" {
		t.Fatalf("external dependency lost its exact contract: %+v", edge)
	}
	if len(analysis.Graph.Nodes) != 2 || len(analysis.Graph.Edges) != 1 || analysis.Graph.Edges[0].Target != "src/app/contracts.py" {
		t.Fatalf("external object became a local graph node or edge: %+v", analysis.Graph)
	}
	summary := Summary(repo, []string{"src/app/loader.py", "src/app/contracts.py"}, analysis.Graph)
	if len(summary) != 1 || summary[0].External != 1 || summary[0].Outgoing != 0 {
		t.Fatalf("external composition altered local module direction: %+v", summary)
	}
}

func TestPythonExternalPluginRegistryBindsCurrentInputAndDependencyBytes(t *testing.T) {
	repo, graphRunner := externalArchitectureFixture(t, true)
	first := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/registry.json"}, graphRunner)
	if first.Graph == nil || len(first.Findings) != 0 || len(first.Graph.External) != 1 {
		t.Fatalf("governed external registry did not pass: %+v", first)
	}
	content := `{"plugins":{"chosen":"third_party.plugins.other:Plugin"}}`
	writeArchitectureFile(t, repo.Root, "src/app/registry.json", content)
	stale := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/registry.json"}, graphRunner)
	assertExternalArchitectureFailure(t, stale)
	digest := sha256.Sum256([]byte(content))
	repo.Config.Scope.PythonExternalPluginImports[0].Configuration.SHA256 = hex.EncodeToString(digest[:])
	refreshed := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/registry.json"}, graphRunner)
	if refreshed.Graph == nil || len(refreshed.Findings) != 0 || refreshed.Graph.External[0].Contract.InputSHA256 == first.Graph.External[0].Contract.InputSHA256 || refreshed.Graph.Inputs[0].ResolutionSHA256 == first.Graph.Inputs[0].ResolutionSHA256 {
		t.Fatalf("registry update did not refresh graph evidence: %+v", refreshed)
	}
	lock, err := repo.Read("uv.lock")
	if err != nil {
		t.Fatal(err)
	}
	writeArchitectureFile(t, repo.Root, "uv.lock", string(lock)+"\n")
	updated := AnalyzeWithRunner(t.Context(), repo, []string{"uv.lock"}, graphRunner)
	if updated.Graph == nil || len(updated.Findings) != 0 || updated.Graph.External[0].Dependency.LockSHA256 == refreshed.Graph.External[0].Dependency.LockSHA256 || updated.Graph.Identity == refreshed.Graph.Identity {
		t.Fatalf("lock update did not invalidate composition evidence: %+v", updated)
	}
}

func TestPythonExternalPluginRejectsStaleAndDisconnectedDeclarations(t *testing.T) {
	for name, change := range map[string]func(*policy.PythonExternalPluginImport){
		"source digest":           func(value *policy.PythonExternalPluginImport) { value.Consumer.SourceSHA256 = strings.Repeat("a", 64) },
		"loader moved":            func(value *policy.PythonExternalPluginImport) { value.Consumer.Site.Line++ },
		"check moved":             func(value *policy.PythonExternalPluginImport) { value.Check.Site.Line++ },
		"different protocol":      func(value *policy.PythonExternalPluginImport) { value.Check.Protocol = "app.contracts.Other" },
		"outside namespace":       func(value *policy.PythonExternalPluginImport) { value.Namespace = "other.plugins" },
		"undeclared distribution": func(value *policy.PythonExternalPluginImport) { value.Distribution = "transitive-only" },
		"different argument":      func(value *policy.PythonExternalPluginImport) { value.Consumer.Argument = "name" },
		"unsupported grammar":     func(value *policy.PythonExternalPluginImport) { value.InputGrammar = "python-expression/v1" },
		"wrong project":           func(value *policy.PythonExternalPluginImport) { value.Project = "other/pyproject.toml" },
	} {
		t.Run(name, func(t *testing.T) {
			repo, graphRunner := externalArchitectureFixture(t, false)
			change(&repo.Config.Scope.PythonExternalPluginImports[0])
			assertExternalArchitectureFailure(t, AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner))
		})
	}
}

func TestPythonExternalPluginCannotAdmitLocalStandardLibraryOrUnboundedInputs(t *testing.T) {
	for name, argument := range map[string]string{"local": "'app.contracts:Contract'", "standard library": "'collections:Counter'", "unbounded parameter": "name", "relative": "'.third_party:Plugin'", "wildcard": "'third_party.*:Plugin'"} {
		t.Run(name, func(t *testing.T) {
			repo, graphRunner := externalArchitectureFixture(t, false)
			changeExternalLoader(t, &repo, func(source string) string {
				return strings.Replace(source, "'third_party.plugins:Plugin'", argument, 1)
			})
			repo.Config.Scope.PythonExternalPluginImports[0].Consumer.Argument = argument
			assertExternalArchitectureFailure(t, AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner))
		})
	}
}

func TestPythonExternalPluginCannotCoverSiblingOrUncheckedLoads(t *testing.T) {
	for name, change := range map[string]func(string) string{
		"different checked value": func(source string) string {
			return strings.Replace(source, "issubclass(plugin,", "issubclass(name,", 1)
		},
		"ignored result": func(source string) string {
			return strings.Replace(source, "    if not issubclass(plugin, Contract):\n        raise TypeError\n", "    issubclass(plugin, Contract)\n", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, graphRunner := externalArchitectureFixture(t, false)
			changeExternalLoader(t, &repo, change)
			assertExternalArchitectureFailure(t, AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner))
		})
	}
	repo, graphRunner := externalArchitectureFixture(t, false)
	changeExternalLoader(t, &repo, func(source string) string {
		return strings.Replace(source, "    return plugin", "    resolve_name(name)\n    return plugin", 1)
	})
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner)
	if analysis.Graph != nil || !slices.ContainsFunc(analysis.Findings, func(f policy.Finding) bool { return f.Check == "architecture.importCoverage" }) {
		t.Fatalf("declaration covered an unrelated sibling load: %+v", analysis)
	}
}

func TestPythonExternalPluginRequiresOneDeclarationAndCurrentDirectLock(t *testing.T) {
	for name, change := range map[string]func(*testing.T, *repository.Repository){
		"no declaration": func(_ *testing.T, repo *repository.Repository) { repo.Config.Scope.PythonExternalPluginImports = nil },
		"duplicate": func(_ *testing.T, repo *repository.Repository) {
			repo.Config.Scope.PythonExternalPluginImports = append(repo.Config.Scope.PythonExternalPluginImports, repo.Config.Scope.PythonExternalPluginImports[0])
		},
		"transitive only": func(t *testing.T, repo *repository.Repository) {
			data, err := repo.Read("pyproject.toml")
			if err != nil {
				t.Fatal(err)
			}
			writeArchitectureFile(t, repo.Root, "pyproject.toml", strings.Replace(string(data), "plug-dist==1.0", "other==1.0", 1))
		},
		"missing lock": func(t *testing.T, repo *repository.Repository) {
			if err := os.Remove(filepath.Join(repo.Root, "uv.lock")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, graphRunner := externalArchitectureFixture(t, false)
			change(t, &repo)
			assertExternalArchitectureFailure(t, AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner))
		})
	}
}

func TestPythonExternalPluginSelectionRetainsOnlyItsProject(t *testing.T) {
	repo, _ := externalArchitectureFixture(t, true)
	for _, selected := range [][]string{nil, {"AGENTS.md"}, {"README.md"}, {"other.go"}} {
		if commands := PythonGraphCommands(repo, selected); len(commands) != 0 {
			t.Fatalf("unrelated selection started plug-in analysis: %+v", commands)
		}
	}
	for _, selected := range [][]string{{"src/app/loader.py"}, {"src/app/registry.json"}, {"uv.lock"}, {"pyproject.toml"}, {policy.ConfigFilename}} {
		if commands := PythonGraphCommands(repo, selected); len(commands) != 1 {
			t.Fatalf("selected plug-in input did not select its project: %v, %+v", selected, commands)
		}
	}
}

func externalArchitectureFixture(t *testing.T, registry bool) (repository.Repository, *pythonGraphRunner) {
	t.Helper()
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"src/app/**"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = 'example'\nversion = '0'\nrequires-python = '==3.12.*'\ndependencies = ['plug-dist==1.0']\n")
	writeArchitectureFile(t, repo.Root, "uv.lock", "version = 1\n[[package]]\nname = 'plug-dist'\nversion = '1.0.0'\nsource = {registry = 'https://pypi.org/simple'}\n")
	writeArchitectureFile(t, repo.Root, "src/app/contracts.py", "from typing import Protocol, runtime_checkable\n@runtime_checkable\nclass Contract(Protocol):\n    def run(self):\n        ...\n")
	argument, imports, line := "'third_party.plugins:Plugin'", "from pkgutil import resolve_name\nfrom app.contracts import Contract\n", 4
	var configuration *policy.PythonComputedImportInput
	if registry {
		argument = "json.loads(Path('src/app/registry.json').read_text(encoding='utf-8'))['plugins'][name]"
		imports += "import json\nfrom pathlib import Path\n"
		line = 6
		content := `{"plugins":{"chosen":"third_party.plugins:Plugin"}}`
		writeArchitectureFile(t, repo.Root, "src/app/registry.json", content)
		digest := sha256.Sum256([]byte(content))
		configuration = &policy.PythonComputedImportInput{Path: "src/app/registry.json", JSONPointer: "/plugins", SHA256: hex.EncodeToString(digest[:])}
	}
	source := imports + "def load(name):\n    plugin = resolve_name(" + argument + ")\n    if not issubclass(plugin, Contract):\n        raise TypeError\n    return plugin\n"
	writeArchitectureFile(t, repo.Root, "src/app/loader.py", source)
	digest := sha256.Sum256([]byte(source))
	repo.Config.Scope.PythonExternalPluginImports = []policy.PythonExternalPluginImport{{Project: "pyproject.toml", Distribution: "plug-dist", Namespace: "third_party.plugins", InputGrammar: "python-module-object/v1", Configuration: configuration, Consumer: policy.PythonDynamicConsumer{Kind: "callsite", Importer: "src/app/loader.py", Module: "app.loader", Callable: "load", Site: policy.PythonSourceLocation{Line: line, Column: 14}, Callee: "pkgutil.resolve_name", Shape: "module-object-call/v1", Argument: argument, SourceSHA256: hex.EncodeToString(digest[:])}, Check: policy.PythonRuntimeCheck{Kind: "issubclass", Protocol: "app.contracts.Contract", Site: policy.PythonSourceLocation{Line: line + 1, Column: 12}}}}
	return repo, &pythonGraphRunner{outputs: map[string]string{".": `{"src/app/loader.py":["src/app/contracts.py"],"src/app/contracts.py":[]}`}}
}

func changeExternalLoader(t *testing.T, repo *repository.Repository, change func(string) string) {
	t.Helper()
	data, err := repo.Read("src/app/loader.py")
	if err != nil {
		t.Fatal(err)
	}
	source := change(string(data))
	writeArchitectureFile(t, repo.Root, "src/app/loader.py", source)
	digest := sha256.Sum256([]byte(source))
	repo.Config.Scope.PythonExternalPluginImports[0].Consumer.SourceSHA256 = hex.EncodeToString(digest[:])
}

func assertExternalArchitectureFailure(t *testing.T, analysis Analysis) {
	t.Helper()
	if analysis.Graph != nil || !slices.ContainsFunc(analysis.Findings, func(f policy.Finding) bool { return f.Check == "policy.pythonExternalPluginImport" }) {
		t.Fatalf("invalid external contract acquired architecture evidence: %+v", analysis)
	}
}
