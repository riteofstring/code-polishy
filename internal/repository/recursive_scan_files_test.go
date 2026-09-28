package repository

import (
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

func TestRecursiveScanFilesHonorGitIgnoreButNotPolicyExcludes(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, ".gitignore", "ignored/\n")
	writeFile(t, repo.Root, "dist/Cargo.lock", "")
	writeFile(t, repo.Root, "ignored/Cargo.lock", "")
	writeFile(t, repo.Root, ".code-polishy-reports/package-lock.json", "{}\n")
	git(t, repo.Root, "add", "-f", ".gitignore", "dist/Cargo.lock", ".code-polishy-reports/package-lock.json")
	files, err := repo.RecursiveScanFiles()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(files, "dist/Cargo.lock") {
		t.Fatalf("recursive scan omitted tracked generated input: %v", files)
	}
	for _, excluded := range []string{"ignored/Cargo.lock", ".code-polishy-reports/package-lock.json"} {
		if slices.Contains(files, excluded) {
			t.Fatalf("recursive scan included %s: %v", excluded, files)
		}
	}
}
