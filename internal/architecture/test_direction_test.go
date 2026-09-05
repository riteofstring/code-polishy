package architecture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestGoTestImportsDoNotCreateProductionDependencyEdges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeArchitectureTestFile(t, root, "go.mod", "module example.com/project\n")
	writeArchitectureTestFile(t, root, "a/a.go", "package a\nimport _ \"example.com/project/b\"\n")
	writeArchitectureTestFile(t, root, "a/a_test.go", "package a\nimport _ \"example.com/project/b\"\n")
	writeArchitectureTestFile(t, root, "b/b.go", "package b\n")
	config := policy.Config{
		Modules:      []policy.Module{{Name: "a", Paths: []string{"a/**"}}, {Name: "b", Paths: []string{"b/**"}}},
		ModuleByName: map[string]int{"a": 0, "b": 1},
		Tests:        policy.Testing{Ownership: []policy.TestOwnership{{Paths: []string{"a/a_test.go"}, Module: "b", FocusedSuite: "b-unit"}}},
	}
	repo := repository.Repository{Root: root, Config: config}
	if findings := Check(t.Context(), repo, []string{"a/a.go"}); len(findings) != 1 || findings[0].Check != "architecture.moduleDependency" {
		t.Fatalf("production findings = %+v", findings)
	}
	if findings := Check(t.Context(), repo, []string{"a/a_test.go"}); len(findings) != 0 {
		t.Fatalf("test findings = %+v", findings)
	}
}

func TestJavaScriptAndPythonTestsDoNotCreateProductionDependencyEdges(t *testing.T) {
	t.Parallel()
	config := policy.Config{
		Modules:      []policy.Module{{Name: "a", Paths: []string{"a/**"}}, {Name: "b", Paths: []string{"b/**"}}},
		ModuleByName: map[string]int{"a": 0, "b": 1},
	}
	repo := repository.Repository{Config: config}
	governed := map[string]bool{"b/value.ts": true}
	productionFact := javascript.ImportFact{Path: "a/value.ts", Resolved: "b/value.ts", Line: 1}
	testFact := javascript.ImportFact{Path: "a/value.test.ts", Resolved: "b/value.ts", Line: 1}
	if _, found := javascriptImportFinding(repo, governed, productionFact); !found {
		t.Fatal("production JavaScript dependency was not reported")
	}
	if _, found := javascriptImportFinding(repo, governed, testFact); found {
		t.Fatal("test JavaScript dependency was added to the production graph")
	}
	if findings := pythonSourceModuleDependencyFindings(repo, "a/value.py", map[string]bool{"b/value.py": true}); len(findings) != 1 {
		t.Fatalf("production Python findings = %+v", findings)
	}
	if findings := pythonSourceModuleDependencyFindings(repo, "a/value_test.py", map[string]bool{"b/value.py": true}); len(findings) != 0 {
		t.Fatalf("test Python findings = %+v", findings)
	}
}

func writeArchitectureTestFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
