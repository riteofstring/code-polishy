package repository

import (
	"slices"
	"strings"
	"testing"
)

func TestPythonRuffTargetVersionUsesMinimumSupportedMinor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		requiresPython string
		want           string
	}{
		{requiresPython: "==3.12.*", want: "py312"},
		{requiresPython: ">=3.11", want: "py311"},
		{requiresPython: ">3.11.4", want: "py311"},
		{requiresPython: "~=3.10.2", want: "py310"},
		{requiresPython: ">=3.12, !=3.12.*", want: "py313"},
		{requiresPython: ">=3.15", want: "py315"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.requiresPython, func(t *testing.T) {
			t.Parallel()
			got, err := pythonRuffTargetVersion(test.requiresPython)
			if err != nil || got != test.want {
				t.Fatalf("target = %q, error = %v", got, err)
			}
		})
	}
}

func TestPythonRuffTargetVersionRejectsIndeterminateOrUnsupportedRanges(t *testing.T) {
	t.Parallel()
	for _, requiresPython := range []string{"", "<3.12", ">=3.6", ">=3.16", ">=3.12rc1", ">=3.12, <3.12"} {
		requiresPython := requiresPython
		t.Run(requiresPython, func(t *testing.T) {
			t.Parallel()
			if target, err := pythonRuffTargetVersion(requiresPython); err == nil {
				t.Fatalf("target %q was accepted", target)
			}
		})
	}
}

func TestPythonRuffSettingsOwnTargetRootsAndLineLength(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "pyproject.toml", `[project]
requires-python = "==3.12.*"

[tool.ruff]
line-length = 88

[tool.ruff.lint.pycodestyle]
max-line-length = 88
`)
	writeFile(t, root, "src/example/app.py", "value = 1\n")
	inventory := (Repository{Root: root}).PythonProjectInventory([]string{"pyproject.toml", "src/example/app.py"})
	if len(inventory.Problems) != 0 || len(inventory.Projects) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	project := inventory.Projects[0]
	if project.Ruff.RequiresPython != "==3.12.*" || project.Ruff.TargetVersion != "py312" ||
		!slices.Equal(project.SourceRoots, []string{".", "src"}) {
		t.Fatalf("Ruff settings = %+v", project.Ruff)
	}
	want := []string{
		"--target-version", "py312",
		"--config", "line-length = 88",
		"--config", "lint.pycodestyle.max-line-length = 88",
		"--config", `src = [".", "src"]`,
	}
	options, err := project.Ruff.CommandOptions(project.Root, project.SourceRoots)
	if err != nil || !slices.Equal(options, want) {
		t.Fatalf("options = %v, error = %v", options, err)
	}
}

func TestPythonRuffConfigurationRejectsMissingTargetAndConflictingLineLengths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		manifest   string
		ruffConfig string
		message    string
	}{
		{name: "missing target", manifest: "[project]\nname = \"example\"\n", message: "project.requires-python is required"},
		{name: "pyproject conflict", manifest: "[project]\nrequires-python = \"==3.12.*\"\n\n[tool.ruff]\nline-length = 100\n", message: "policy-owned line length 88"},
		{name: "Ruff file conflict", manifest: "[project]\nrequires-python = \"==3.12.*\"\n", ruffConfig: "line-length = 100\n", message: "policy-owned line length 88"},
		{name: "lint conflict", manifest: "[project]\nrequires-python = \"==3.12.*\"\n", ruffConfig: "[lint.pycodestyle]\nmax-line-length = 100\n", message: "policy-owned line length 88"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, root, "pyproject.toml", test.manifest)
			writeFile(t, root, "app.py", "value = 1\n")
			files := []string{"pyproject.toml", "app.py"}
			if test.ruffConfig != "" {
				writeFile(t, root, "ruff.toml", test.ruffConfig)
				files = append(files, "ruff.toml")
			}
			inventory := (Repository{Root: root}).PythonProjectInventory(files)
			if len(inventory.Problems) != 1 || inventory.Problems[0].Kind != PythonRuffConfigurationProblem ||
				!strings.Contains(inventory.Problems[0].Message, test.message) {
				t.Fatalf("problems = %+v", inventory.Problems)
			}
		})
	}
}
