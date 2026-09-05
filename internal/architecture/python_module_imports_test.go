package architecture

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/architecture/sourcegraph"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/pythonfacts"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestPythonReexportedLoadersProduceCurrentGraphEdgesWithPinnedRuff(t *testing.T) {
	for _, kind := range []string{"proven-dynamic", "type-only"} {
		t.Run(kind, func(t *testing.T) {
			repo := pythonArchitectureRepository(t, []policy.Module{
				{Name: "plugins", Paths: []string{"src/app/plugins/**"}},
				{Name: "application", Paths: []string{"src/app/loader.py", "src/app/exports.py"}},
			})
			policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
			if err != nil {
				t.Fatal(err)
			}
			repo.PolicyRoot = policyRoot
			writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nrequires-python = \"==3.12.*\"\n")
			source := "from app.exports import load, CHECK\n"
			if kind == "type-only" {
				source += "if CHECK:\n    load((\"app.plugins.first\",)[choice])\n"
			} else {
				source += "load((\"app.plugins.first\",)[choice])\n"
			}
			inputs := []pythonfacts.Input{
				{Path: "src/app/loader.py", Source: source},
				{Path: "src/app/exports.py", Source: "from importlib import import_module as load\nfrom typing import TYPE_CHECKING as CHECK\n"},
				{Path: "src/app/plugins/first.py", Source: "value = 1\n"},
			}
			facts := map[string]pythonSourceFact{}
			for _, input := range inputs {
				writeArchitectureFile(t, repo.Root, input.Path, input.Source)
			}
			project, err := pythonfacts.AnalyzeProjectSources(t.Context(), repo.PythonTool(), inputs)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range project.Sources {
				facts[input.Path] = pythonSourceFact{SHA256: input.SHA256}
			}
			pythonResolveTestImports(t, project.Sources, facts)
			fact := facts[inputs[0].Path]
			if len(fact.ComputedImports) != 1 {
				t.Fatalf("reexported loader = %+v", fact.ComputedImports)
			}
			computed := fact.ComputedImports[0]
			repo.Config.Scope.PythonComputedImports = []policy.PythonComputedImport{{
				Project: "pyproject.toml", Importer: inputs[0].Path, Module: "app.loader", ModuleScope: true,
				Callee: computed.Callee, Line: computed.Line, Column: computed.Column, Shape: computed.Shape, Argument: computed.Argument,
				SourceSHA256: fact.SHA256, Namespace: "app.plugins", Targets: []string{"app.plugins.first"},
			}}
			analysis := AnalyzeWithRunner(t.Context(), repo, []string{inputs[0].Path}, runner.OSRunner{})
			if len(analysis.Findings) != 1 || analysis.Findings[0].Check != "architecture.moduleDependency" {
				t.Fatalf("reexported loader lost module policy: %+v", analysis.Findings)
			}
			assertPythonModuleImportEdge(t, analysis.Graph, inputs[0].Path, inputs[2].Path, kind)
			repo.Config.Modules[1].DependsOn = []string{"plugins"}
			if findings := Check(t.Context(), repo, []string{inputs[0].Path}); len(findings) != 0 {
				t.Fatalf("authorized computed edge = %+v", findings)
			}
			writeArchitectureFile(t, repo.Root, inputs[1].Path, "from vendor import load\nfrom typing import TYPE_CHECKING as CHECK\n")
			findings := Check(t.Context(), repo, []string{inputs[0].Path})
			if len(findings) != 1 || findings[0].Check != "architecture.importCoverage" || !strings.Contains(findings[0].Message, "stale") {
				t.Fatalf("changed reexport retained stale declaration: %+v", findings)
			}
		})
	}
}

func assertPythonModuleImportEdge(t *testing.T, graph *sourcegraph.Graph, source, target, kind string) {
	t.Helper()
	if graph == nil {
		t.Fatal("resolved loader omitted its graph")
	}
	for _, edge := range graph.Edges {
		if edge.Source == source && edge.Target == target && string(edge.Kind) == kind {
			return
		}
	}
	t.Fatalf("resolved loader edge missing: %+v", graph.Edges)
}
