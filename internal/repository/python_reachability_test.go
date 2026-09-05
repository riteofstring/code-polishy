package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestPythonReachabilitySnapshotsCurrentGovernedRegistry(t *testing.T) {
	t.Parallel()
	repo, project := reachabilityRepository(t)
	first := repo.PythonReachabilityInputs(project)
	if len(first) != 1 || first[0].Error != "" || first[0].Registry != `{"plugins":["loader:load"]}` {
		t.Fatalf("current registry was not captured: %+v", first)
	}
	writeFile(t, repo.Root, "src/registry.json", `{"plugins":["loader:other"]}`)
	second := repo.PythonReachabilityInputs(project)
	if len(second) != 1 || second[0].Error != "" || second[0].Registry == first[0].Registry || second[0].ID != first[0].ID {
		t.Fatalf("registry refresh did not retain declaration with current input: %+v", second)
	}
}

func TestPythonReachabilityRejectsUngovernedRegistryInputs(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*Repository, *PythonProject){
		"excluded":  func(repo *Repository, _ *PythonProject) { repo.Config.Scope.Exclude = []string{"src/registry.json"} },
		"generated": func(repo *Repository, _ *PythonProject) { repo.Config.Scope.Generated = []string{"src/registry.json"} },
		"missing owner": func(repo *Repository, _ *PythonProject) {
			repo.Config.Modules[0].Paths = []string{"src/**/*.py"}
		},
		"ambiguous owner": func(repo *Repository, _ *PythonProject) {
			repo.Config.Modules = append(repo.Config.Modules, policy.Module{Name: "other", Paths: []string{"src/**"}})
		},
		"foreign project": func(repo *Repository, _ *PythonProject) {
			writeFile(t, repo.Root, "src/pyproject.toml", "[project]\nname = 'nested'\n")
		},
		"missing": func(repo *Repository, _ *PythonProject) {
			repo.Config.Scope.PythonDynamicReferences[0].Registry.Path = "src/missing.json"
		},
		"directory": func(repo *Repository, _ *PythonProject) {
			repo.Config.Scope.PythonDynamicReferences[0].Registry.Path = "src"
		},
		"escaping": func(repo *Repository, _ *PythonProject) {
			repo.Config.Scope.PythonDynamicReferences[0].Registry.Path = "../registry.json"
		},
		"empty": func(repo *Repository, _ *PythonProject) { writeFile(t, repo.Root, "src/registry.json", "") },
		"oversized": func(repo *Repository, _ *PythonProject) {
			writeFile(t, repo.Root, "src/registry.json", strings.Repeat("x", (2<<20)+1))
		},
		"foreign consumer": func(_ *Repository, project *PythonProject) { project.Files = nil },
		"wrong module": func(repo *Repository, _ *PythonProject) {
			repo.Config.Scope.PythonDynamicReferences[0].Consumer.Module = "other"
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, project := reachabilityRepository(t)
			change(&repo, &project)
			inputs := repo.PythonReachabilityInputs(project)
			if len(inputs) != 1 || inputs[0].Error == "" || inputs[0].Registry != "" {
				t.Fatalf("ungoverned registry acquired consumer input: %+v", inputs)
			}
		})
	}
}

func TestPythonReachabilityRejectsRegistrySymlinkSubstitution(t *testing.T) {
	t.Parallel()
	for _, parent := range []bool{false, true} {
		repo, project := reachabilityRepository(t)
		outside := t.TempDir()
		writeFile(t, outside, "registry.json", `{"plugins":["private:target"]}`)
		target, link, path := filepath.Join(outside, "registry.json"), "src/alias.json", "src/alias.json"
		if parent {
			target, link, path = outside, "src/linked", "src/linked/registry.json"
		}
		if err := os.Symlink(target, filepath.Join(repo.Root, filepath.FromSlash(link))); err != nil {
			t.Fatal(err)
		}
		repo.Config.Scope.PythonDynamicReferences[0].Registry.Path = path
		inputs := repo.PythonReachabilityInputs(project)
		if len(inputs) != 1 || inputs[0].Error == "" || inputs[0].Registry != "" {
			t.Fatalf("symlink imported an unowned registry: %+v", inputs)
		}
	}
}

func reachabilityRepository(t *testing.T) (Repository, PythonProject) {
	t.Helper()
	repo := Repository{Root: t.TempDir(), Config: policy.Config{Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}}}}
	writeFile(t, repo.Root, "pyproject.toml", "[project]\nname = 'application'\n")
	writeFile(t, repo.Root, "src/loader.py", "def load():\n    return 1\n")
	writeFile(t, repo.Root, "src/registry.json", `{"plugins":["loader:load"]}`)
	project := PythonProject{Manifest: "pyproject.toml", Root: ".", SourceRoots: []string{"src"}, Files: []string{"src/loader.py"}}
	repo.Config.Scope.PythonDynamicReferences = []policy.PythonDynamicReference{{Kind: "registry", Project: project.Manifest,
		Registry: &policy.PythonDynamicRegistry{Path: "src/registry.json", JSONPointer: "/plugins"},
		Consumer: policy.PythonDynamicConsumer{Kind: "callsite", Importer: "src/loader.py", Module: "loader"},
	}}
	return repo, project
}
