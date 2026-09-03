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
build-backend = "setta_build_backend"
backend-path = ["packages/setta-runtime"]

[project]
name = "setta"
requires-python = ">=3.12"
dependencies = []
`)
	files := []string{
		"packages/setta-runtime/setta_build_backend.py",
		"packages/setta-runtime/src/setta/__init__.py",
		"packages/setta-runtime/src/setta/runtime.py",
		"packages/setta-runtime/tests/test_runtime.py",
	}
	for _, path := range files {
		writeFile(t, root, path, "")
	}
	inventory := (Repository{Root: root}).PythonProjectInventory(append([]string{"pyproject.toml"}, files...))
	if len(inventory.Problems) != 0 || len(inventory.Projects) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	project := inventory.Projects[0]
	if project.BuildBackend.Module != "setta_build_backend" || project.BuildBackend.Object != "" ||
		!slices.Equal(project.SourceRoots, []string{".", "packages/setta-runtime", "packages/setta-runtime/src"}) {
		t.Fatalf("project = %+v", project)
	}
	wantModules := map[string]string{
		"packages/setta-runtime/setta_build_backend.py": "setta_build_backend",
		"packages/setta-runtime/src/setta/__init__.py":  "setta",
		"packages/setta-runtime/src/setta/runtime.py":   "setta.runtime",
		"packages/setta-runtime/tests/test_runtime.py":  "tests.test_runtime",
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
	writeFile(t, root, "packages/setta-runtime/src/setta/first.py", "")
	writeFile(t, root, "packages/setta-runtime/src/setta/second.py", "")
	inventory := (Repository{Root: root}).PythonProjectInventory([]string{
		"pyproject.toml", "packages/setta-runtime/src/setta/first.py", "packages/setta-runtime/src/setta/second.py",
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
