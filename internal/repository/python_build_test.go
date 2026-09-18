package repository

import (
	"slices"
	"strings"
	"testing"
)

func TestPythonProjectInventoryUsesInTreeBackendAndNestedSrcRoots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "pyproject.toml", `[build-system]
requires = []
build-backend = "sample_build_backend"
backend-path = ["packages/sample-runtime"]

[project]
name = "sample"
requires-python = ">=3.12"
dependencies = []
`)
	files := []string{
		"packages/sample-runtime/sample_build_backend.py",
		"packages/sample-runtime/src/sample/__init__.py",
		"packages/sample-runtime/src/sample/runtime.py",
		"packages/sample-runtime/tests/test_runtime.py",
	}
	for _, path := range files {
		writeFile(t, root, path, "")
	}
	inventory := (Repository{Root: root}).PythonProjectInventory(append([]string{"pyproject.toml"}, files...))
	if len(inventory.Problems) != 0 || len(inventory.Projects) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	project := inventory.Projects[0]
	if project.BuildBackend.Module != "sample_build_backend" || project.BuildBackend.Object != "" ||
		!slices.Equal(project.SourceRoots, []string{".", "packages/sample-runtime", "packages/sample-runtime/src"}) {
		t.Fatalf("project = %+v", project)
	}
	wantModules := map[string]string{
		"packages/sample-runtime/sample_build_backend.py": "sample_build_backend",
		"packages/sample-runtime/src/sample/__init__.py":  "sample",
		"packages/sample-runtime/src/sample/runtime.py":   "sample.runtime",
		"packages/sample-runtime/tests/test_runtime.py":   "tests.test_runtime",
	}
	for path, want := range wantModules {
		module, _ := PythonModuleName(project, path)
		if module != want {
			t.Fatalf("module for %s = %q, want %q", path, module, want)
		}
	}
}

func TestPythonProjectInventoryReportsOneUnsupportedLayoutProblem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "pyproject.toml", "[project]\nrequires-python = \"==3.12.*\"\ndependencies = []\n")
	writeFile(t, root, "packages/sample-runtime/src/sample/first.py", "")
	writeFile(t, root, "packages/sample-runtime/src/sample/second.py", "")
	inventory := (Repository{Root: root}).PythonProjectInventory([]string{
		"pyproject.toml", "packages/sample-runtime/src/sample/first.py", "packages/sample-runtime/src/sample/second.py",
	})
	if len(inventory.Problems) != 1 || inventory.Problems[0].Kind != PythonUnsupportedLayoutProblem ||
		inventory.Problems[0].Path != "pyproject.toml" || !strings.Contains(inventory.Problems[0].Message, "build-system.backend-path root") {
		t.Fatalf("problems = %+v", inventory.Problems)
	}
}

func TestPythonProjectRejectsInvalidOrMissingBackendPaths(t *testing.T) {
	t.Parallel()
	t.Run("invalid", func(t *testing.T) {
		_, err := ParsePythonProject("pyproject.toml", []byte("[build-system]\nbuild-backend = \"backend\"\nbackend-path = [\"../outside\"]\n"))
		if err == nil || !strings.Contains(err.Error(), "normalized relative directory paths") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "pyproject.toml", "[build-system]\nbuild-backend = \"backend\"\nbackend-path = [\"backend\"]\n")
		inventory := (Repository{Root: root}).PythonProjectInventory([]string{"pyproject.toml"})
		if len(inventory.Problems) != 1 || inventory.Problems[0].Kind != PythonUnsupportedLayoutProblem ||
			!strings.Contains(inventory.Problems[0].Message, "does not exist") {
			t.Fatalf("problems = %+v", inventory.Problems)
		}
	})
}
