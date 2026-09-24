package repository

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestRustConfigurationChangesExpandSelection(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"Cargo.toml", "Cargo.lock", ".cargo/config", "crates/graphics/.cargo/config.toml",
		"rust-toolchain", "crates/graphics/rust-toolchain.toml", "rustfmt.toml", ".rustfmt.toml",
		"clippy.toml", "crates/graphics/.clippy.toml",
	} {
		t.Run(path, func(t *testing.T) {
			repo := newGitRepository(t)
			writeFile(t, repo.Root, path, "old settings\n")
			writeFile(t, repo.Root, "src/lib.rs", "pub fn value() -> u32 { 1 }\n")
			writeFile(t, repo.Root, "shaders/main.wgsl", "@compute @workgroup_size(1) fn main() {}\n")
			git(t, repo.Root, "add", ".")
			git(t, repo.Root, "commit", "-m", "base")
			writeFile(t, repo.Root, path, "new settings\n")
			for _, mode := range []string{"changes", "staged"} {
				git(t, repo.Root, "add", path)
				selection, err := repo.Select(mode, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !selection.All || !selection.PolicySensitive || !slices.Equal(selection.Candidate.AddedOrModified, []string{path}) ||
					!slices.Contains(selection.Files, "src/lib.rs") || !slices.Contains(selection.Files, "shaders/main.wgsl") {
					t.Fatalf("%s selection = %+v", mode, selection)
				}
			}
			if err := os.Remove(filepath.Join(repo.Root, filepath.FromSlash(path))); err != nil {
				t.Fatal(err)
			}
			selection, err := repo.Select("changes", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !selection.PolicySensitive || !slices.Equal(selection.Candidate.Deleted, []string{path}) || !slices.Contains(selection.Files, "src/lib.rs") {
				t.Fatalf("deleted configuration selection = %+v", selection)
			}
		})
	}
}

func TestWGSLSourceRetainsOwnershipAndChangedImpact(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config.Modules = []policy.Module{
		{Name: "shaders", Paths: []string{"shaders/**"}},
		{Name: "renderer", Paths: []string{"src/**"}, DependsOn: []string{"shaders"}},
	}
	writeFile(t, repo.Root, "src/lib.rs", "pub fn value() -> u32 { 1 }\n")
	writeFile(t, repo.Root, "shaders/main.WGSL", "@compute @workgroup_size(1) fn main() {}\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	writeFile(t, repo.Root, "shaders/main.WGSL", "@compute @workgroup_size(2) fn main() {}\n")
	selection, err := repo.Select("changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if selection.All || selection.PolicySensitive || !slices.Equal(selection.Files, []string{"shaders/main.WGSL"}) {
		t.Fatalf("shader selection = %+v", selection)
	}
	path := selection.Files[0]
	if repo.Language(path) != "wgsl" || !repo.IsExecutableSource(path) || repo.SourceCommentLanguage(path) != "wgsl" {
		t.Fatal("shader was not classified as governed WGSL source")
	}
	impact := repo.CandidateImpact(selection.Candidate)
	if !slices.Equal(impact.DirectModules, []string{"shaders"}) || !slices.Equal(impact.ImpactedModules, []string{"renderer", "shaders"}) {
		t.Fatalf("shader impact = %+v", impact)
	}
}

func TestRustInputsCannotBeDeclaredData(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"crates/app/.cargo/config.toml", "crates/app/.clippy.toml", "shaders/main.wgsl"} {
		t.Run(path, func(t *testing.T) {
			repo := Repository{Root: t.TempDir(), Config: policy.Config{Scope: policy.Scope{Data: []string{path}}}}
			writeFile(t, repo.Root, path, "input\n")
			if repo.IsData(path) {
				t.Fatal("executable or control input was classified as data")
			}
			if _, err := repo.AllFiles(); err == nil || !strings.Contains(err.Error(), "scope.data must not classify") {
				t.Fatalf("inventory error = %v", err)
			}
		})
	}
}
