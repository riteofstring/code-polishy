package repository

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWorktreeRootsIncludeEveryExistingWorkingCopy(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "src/app.go", "package app\n")
	git(t, repo.Root, "add", "-A")
	git(t, repo.Root, "commit", "-q", "-m", "start")
	parent := t.TempDir()
	linked := filepath.Join(parent, "linked")
	removed := filepath.Join(parent, "removed")
	git(t, repo.Root, "worktree", "add", "-q", "-b", "linked", linked)
	git(t, repo.Root, "worktree", "add", "-q", "-b", "removed", removed)
	if err := os.RemoveAll(removed); err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		t.Fatal(err)
	}
	resolvedLinked, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}

	roots, err := repo.WorktreeRoots()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{resolvedRoot, resolvedLinked}
	slices.Sort(want)
	if !slices.Equal(roots, want) {
		t.Fatalf("roots = %v, want %v", roots, want)
	}
}
