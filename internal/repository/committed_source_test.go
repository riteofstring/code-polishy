package repository

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegularFileAtLimitUsesCommittedContentAndEnforcesByteLimit(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	expected := "package app\n\nfunc Value() int { return 2 }\n"
	writeFile(t, repo.Root, "app/value.go", expected)
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	candidate, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, "app/value.go", "uncommitted replacement")
	data, present, err := repo.ReadRegularFileAtLimit(candidate, "app/value.go", len(expected))
	if err != nil || !present || string(data) != expected {
		t.Fatalf("committed source = %q, %v, %v", data, present, err)
	}
	if data, _, err := repo.ReadRegularFileAtLimit(candidate, "app/value.go", len(expected)-1); err == nil || data != nil {
		t.Fatalf("oversized source returned partial evidence: %q, %v", data, err)
	}
}

func TestReadRegularFileAtLimitRejectsNonRegularAndUnboundedInputs(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "app/value.go", "package app\n")
	if err := os.Symlink("app/value.go", filepath.Join(repo.Root, "linked.go")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	candidate, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"linked.go", "app", "../outside.go"} {
		if _, _, err := repo.ReadRegularFileAtLimit(candidate, path, 128); err == nil {
			t.Fatalf("invalid committed source accepted: %s", path)
		}
	}
	if data, present, err := repo.ReadRegularFileAtLimit(candidate, "missing.go", 128); err != nil || present || data != nil {
		t.Fatalf("missing committed source = %q, %v, %v", data, present, err)
	}
	if _, _, err := repo.ReadRegularFileAtLimit("main", "app/value.go", 128); err == nil {
		t.Fatal("mutable revision accepted")
	}
	if _, _, err := repo.ReadRegularFileAtLimit(candidate, "app/value.go", 0); err == nil {
		t.Fatal("unbounded committed read accepted")
	}
}
