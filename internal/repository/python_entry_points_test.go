package repository

import (
	"slices"
	"strings"
	"testing"
)

func TestParsePythonProjectCollectsPEP621DynamicReferences(t *testing.T) {
	t.Parallel()
	project, err := ParsePythonProject("apps/api/pyproject.toml", []byte(`[project]
name = "example"

[project.scripts]
example-cli = "example.cli:main [cli-extra, other.extra]"

[project.gui-scripts]
example-gui = "example.gui:Application.run"

[project.entry-points.example.plugins]
render = "example.plugins.render:render"
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []PythonDynamicReference{
		{Module: "example.plugins.render", Symbol: "render", Table: "project.entry-points.example.plugins", Name: "render", Line: 11},
		{Module: "example.gui", Symbol: "Application.run", Table: "project.gui-scripts", Name: "example-gui", Line: 8},
		{Module: "example.cli", Symbol: "main", Table: "project.scripts", Name: "example-cli", Line: 5},
	}
	if !slices.Equal(project.DynamicReferences, want) {
		t.Fatalf("dynamic references = %+v", project.DynamicReferences)
	}
}

func TestParsePythonProjectCollectsEquivalentPEP621DynamicReferenceForms(t *testing.T) {
	t.Parallel()
	want := []PythonDynamicReference{
		{Module: "example.plugins.render", Symbol: "render", Table: "project.entry-points.plugins", Name: "render"},
		{Module: "example.gui", Symbol: "Application.run", Table: "project.gui-scripts", Name: "example-gui"},
		{Module: "example.cli", Symbol: "main", Table: "project.scripts", Name: "example-cli"},
	}
	cases := map[string]struct {
		manifest string
		lines    []int
	}{
		"project-relative dotted keys": {
			manifest: `[project]
scripts.example-cli = "example.cli:main"
gui-scripts.example-gui = "example.gui:Application.run"
entry-points.plugins.render = "example.plugins.render:render"
`,
			lines: []int{4, 3, 2},
		},
		"inline project fields": {
			manifest: `[project]
scripts = { example-cli = "example.cli:main" }
gui-scripts = { example-gui = "example.gui:Application.run" }
entry-points = { plugins = { render = "example.plugins.render:render" } }
`,
			lines: []int{4, 3, 2},
		},
		"root dotted keys": {
			manifest: `project.scripts.example-cli = "example.cli:main"
project.gui-scripts.example-gui = "example.gui:Application.run"
project.entry-points.plugins.render = "example.plugins.render:render"
`,
			lines: []int{3, 2, 1},
		},
		"root inline project": {
			manifest: `project = { scripts = { example-cli = "example.cli:main" }, gui-scripts = { example-gui = "example.gui:Application.run" }, entry-points = { plugins = { render = "example.plugins.render:render" } } }
`,
			lines: []int{1, 1, 1},
		},
		"quoted semantic segments": {
			manifest: `[project]
"scripts".example-cli = "example.cli:main"
"gui-scripts".example-gui = "example.gui:Application.run"
"entry-points".plugins.render = "example.plugins.render:render"
`,
			lines: []int{4, 3, 2},
		},
	}
	for name, testCase := range cases {
		name, testCase := name, testCase
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			project, err := ParsePythonProject("pyproject.toml", []byte(testCase.manifest))
			if err != nil {
				t.Fatal(err)
			}
			if !project.HasProjectTable {
				t.Fatalf("project table was not recognized: %+v", project)
			}
			got := slices.Clone(project.DynamicReferences)
			for index := range got {
				got[index].Line = 0
			}
			if !slices.Equal(got, want) {
				t.Fatalf("dynamic references = %+v", project.DynamicReferences)
			}
			for index, line := range testCase.lines {
				if project.DynamicReferences[index].Line != line {
					t.Fatalf("reference %d line = %d, want %d", index, project.DynamicReferences[index].Line, line)
				}
			}
		})
	}
}

func TestParsePythonProjectKeepsQuotedDynamicReferenceKeySegmentsExact(t *testing.T) {
	t.Parallel()
	project, err := ParsePythonProject("pyproject.toml", []byte(`[project]
"scripts.cli" = "example.cli:main"
`))
	if err != nil {
		t.Fatal(err)
	}
	if !project.HasProjectTable || len(project.DynamicReferences) != 0 {
		t.Fatalf("project = %+v", project)
	}
}

func TestParsePythonProjectRejectsInvalidEntryPointReferences(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"array table":          "[[project.scripts]]\nexample = \"example.cli:main\"\n",
		"non-string value":     "[project.scripts]\nexample = [\"example.cli:main\"]\n",
		"missing symbol":       "[project.scripts]\nexample = \"example.cli\"\n",
		"wildcard module":      "[project.scripts]\nexample = \"example.*:main\"\n",
		"empty extras":         "[project.scripts]\nexample = \"example.cli:main []\"\n",
		"duplicate extras":     "[project.scripts]\nexample = \"example.cli:main [cli, CLI]\"\n",
		"trailing content":     "[project.scripts]\nexample = \"example.cli:main [cli] later\"\n",
		"empty member chain":   "[project.entry-points.plugins]\nexample = \"example.cli:main..run\"\n",
		"inline non-string":    "[project]\nscripts = { example = [\"example.cli:main\"] }\n",
		"nested script":        "[project]\nscripts = { example = { main = \"example.cli:main\" } }\n",
		"non-string group":     "[project]\nentry-points = { plugins = \"example.cli:main\" }\n",
		"equivalent duplicate": "project.scripts.example = \"example.cli:main\"\n[project]\nscripts.example = \"example.cli:main\"\n",
	}
	for name, manifest := range cases {
		name, manifest := name, manifest
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParsePythonProject("pyproject.toml", []byte(manifest)); err == nil {
				t.Fatal("invalid entry point was accepted")
			}
		})
	}
}

func TestPythonProjectDynamicReferencesAreCopiedFromInventoryCache(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "pyproject.toml", `[project]
name = "example"

[project.scripts]
example = "example.cli:main"
`)
	writeFile(t, root, "src/example/cli.py", "")
	repo, err := (Repository{Root: root}).WithPythonProjectInventory([]string{"pyproject.toml", "src/example/cli.py"})
	if err != nil {
		t.Fatal(err)
	}
	inventory := repo.PythonProjectInventory([]string{"pyproject.toml", "src/example/cli.py"})
	if len(inventory.Projects) != 1 || len(inventory.Projects[0].DynamicReferences) != 1 {
		t.Fatalf("inventory = %+v", inventory)
	}
	inventory.Projects[0].DynamicReferences[0].Symbol = "changed"
	fresh, err := repo.ReadPythonProject("pyproject.toml")
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.DynamicReferences) != 1 || fresh.DynamicReferences[0].Symbol != "main" {
		t.Fatalf("fresh project = %+v", fresh)
	}
	if strings.Join([]string{fresh.DynamicReferences[0].Module, fresh.DynamicReferences[0].Table, fresh.DynamicReferences[0].Name}, ":") != "example.cli:project.scripts:example" {
		t.Fatalf("fresh reference = %+v", fresh.DynamicReferences[0])
	}
}
