package architecture

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGoPackageCycleReportsRealFileWitnesses(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, true)
	repo.Config.Modules[0].DependsOn = []string{"api"}
	writeArchitectureFile(t, repo.Root, "internal/domain/domain.go", "package domain\nimport _ \"example.test/app/internal/api\"\n")
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"internal/api/api.go"}, &pythonGraphRunner{})
	finding := onlyCycleFinding(t, analysis.Findings, "architecture.fileCycle")
	if analysis.Graph == nil || finding.DependencyComponent == nil || len(finding.DependencyComponent.Members) != 2 || len(finding.DependencyComponent.Witness) != 2 {
		t.Fatalf("analysis = %+v", analysis)
	}
	for _, edge := range finding.DependencyComponent.Witness {
		if edge.Source != "internal/api/api.go" && edge.Source != "internal/domain/domain.go" {
			t.Fatalf("invented Go witness edge = %+v", edge)
		}
	}
}

func TestGoManifestSelectionRetainsPackageCycleContext(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, true)
	repo.Config.Modules[0].DependsOn = []string{"api"}
	writeArchitectureFile(t, repo.Root, "internal/domain/domain.go", "package domain\nimport _ \"example.test/app/internal/api\"\n")
	writeArchitectureFile(t, repo.Root, "go.work", "go 1.26.5\nuse .\n")
	for _, path := range []string{"go.mod", "go.work"} {
		analysis := AnalyzeWithRunner(t.Context(), repo, []string{path}, &pythonGraphRunner{})
		finding := onlyCycleFinding(t, analysis.Findings, "architecture.fileCycle")
		if len(finding.DependencyComponent.Members) != 2 || analysis.Graph == nil {
			t.Fatalf("manifest selection lost package context: %+v", analysis)
		}
	}
}

func TestGoExternalTestPackageImportsDoNotCreateSelfCycles(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, true)
	path := "internal/api/api_test.go"
	writeArchitectureFile(t, repo.Root, path, "package api_test\nimport _ \"example.test/app/internal/api\"\n")
	repo.Config.Tests.Ownership = []policy.TestOwnership{{Paths: []string{path}, Module: "domain", FocusedSuite: "domain-unit"}}
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{path}, &pythonGraphRunner{})
	if analysis.Graph == nil || len(analysis.Findings) != 0 {
		t.Fatalf("analysis = %+v", analysis)
	}
	found := false
	for _, edge := range analysis.Graph.Edges {
		if edge.Source == path {
			found = true
			if edge.SourceResolution == edge.TargetResolution {
				t.Fatalf("external test package collapsed into production: %+v", edge)
			}
		}
	}
	if !found {
		t.Fatal("external test import disappeared")
	}
	files, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	if findings := CoverageFindings(repo, files); len(findings) != 0 {
		t.Fatalf("explicit test owner changed production package ownership: %+v", findings)
	}
}

func TestAllowedModuleProjectionCannotHideJavaScriptSourceCycle(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, true)
	repo.Config.Modules[0].DependsOn = []string{"web"}
	repo.PolicyRoot = fakeImportBundle(t,
		importFactKind("web/app.ts", 3, "../domain/model.js", "domain/model.ts", "", "type-only"),
		importFactKind("domain/model.ts", 4, "../web/app.js", "web/app.ts", "", "re-export"))
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"web/app.ts"}, &pythonGraphRunner{})
	finding := onlyCycleFinding(t, analysis.Findings, "architecture.fileCycle")
	if analysis.Graph == nil || finding.DependencyComponent == nil || finding.DependencyComponent.Classification != "production" {
		t.Fatalf("analysis = %+v", analysis)
	}
	kinds := []string{finding.DependencyComponent.Edges[0].Kind, finding.DependencyComponent.Edges[1].Kind}
	slices.Sort(kinds)
	if !slices.Equal(kinds, []string{"re-export", "type-only"}) {
		t.Fatalf("edge kinds = %+v", kinds)
	}
}

func TestGeneratedAndTestOnlyJavaScriptCyclesRemainSeparate(t *testing.T) {
	t.Parallel()
	t.Run("generated", func(t *testing.T) {
		repo := javascriptRepository(t, true)
		repo.Config.Modules[0].DependsOn = []string{"web"}
		repo.Config.Scope.Generated = []string{"domain/**", "web/**"}
		repo.PolicyRoot = fakeImportBundle(t,
			importFactKind("web/app.ts", 1, "../domain/model.js", "domain/model.ts", "", "runtime"),
			importFactKind("domain/model.ts", 1, "../web/app.js", "web/app.ts", "", "proven-dynamic"))
		finding := onlyCycleFinding(t, Check(t.Context(), repo, []string{"web/app.ts"}), "architecture.fileCycle")
		if finding.Subject != "generated" || finding.DependencyComponent.Classification != "generated" {
			t.Fatalf("finding = %+v", finding)
		}
	})
	t.Run("test-only", func(t *testing.T) {
		repo := javascriptRepository(t, true)
		repo.Config.Modules[0].DependsOn = []string{"web"}
		repo.Config.Tests.Ownership = []policy.TestOwnership{
			{Paths: []string{"web/app.test.ts"}, Module: "web", FocusedSuite: "web-unit"},
			{Paths: []string{"domain/model.test.ts"}, Module: "domain", FocusedSuite: "domain-unit"},
		}
		writeArchitectureFile(t, repo.Root, "domain/model.test.ts", "export {};\n")
		writeArchitectureFile(t, repo.Root, "web/app.test.ts", "export {};\n")
		repo.PolicyRoot = fakeImportBundle(t,
			importFactKind("web/app.test.ts", 1, "../domain/model.test.js", "domain/model.test.ts", "", "runtime"),
			importFactKind("domain/model.test.ts", 1, "../web/app.test.js", "web/app.test.ts", "", "runtime"))
		finding := onlyCycleFinding(t, Check(t.Context(), repo, []string{"web/app.test.ts"}), "testing.fileCycle")
		if finding.Subject != "test-only" || finding.DependencyComponent.Classification != "test-only" {
			t.Fatalf("finding = %+v", finding)
		}
	})
}

func TestPythonCycleUsesCompleteSelectedProject(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "application", Paths: []string{"app/**"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"example\"\nversion = \"0\"\nrequires-python = \"==3.12.*\"\n")
	writeArchitectureFile(t, repo.Root, "app/__init__.py", "")
	writeArchitectureFile(t, repo.Root, "app/a.py", "import app.b\n")
	writeArchitectureFile(t, repo.Root, "app/b.py", "import app.a\n")
	graphRunner := &pythonGraphRunner{outputs: map[string]string{
		".": `{"app/a.py":["app/b.py"],"app/b.py":["app/a.py"]}`,
	}}
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"app/a.py"}, graphRunner)
	finding := onlyCycleFinding(t, analysis.Findings, "architecture.fileCycle")
	if analysis.Graph == nil || finding.DependencyComponent == nil || len(finding.DependencyComponent.Members) != 2 {
		t.Fatalf("analysis = %+v", analysis)
	}
	if len(analysis.Graph.Inputs) != 1 || !slices.Equal(analysis.Graph.Inputs[0].Paths, []string{"app/__init__.py", "app/a.py", "app/b.py"}) {
		t.Fatalf("selected project lost fact coverage: %+v", analysis.Graph.Inputs)
	}
	before, err := BuildReviewTopology(repo, *analysis.Graph)
	if err != nil {
		t.Fatal(err)
	}
	writeArchitectureFile(t, repo.Root, "app/b.py", "import app.a\nvalue = 2\n")
	updated := AnalyzeWithRunner(t.Context(), repo, []string{"app/a.py"}, graphRunner)
	updatedFinding := onlyCycleFinding(t, updated.Findings, "architecture.fileCycle")
	if updated.Graph == nil || updated.Graph.Identity == analysis.Graph.Identity || updatedFinding.DependencyComponent.Identity != finding.DependencyComponent.Identity {
		t.Fatalf("source change failed to invalidate evidence or changed semantic cycle: %+v", updated)
	}
	after, err := BuildReviewTopology(repo, *updated.Graph)
	if err != nil || before.Identity != after.Identity {
		t.Fatalf("unchanged topology lost review identity: %+v, %v", after, err)
	}
}

func TestUnownedJavaScriptTestsRetainResolvedImportsForDiagnostics(t *testing.T) {
	t.Parallel()
	repo := javascriptRepository(t, true)
	writeArchitectureFile(t, repo.Root, "checks/contract.test.ts", "import '../domain/model.js';\n")
	repo.PolicyRoot = fakeImportBundle(t, importFact("checks/contract.test.ts", 1, "../domain/model.js", "domain/model.ts", ""))
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"checks/contract.test.ts"}, &pythonGraphRunner{})
	if analysis.Graph != nil || !slices.Equal(analysis.TestImports["checks/contract.test.ts"], []string{"domain/model.ts"}) {
		t.Fatalf("analysis = %+v", analysis)
	}
	if len(analysis.Findings) != 1 || analysis.Findings[0].Check != "policy.testOwnership" {
		t.Fatalf("findings = %+v", analysis.Findings)
	}
}

func TestUnownedPythonTestsRetainResolvedImportsForDiagnostics(t *testing.T) {
	t.Parallel()
	repo := pythonArchitectureRepository(t, []policy.Module{{Name: "app", Paths: []string{"app/**"}}})
	writeArchitectureFile(t, repo.Root, "pyproject.toml", "[project]\nname = \"app\"\nversion = \"0.1.0\"\nrequires-python = \">=3.12\"\n")
	writeArchitectureFile(t, repo.Root, "app/value.py", "value = 1\n")
	writeArchitectureFile(t, repo.Root, "checks/test_contract.py", "from app import value\n")
	graphRunner := &pythonGraphRunner{outputs: map[string]string{".": `{"app/value.py":[],"checks/test_contract.py":["app/value.py"]}`}}
	analysis := AnalyzeWithRunner(t.Context(), repo, []string{"checks/test_contract.py"}, graphRunner)
	if analysis.Graph != nil || !slices.Equal(analysis.TestImports["checks/test_contract.py"], []string{"app/value.py"}) {
		t.Fatalf("analysis = %+v", analysis)
	}
	if len(analysis.Findings) != 1 || analysis.Findings[0].Check != "policy.testOwnership" {
		t.Fatalf("findings = %+v", analysis.Findings)
	}
}

func onlyCycleFinding(t *testing.T, findings []policy.Finding, check string) policy.Finding {
	t.Helper()
	if len(findings) != 1 || findings[0].Check != check || findings[0].DependencyComponent == nil {
		t.Fatalf("findings = %+v", findings)
	}
	return findings[0]
}

func importFactKind(path string, line int, specifier, resolved, imported, kind string) string {
	fact := importFact(path, line, specifier, resolved, imported)
	return fact[:len(fact)-len(`runtime"}`)] + kind + `"}`
}
