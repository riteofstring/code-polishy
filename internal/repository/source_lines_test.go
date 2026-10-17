package repository

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestCountSourceLinesCountsHandwrittenNonBlankLinesByScope(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config = policy.Config{
		Scope:   policy.Scope{Generated: []string{"gen/**"}},
		Quality: policy.Quality{LineBudget: &policy.LineBudget{Production: 3, Total: 10}},
	}
	writeFile(t, repo.Root, "src/app.go", "package app\n\n\nfunc Run() {}\n  \t\nvar x = 1\n")
	writeFile(t, repo.Root, "src/app_test.go", "package app\n\nfunc TestRun() {}\n")
	writeFile(t, repo.Root, "scripts/tool.py", "print('a')\nprint('b')")
	writeFile(t, repo.Root, "gen/model.go", "package gen\nvar a = 1\n")
	writeFile(t, repo.Root, "docs/guide.md", "# Guide\ntext\n")
	writeFile(t, repo.Root, "config/settings.json", "{\n}\n")
	writeFile(t, repo.Root, "go.sum", "module v1 h1:x\n")

	lines, err := repo.CountSourceLines()
	if err != nil {
		t.Fatal(err)
	}
	if lines.Production != 5 || lines.Test != 2 {
		t.Fatalf("lines = %+v, want production 5 and test 2", lines)
	}
	want := []SourceLineGroup{{Name: "go", Production: 3, Test: 2}, {Name: "python", Production: 2}}
	if !slices.Equal(lines.Languages, want) {
		t.Fatalf("languages = %+v, want %+v", lines.Languages, want)
	}
	wantBudget := []LineBudgetScope{{Scope: "production", Lines: 5, Budget: 3}, {Scope: "total", Lines: 7, Budget: 10}}
	if !slices.Equal(lines.Budget, wantBudget) {
		t.Fatalf("budget = %+v, want %+v", lines.Budget, wantBudget)
	}
	if !lines.Budget[0].Exceeded() || lines.Budget[1].Exceeded() {
		t.Fatalf("exceeded scopes = %+v", lines.Budget)
	}
}

func TestCountSourceLinesWithoutBudgetReportsNoScopes(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "src/app.go", "package app\n")

	lines, err := repo.CountSourceLines()
	if err != nil {
		t.Fatal(err)
	}
	if lines.Production != 1 || len(lines.Budget) != 0 {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestCountSourceLinesCountsTrackedSourceOutsideSparseCheckout(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config.Quality.LineBudget = &policy.LineBudget{Production: 1}
	writeFile(t, repo.Root, "src/app.go", "package app\n")
	writeFile(t, repo.Root, "src/omitted.go", "package app\n\nfunc A() {}\nfunc B() {}\n")
	writeFile(t, repo.Root, "tools/release", "#!/bin/sh\necho release\n")
	writeFile(t, repo.Root, "jobs/run.sbatch.template", "#!/bin/bash\nsrun job\n")
	writeFile(t, repo.Root, "tools/stale.go", "package tools\nfunc A() {}\nfunc B() {}\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "source")
	git(t, repo.Root, "update-index", "--skip-worktree", "src/omitted.go", "tools/release", "jobs/run.sbatch.template", "tools/stale.go")
	for _, path := range []string{"src/omitted.go", "tools/release", "jobs/run.sbatch.template"} {
		if err := os.Remove(filepath.Join(repo.Root, filepath.FromSlash(path))); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, repo.Root, "tools/stale.go", "package tools\n")

	lines, err := repo.CountSourceLines()
	if err != nil {
		t.Fatal(err)
	}
	want := []SourceLineGroup{{Name: "go", Production: 7}, {Name: "shell", Production: 4}}
	if lines.Production != 11 || !slices.Equal(lines.Languages, want) {
		t.Fatalf("lines = %+v, want production 11 in %+v", lines, want)
	}
	if !lines.Budget[0].Exceeded() {
		t.Fatalf("budget = %+v, want exceeded", lines.Budget)
	}
}

func TestCountSourceLinesReadsOnlySourceFromPartialSparseClone(t *testing.T) {
	t.Parallel()
	origin := newGitRepository(t)
	writeFile(t, origin.Root, "src/app.go", "package app\n")
	writeFile(t, origin.Root, "lib/omitted.go", "package lib\nfunc A() {}\n")
	writeFile(t, origin.Root, "lib/data.json", "{}\n")
	writeFile(t, origin.Root, "lib/guide.md", "# Guide\n")
	git(t, origin.Root, "add", ".")
	git(t, origin.Root, "commit", "-m", "source")
	git(t, origin.Root, "config", "uploadpack.allowFilter", "true")
	clone := filepath.Join(t.TempDir(), "clone")
	git(t, origin.Root, "clone", "--quiet", "--filter=blob:none", "--no-checkout", "file://"+filepath.ToSlash(origin.Root), clone)
	git(t, clone, "sparse-checkout", "set", "src")
	git(t, clone, "checkout", "--quiet", "main")
	git(t, clone, "cat-file", "blob", "HEAD:lib/omitted.go")
	git(t, clone, "remote", "set-url", "origin", "file://"+filepath.ToSlash(filepath.Join(t.TempDir(), "offline")))
	repo := Repository{Root: clone}

	lines, err := repo.CountSourceLines()
	if err != nil {
		t.Fatal(err)
	}
	if lines.Production != 3 {
		t.Fatalf("lines = %+v, want production 3", lines)
	}
}
