package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestPythonObjectLoadsEnforceModuleDirectionIndependentlyOfReachability(t *testing.T) {
	repo, graphRunner := objectArchitectureFixture(t, false)
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{Kind: "target", Project: "pyproject.toml", Target: &policy.PythonDynamicTarget{Module: "app.plugins.first", Symbol: "Handler"}}}
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner)
	if len(analysis.Findings) != 1 || analysis.Findings[0].Check != "architecture.moduleDependency" || analysis.Findings[0].Subject != "plugins" {
		t.Fatalf("reachability altered architecture permission: %+v", analysis.Findings)
	}
	if analysis.Graph == nil || len(analysis.Graph.Edges) != 1 || analysis.Graph.Edges[0].Kind != "proven-dynamic" || analysis.Graph.Edges[0].Target != "src/app/plugins/first.py" {
		t.Fatalf("literal object loader omitted its module edge: %+v", analysis.Graph)
	}
	repo.Config.Modules[1].DependsOn = []string{"plugins"}
	if findings := CheckWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner); len(findings) != 0 {
		t.Fatalf("declared module direction failed: %+v", findings)
	}
}

func TestPythonRegistryObjectLoadsRequireIndependentCurrentArchitectureEvidence(t *testing.T) {
	repo, graphRunner := objectArchitectureFixture(t, true)
	repo.Config.Modules[1].DependsOn = []string{"plugins"}
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner)
	if len(analysis.Findings) != 0 || analysis.Graph == nil || len(analysis.Graph.Edges) != 1 {
		t.Fatalf("exact registry architecture evidence failed: %+v", analysis)
	}
	firstResolution := analysis.Graph.Inputs[0].ResolutionSHA256
	content := `{"plugins":{"default":"app.plugins.first:Handler"}}` + "\n"
	writeArchitectureFile(t, repo.Root, "src/app/registry.json", content)
	stale := CheckWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner)
	if !slices.ContainsFunc(stale, func(finding policy.Finding) bool {
		return finding.Check == "architecture.importCoverage" && strings.Contains(finding.Message, "digest is stale")
	}) {
		t.Fatalf("changed registry passed with stale evidence: %+v", stale)
	}
	digest := sha256.Sum256([]byte(content))
	repo.Config.Scope.PythonComputedImports[0].Configuration[0].SHA256 = hex.EncodeToString(digest[:])
	refreshed := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner)
	if len(refreshed.Findings) != 0 || refreshed.Graph == nil || refreshed.Graph.Inputs[0].ResolutionSHA256 == firstResolution {
		t.Fatalf("registry refresh did not invalidate graph inputs: %+v", refreshed)
	}
	repo.Config.Scope.PythonComputedImports = nil
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{Kind: "registry", Project: "pyproject.toml", Registry: &policy.PythonDynamicRegistry{Path: "src/app/registry.json", JSONPointer: "/plugins"}}}
	if findings := CheckWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner); !slices.ContainsFunc(findings, func(finding policy.Finding) bool { return finding.Check == "architecture.importCoverage" }) {
		t.Fatalf("dead-code registry substituted for architecture evidence: %+v", findings)
	}
}

func TestPythonObjectArchitectureRejectsDisconnectedRegistryContracts(t *testing.T) {
	for name, mutate := range map[string]func(*policy.PythonComputedImport){
		"source":              func(declaration *policy.PythonComputedImport) { declaration.SourceSHA256 = strings.Repeat("a", 64) },
		"site":                func(declaration *policy.PythonComputedImport) { declaration.Line++ },
		"callable":            func(declaration *policy.PythonComputedImport) { declaration.Callable = "another" },
		"argument":            func(declaration *policy.PythonComputedImport) { declaration.Argument = "unrelated" },
		"shape":               func(declaration *policy.PythonComputedImport) { declaration.Shape = "unknown" },
		"namespace":           func(declaration *policy.PythonComputedImport) { declaration.Namespace = "app.other" },
		"selector":            func(declaration *policy.PythonComputedImport) { declaration.Configuration[0].JSONPointer = "/other" },
		"duplicate inventory": func(declaration *policy.PythonComputedImport) { declaration.Targets = []string{"app.plugins.first"} },
	} {
		t.Run(name, func(t *testing.T) {
			repo, graphRunner := objectArchitectureFixture(t, true)
			mutate(&repo.Config.Scope.PythonComputedImports[0])
			analysis := AnalyzeWithRunner(t.Context(), repo, []string{"src/app/loader.py"}, graphRunner)
			if !slices.ContainsFunc(analysis.Findings, func(finding policy.Finding) bool { return finding.Check == "architecture.importCoverage" }) || analysis.Graph != nil {
				t.Fatalf("disconnected object import contract acquired graph evidence: %+v", analysis)
			}
		})
	}
}

func TestPythonObjectArchitectureScopesDeclaredConsumersToSelectedInputs(t *testing.T) {
	repo, _ := objectArchitectureFixture(t, true)
	for _, selected := range [][]string{nil, {"AGENTS.md"}, {"README.md"}, {"src/other.go"}} {
		if commands := PythonGraphCommands(repo, selected); len(commands) != 0 {
			t.Fatalf("unrelated selection %v started Python architecture work: %+v", selected, commands)
		}
	}
	for _, selected := range [][]string{{"src/app/registry.json"}, {"src/app/loader.py"}, {"pyproject.toml"}, {policy.ConfigFilename}} {
		if commands := PythonGraphCommands(repo, selected); len(commands) != 1 {
			t.Fatalf("selected object import input %v did not select its project: %+v", selected, commands)
		}
	}
}

func objectArchitectureFixture(t *testing.T, registry bool) (repository.Repository, *pythonGraphRunner) {
	t.Helper()
	repo := pythonArchitectureRepository(t, []policy.Module{
		{Name: "plugins", Paths: []string{"src/app/plugins/**"}},
		{Name: "application", Paths: []string{"src/app/loader.py", "src/app/registry.json"}},
	})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = 'example'\nversion = '0'\nrequires-python = '==3.12.*'\n")
	writeArchitectureFile(t, repo.Root, "src/app/plugins/first.py", "class Handler:\n    pass\n")
	source := "from pkgutil import resolve_name\ndef load():\n    return resolve_name('app.plugins.first:Handler')\n"
	if registry {
		argument := "json.loads(Path('src/app/registry.json').read_text(encoding='utf-8'))['plugins'][name]"
		source = "from pkgutil import resolve_name\nimport json\nfrom pathlib import Path\ndef load(name):\n    return resolve_name(" + argument + ")\n"
		content := `{"plugins":{"default":"app.plugins.first:Handler"}}`
		writeArchitectureFile(t, repo.Root, "src/app/registry.json", content)
		sourceDigest, registryDigest := sha256.Sum256([]byte(source)), sha256.Sum256([]byte(content))
		repo.Config.Scope.PythonComputedImports = []policy.PythonComputedImport{{Project: "pyproject.toml", Importer: "src/app/loader.py", Module: "app.loader", Callable: "load", Callee: "pkgutil.resolve_name", Line: 5, Column: 12, Shape: "module-object-call/v1", Argument: argument, SourceSHA256: hex.EncodeToString(sourceDigest[:]), Namespace: "app.plugins", Configuration: []policy.PythonComputedImportInput{{Path: "src/app/registry.json", JSONPointer: "/plugins", SHA256: hex.EncodeToString(registryDigest[:])}}}}
	}
	writeArchitectureFile(t, repo.Root, "src/app/loader.py", source)
	return repo, &pythonGraphRunner{outputs: map[string]string{".": `{"src/app/loader.py":[]}`}}
}
