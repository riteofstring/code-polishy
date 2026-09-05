package repository

import (
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestDynamicControlInputsCannotBeClassifiedByScope(t *testing.T) {
	t.Parallel()
	for name, scope := range map[string]policy.Scope{
		"data":      {Data: []string{"ci/includes/*.yml"}},
		"generated": {Generated: []string{"ci/includes/*.yml"}},
		"exclude":   {Exclude: []string{"ci/includes/*.yml"}},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, "ci/includes/release.yml", "image: registry.example/app@sha256:"+strings.Repeat("a", 64)+"\n")
			repo := Repository{Root: root, Config: policy.Config{Scope: scope}}
			configured, err := repo.WithDynamicControlInputs([]string{"ci/includes/release.yml"})
			if err != nil {
				t.Fatal(err)
			}
			if configured.IsData("ci/includes/release.yml") || configured.IsGenerated("ci/includes/release.yml") || configured.IsExcluded("ci/includes/release.yml") {
				t.Fatalf("dynamic GitLab include was classified by %s scope", name)
			}
			if _, err := configured.AllFiles(); err == nil || !strings.Contains(err.Error(), "policy-sensitive GitLab include ci/includes/release.yml") {
				t.Fatalf("AllFiles error = %v", err)
			}
		})
	}
}

func TestDynamicControlInputChangesExpandSelection(t *testing.T) {
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "ci/includes/release.yml", "image: registry.example/app@sha256:"+strings.Repeat("a", 64)+"\n")
	writeFile(t, repo.Root, "app/main.go", "package app\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	configured, err := repo.WithDynamicControlInputs([]string{"ci/includes/release.yml"})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, configured.Root, "ci/includes/release.yml", "image: registry.example/app@sha256:"+strings.Repeat("b", 64)+"\n")
	selection, err := configured.Select("changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !selection.All || !selection.PolicySensitive || !slices.Equal(selection.Candidate.AddedOrModified, []string{"ci/includes/release.yml"}) {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestDynamicControlInputsMustBeDistinctContainedPaths(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	for _, paths := range [][]string{{"../outside.yml"}, {"ci/./include.yml"}, {"ci/include.yml", "ci/include.yml"}} {
		if _, err := repo.WithDynamicControlInputs(paths); err == nil {
			t.Fatalf("WithDynamicControlInputs(%v) succeeded", paths)
		}
	}
}

func TestWorkflowRunnerConfigurationChangesExpandSelection(t *testing.T) {
	for _, path := range []string{".github/actionlint.yaml", ".github/actionlint.yml"} {
		t.Run(path, func(t *testing.T) {
			repo := newGitRepository(t)
			writeFile(t, repo.Root, path, "self-hosted-runner:\n  labels: [first]\n")
			writeFile(t, repo.Root, "app/main.go", "package app\n")
			git(t, repo.Root, "add", ".")
			git(t, repo.Root, "commit", "-m", "base")
			writeFile(t, repo.Root, path, "self-hosted-runner:\n  labels: [second]\n")
			selection, err := repo.Select("changes", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !selection.All || !selection.PolicySensitive || !slices.Contains(selection.Files, "app/main.go") {
				t.Fatalf("runner configuration did not expand selection: %+v", selection)
			}
		})
	}
}
