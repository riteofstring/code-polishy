package repository

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestContainedDirectoryReadsOnlyBoundedDirectEntries(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "cache/one", "one")
	writeFile(t, repo.Root, "cache/nested/two", "two")
	entries, err := repo.ReadContainedDirectory("cache", 2)
	if err != nil || len(entries) != 2 {
		t.Fatalf("contained entries = %+v, %v", entries, err)
	}
	names := []string{entries[0].Name(), entries[1].Name()}
	slices.Sort(names)
	if !slices.Equal(names, []string{"nested", "one"}) {
		t.Fatalf("directory read expanded or omitted children: %v", names)
	}
	for _, limit := range []int{0, 1} {
		if _, err := repo.ReadContainedDirectory("cache", limit); err == nil {
			t.Fatalf("entry limit %d was ignored", limit)
		}
	}
	if _, err := repo.ReadContainedDirectory("cache/one", 2); err == nil {
		t.Fatal("a file was accepted as a directory")
	}
	if _, err := repo.ReadContainedFile("cache", 1024); err == nil {
		t.Fatal("a directory was accepted as a regular file")
	}
}

func TestContainedDirectoryRejectsNoncanonicalAndLinkedParents(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	writeFile(t, repo.Root, "cache/nested/one", "one")
	if err := os.Symlink("cache", filepath.Join(repo.Root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(repo.Root, "outside")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../cache", "cache/../cache", "cache\\nested", "linked", "linked/nested", "outside"} {
		if _, err := repo.ReadContainedDirectory(path, 2); err == nil {
			t.Fatalf("unsafe directory %q was read", path)
		}
	}
}
