package repository

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanHeadRequiresOneCleanCommittedCandidate(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*testing.T, Repository){
		"untracked": func(t *testing.T, repo Repository) {
			writeFile(t, repo.Root, "untracked.txt", "new\n")
		},
		"unstaged": func(t *testing.T, repo Repository) {
			writeFile(t, repo.Root, "tracked.txt", "changed\n")
		},
		"staged": func(t *testing.T, repo Repository) {
			writeFile(t, repo.Root, "tracked.txt", "changed\n")
			git(t, repo.Root, "add", "tracked.txt")
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo, _ := committedRepository(t)
			change(t, repo)
			_, err := repo.CleanHead()
			if !errors.Is(err, ErrDirtyCandidate) {
				t.Fatalf("CleanHead() error = %v, want dirty candidate", err)
			}
			var typed *GitError
			if !errors.As(err, &typed) || typed.Kind != GitErrorDirtyCandidate {
				t.Fatalf("CleanHead() typed error = %#v", typed)
			}
		})
	}
}

func TestCleanHeadReturnsAnExactCommittedHeadAndIgnoresReports(t *testing.T) {
	t.Parallel()
	repo, want := committedRepository(t)
	writeFile(t, repo.Root, ".code-polishy-reports/behavior-review/receipt.json", "{}\n")
	got, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	if got != want || !exactRevision(got) {
		t.Fatalf("CleanHead() = %q, want exact %q", got, want)
	}
}

func TestCleanHeadRejectsARepositoryWithoutACommit(t *testing.T) {
	t.Parallel()
	_, err := newGitRepository(t).CleanHead()
	if !errors.Is(err, ErrNoHead) {
		t.Fatalf("CleanHead() error = %v, want no head", err)
	}
}

func TestResolveAncestorRequiresACleanCandidateAndExactAncestor(t *testing.T) {
	t.Parallel()
	repo, base := committedRepository(t)
	git(t, repo.Root, "branch", "other")
	git(t, repo.Root, "switch", "-c", "feature")
	writeFile(t, repo.Root, "feature.txt", "feature\n")
	git(t, repo.Root, "add", "feature.txt")
	git(t, repo.Root, "commit", "-m", "feature")

	ancestor, err := repo.ResolveAncestor("main")
	if err != nil || ancestor != base {
		t.Fatalf("ResolveAncestor(main) = %q, %v; want %q", ancestor, err, base)
	}
	if _, err := repo.ResolveAncestor("--all"); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("ResolveAncestor(--all) error = %v, want invalid reference", err)
	}

	git(t, repo.Root, "switch", "other")
	writeFile(t, repo.Root, "other.txt", "other\n")
	git(t, repo.Root, "add", "other.txt")
	git(t, repo.Root, "commit", "-m", "other")
	git(t, repo.Root, "switch", "feature")
	if _, err := repo.ResolveAncestor("other"); !errors.Is(err, ErrNotAncestor) {
		t.Fatalf("ResolveAncestor(other) error = %v, want non-ancestor", err)
	}

	writeFile(t, repo.Root, "untracked.txt", "new\n")
	if _, err := repo.ResolveAncestor("main"); !errors.Is(err, ErrDirtyCandidate) {
		t.Fatalf("ResolveAncestor(main) with candidate change = %v, want dirty candidate", err)
	}
}

func TestIsAncestorUsesExactRevisionsAndReportsDivergence(t *testing.T) {
	t.Parallel()
	repo, base := committedRepository(t)
	git(t, repo.Root, "branch", "other")
	git(t, repo.Root, "switch", "-c", "feature")
	writeFile(t, repo.Root, "feature.txt", "feature\n")
	git(t, repo.Root, "add", "feature.txt")
	git(t, repo.Root, "commit", "-m", "feature")
	feature, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	git(t, repo.Root, "switch", "other")
	writeFile(t, repo.Root, "other.txt", "other\n")
	git(t, repo.Root, "add", "other.txt")
	git(t, repo.Root, "commit", "-m", "other")
	other, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		ancestor, descendant string
		want                 bool
	}{
		"equal":       {ancestor: base, descendant: base, want: true},
		"ancestor":    {ancestor: base, descendant: feature, want: true},
		"nonancestor": {ancestor: other, descendant: feature, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			got, queryErr := repo.IsAncestor(test.ancestor, test.descendant)
			if queryErr != nil || got != test.want {
				t.Fatalf("IsAncestor(%s, %s) = %t, %v; want %t, nil", test.ancestor, test.descendant, got, queryErr, test.want)
			}
		})
	}

	if _, err := repo.IsAncestor("main", feature); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("IsAncestor(main, %s) error = %v, want invalid reference", feature, err)
	}
}

func TestAncestryResultReportsUnexpectedGitFailuresAsOperational(t *testing.T) {
	t.Parallel()
	if _, err := ancestryResult(errors.New("git unavailable")); !errors.Is(err, ErrGitOperation) {
		t.Fatalf("ancestryResult() error = %v, want operational Git error", err)
	}
}

func TestGitFailuresIncludeNativeDiagnostics(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	_, err := repo.gitOutput(context.Background(), "rev-parse", "--verify", "--end-of-options", "refs/code-polishy/missing")
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("gitOutput() error = %v, want Git exit error", err)
	}
	diagnostic := bytes.TrimSpace(exitError.Stderr)
	if len(diagnostic) == 0 || !bytes.Contains([]byte(err.Error()), diagnostic) {
		t.Fatalf("gitOutput() error omitted native diagnostic %q: %v", diagnostic, err)
	}
}

func TestPatchProducesBinaryEvidenceForExactRevisions(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	baseBinary := []byte{0, 1, 2, 3, 4, 5}
	if err := os.WriteFile(filepath.Join(repo.Root, "binary.bin"), baseBinary, 0o600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, "other.txt", "base\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	base, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.Root, "binary.bin"), []byte{5, 4, 3, 2, 1, 0}, 0o600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.Root, "other.txt", "candidate\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "candidate")
	candidate, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}

	binaryPatch, err := repo.Patch(base, candidate, []string{"binary.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(binaryPatch, []byte("GIT binary patch")) || bytes.Contains(binaryPatch, []byte("other.txt")) {
		t.Fatalf("binary patch = %q", binaryPatch)
	}
	allPatch, err := repo.Patch(base, candidate, nil)
	if err != nil || !bytes.Contains(allPatch, []byte("other.txt")) {
		t.Fatalf("all-path patch contains other.txt = %t, err = %v", bytes.Contains(allPatch, []byte("other.txt")), err)
	}
	if _, err := repo.Patch("main", candidate, nil); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("Patch() moving base error = %v, want invalid reference", err)
	}
	if _, err := repo.Patch(base, candidate, []string{"../outside"}); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Patch() escaped path error = %v, want invalid path", err)
	}
}

func TestDetachedWorktreeLifecycleRejectsUnsafeTargets(t *testing.T) {
	t.Parallel()
	repo, head := committedRepository(t)
	parent := t.TempDir()
	worktree := filepath.Join(parent, "baseline")
	if err := repo.AddDetachedWorktree(context.Background(), worktree, head); err != nil {
		t.Fatal(err)
	}
	if !isRegularFile(filepath.Join(worktree, ".git")) {
		t.Fatalf("detached worktree is missing its Git pointer: %s", worktree)
	}
	command := exec.Command("git", "-C", worktree, "symbolic-ref", "--quiet", "HEAD")
	if err := command.Run(); err == nil {
		t.Fatal("detached worktree has a branch HEAD")
	}
	writeFile(t, worktree, "untracked.txt", "temporary\n")
	if err := repo.RemoveWorktree(context.Background(), worktree); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(worktree); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed worktree exists: %v", err)
	}
	if err := repo.RemoveWorktree(context.Background(), worktree); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("second removal error = %v, want invalid path", err)
	}

	invalidRevisionPath := filepath.Join(t.TempDir(), "invalid-revision")
	if err := repo.AddDetachedWorktree(context.Background(), invalidRevisionPath, "main"); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("AddDetachedWorktree() moving revision error = %v, want invalid reference", err)
	}
	if _, err := os.Lstat(invalidRevisionPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid-revision worktree exists: %v", err)
	}
	if err := repo.AddDetachedWorktree(context.Background(), t.TempDir(), head); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("AddDetachedWorktree() existing path error = %v, want invalid path", err)
	}
	if err := repo.RemoveWorktree(context.Background(), repo.Root); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("RemoveWorktree(repository root) error = %v, want invalid path", err)
	}
}

func TestDetachedWorktreeSupportsLongHostPaths(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	relative := filepath.Join("nested", strings.Repeat("x", 80)+".txt")
	writeFile(t, repo.Root, relative, "long path\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "long path")
	head, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	for len(filepath.Join(parent, "worktree")) < 220 {
		parent = filepath.Join(parent, "nested-path")
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(parent, "worktree")
	if len(filepath.Join(worktree, relative)) <= 260 {
		t.Fatalf("test worktree path is too short: %s", worktree)
	}
	if err := repo.AddDetachedWorktree(context.Background(), worktree, head); err != nil {
		t.Fatal(err)
	}
	if !isRegularFile(filepath.Join(worktree, relative)) {
		t.Fatalf("long-path worktree is missing %s", relative)
	}
	if err := repo.RemoveWorktree(context.Background(), worktree); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveWorktreeRefusesARegisteredBranchWorkspace(t *testing.T) {
	t.Parallel()
	repo, _ := committedRepository(t)
	git(t, repo.Root, "branch", "topic")
	worktree := filepath.Join(t.TempDir(), "topic")
	git(t, repo.Root, "worktree", "add", "--quiet", worktree, "topic")
	if err := repo.RemoveWorktree(context.Background(), worktree); !errors.Is(err, ErrInvalidPath) || !strings.Contains(err.Error(), "detached") {
		t.Fatalf("RemoveWorktree(branch workspace) error = %v, want detached-worktree rejection", err)
	}
	git(t, repo.Root, "worktree", "remove", "--force", worktree)
}

func committedRepository(t *testing.T) (Repository, string) {
	t.Helper()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "tracked.txt", "base\n")
	git(t, repo.Root, "add", ".")
	git(t, repo.Root, "commit", "-m", "base")
	head, err := repo.CleanHead()
	if err != nil {
		t.Fatal(err)
	}
	return repo, head
}
