package repository

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestRecursiveScanFilesPreserveScannerVisibleGeneratedInputs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "src/main.go", "package main\n")
	writeFile(t, root, "dist/Cargo.lock", "")
	writeFile(t, root, "build/inventory.cdx.json", "{}\n")
	writeFile(t, root, ".code-polishy-reports/package-lock.json", "{}\n")
	repo := Repository{Root: root}
	files, err := repo.RecursiveScanFiles()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"build/inventory.cdx.json", "dist/Cargo.lock", "src/main.go"}
	if !slices.Equal(files, want) {
		t.Fatalf("recursive scan files = %v, want %v", files, want)
	}
}

func TestRecursiveScanFilesHonorOnlyScannerGitIgnoreAndNotPolicyExcludes(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, ".gitignore", "ignored/\n")
	writeFile(t, repo.Root, ".git/info/exclude", "info-excluded/\n")
	writeFile(t, repo.Root, "global-ignore", "global-excluded/\n")
	writeFile(t, repo.Root, "dist/Cargo.lock", "")
	writeFile(t, repo.Root, "ignored/Cargo.lock", "")
	writeFile(t, repo.Root, "info-excluded/go.mod", "module example.test/info\n")
	writeFile(t, repo.Root, "global-excluded/Cargo.lock", "")
	writeFile(t, repo.Root, ".code-polishy-reports/package-lock.json", "{}\n")
	git(t, repo.Root, "config", "core.excludesFile", filepath.Join(repo.Root, "global-ignore"))
	git(t, repo.Root, "add", "-f", ".gitignore", "dist/Cargo.lock", ".code-polishy-reports/package-lock.json")
	files, err := repo.RecursiveScanFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, included := range []string{"dist/Cargo.lock", "global-excluded/Cargo.lock", "info-excluded/go.mod"} {
		if !slices.Contains(files, included) {
			t.Fatalf("recursive scan omitted %s: %v", included, files)
		}
	}
	for _, excluded := range []string{"ignored/Cargo.lock", ".code-polishy-reports/package-lock.json"} {
		if slices.Contains(files, excluded) {
			t.Fatalf("recursive scan included %s: %v", excluded, files)
		}
	}
}

func TestRecursiveScanFilesDescendNestedRepositoriesAndGitlinks(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	for _, name := range []string{"nested", "gitlink"} {
		root := filepath.Join(repo.Root, name)
		writeFile(t, root, "Cargo.lock", "")
		git(t, root, "init", "-b", "main")
		git(t, root, "config", "user.email", "tests@example.test")
		git(t, root, "config", "user.name", "Code Polishy Tests")
		git(t, root, "add", "Cargo.lock")
		git(t, root, "commit", "-m", "fixture")
	}
	git(t, repo.Root, "add", "gitlink")
	files, err := repo.RecursiveScanFiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, included := range []string{"gitlink/Cargo.lock", "nested/Cargo.lock"} {
		if !slices.Contains(files, included) {
			t.Fatalf("recursive scan omitted %s: %v", included, files)
		}
	}
}
