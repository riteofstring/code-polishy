package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type GitErrorKind string

const (
	GitErrorDirtyCandidate   GitErrorKind = "dirty-candidate"
	GitErrorInvalidPath      GitErrorKind = "invalid-path"
	GitErrorInvalidReference GitErrorKind = "invalid-reference"
	GitErrorNoHead           GitErrorKind = "no-head"
	GitErrorNotAncestor      GitErrorKind = "not-ancestor"
	GitErrorOperation        GitErrorKind = "operation"
)

var (
	ErrDirtyCandidate   = &GitError{Kind: GitErrorDirtyCandidate}
	ErrInvalidPath      = &GitError{Kind: GitErrorInvalidPath}
	ErrInvalidReference = &GitError{Kind: GitErrorInvalidReference}
	ErrNoHead           = &GitError{Kind: GitErrorNoHead}
	ErrNotAncestor      = &GitError{Kind: GitErrorNotAncestor}
	ErrGitOperation     = &GitError{Kind: GitErrorOperation}
)

type GitError struct {
	Kind      GitErrorKind
	Operation string
	Cause     error
}

type CandidateStateSnapshot struct {
	Head   string
	SHA256 string
	Dirty  bool
}

type candidateStateMaterial struct {
	Head      string                        `json:"head"`
	Staged    candidateStateDiff            `json:"staged"`
	Unstaged  candidateStateDiff            `json:"unstaged"`
	Untracked []candidateStateUntrackedFile `json:"untracked"`
}

type candidateStateDiff struct {
	Paths  []string `json:"paths"`
	SHA256 string   `json:"sha256"`
}

type candidateStateUntrackedFile struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Executable bool   `json:"executable"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
}

func (err *GitError) Error() string {
	message := string(err.Kind)
	if err.Operation != "" {
		message = err.Operation + ": " + message
	}
	if err.Cause != nil {
		message += ": " + err.Cause.Error()
	}
	return message
}

func (err *GitError) Unwrap() error {
	return err.Cause
}

func (err *GitError) Is(target error) bool {
	typed, ok := target.(*GitError)
	return ok && err.Kind == typed.Kind
}

func (repo Repository) CleanHead() (string, error) {
	head, err := repo.resolveCommit(context.Background(), "HEAD")
	if err != nil {
		return "", newGitError(GitErrorNoHead, "resolve clean HEAD", err)
	}
	changed, err := repo.candidateChanges(context.Background())
	if err != nil {
		return "", newGitError(GitErrorOperation, "inspect candidate", err)
	}
	if len(changed) > 0 {
		return "", newGitError(GitErrorDirtyCandidate, "inspect candidate", fmt.Errorf("changed paths: %s", strings.Join(changed, ", ")))
	}
	return head, nil
}

func (repo Repository) CandidateState() (CandidateStateSnapshot, error) {
	ctx := context.Background()
	head, err := repo.resolveCommit(ctx, "HEAD")
	if err != nil {
		return CandidateStateSnapshot{}, newGitError(GitErrorNoHead, "snapshot candidate", err)
	}
	staged, err := repo.candidateStateDiff(ctx, true)
	if err != nil {
		return CandidateStateSnapshot{}, err
	}
	unstaged, err := repo.candidateStateDiff(ctx, false)
	if err != nil {
		return CandidateStateSnapshot{}, err
	}
	untracked, err := repo.candidateStateUntracked(ctx)
	if err != nil {
		return CandidateStateSnapshot{}, err
	}
	material := candidateStateMaterial{Head: head, Staged: staged, Unstaged: unstaged, Untracked: untracked}
	data, err := json.Marshal(material)
	if err != nil {
		return CandidateStateSnapshot{}, newGitError(GitErrorOperation, "snapshot candidate", err)
	}
	dirty := len(staged.Paths) > 0 || len(unstaged.Paths) > 0 || len(untracked) > 0
	return CandidateStateSnapshot{Head: head, SHA256: digestBytes(data), Dirty: dirty}, nil
}

func (repo Repository) candidateStateDiff(ctx context.Context, staged bool) (candidateStateDiff, error) {
	nameArguments := []string{"diff", "--name-only", "-z", "--no-renames"}
	patchArguments := []string{"diff", "--binary", "--full-index", "--no-color", "--no-ext-diff", "--no-renames"}
	if staged {
		nameArguments = append(nameArguments, "--cached", "HEAD")
		patchArguments = append(patchArguments, "--cached", "HEAD")
	}
	nameArguments = append(nameArguments, "--")
	output, err := repo.gitOutput(ctx, nameArguments...)
	if err != nil {
		return candidateStateDiff{}, newGitError(GitErrorOperation, "snapshot candidate diff", err)
	}
	paths := uniqueSorted(repo.includedPaths(output))
	if len(paths) == 0 {
		return candidateStateDiff{Paths: []string{}, SHA256: digestBytes(nil)}, nil
	}
	patchArguments = append(patchArguments, "--")
	patchArguments = append(patchArguments, paths...)
	patch, err := repo.gitOutput(ctx, patchArguments...)
	if err != nil {
		return candidateStateDiff{}, newGitError(GitErrorOperation, "snapshot candidate diff", err)
	}
	return candidateStateDiff{Paths: paths, SHA256: digestBytes(patch)}, nil
}

func (repo Repository) candidateStateUntracked(ctx context.Context) ([]candidateStateUntrackedFile, error) {
	output, err := repo.gitOutput(ctx, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, newGitError(GitErrorOperation, "snapshot untracked files", err)
	}
	paths := uniqueSorted(repo.includedPaths(output))
	files := make([]candidateStateUntrackedFile, 0, len(paths))
	for _, path := range paths {
		file, err := repo.candidateStateUntrackedFile(path)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	return files, nil
}

func (repo Repository) candidateStateUntrackedFile(path string) (candidateStateUntrackedFile, error) {
	absolute := filepath.Join(repo.Root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if err != nil {
		return candidateStateUntrackedFile{}, newGitError(GitErrorOperation, "snapshot untracked file", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(absolute)
		if err != nil {
			return candidateStateUntrackedFile{}, newGitError(GitErrorOperation, "snapshot untracked symlink", err)
		}
		return candidateStateUntrackedFile{Path: path, Kind: "symlink", Size: int64(len([]byte(target))), SHA256: digestBytes([]byte(target))}, nil
	}
	if !info.Mode().IsRegular() {
		return candidateStateUntrackedFile{}, newGitError(GitErrorOperation, "snapshot untracked file", fmt.Errorf("non-regular candidate path: %s", path))
	}
	file, err := os.Open(absolute)
	if err != nil {
		return candidateStateUntrackedFile{}, newGitError(GitErrorOperation, "snapshot untracked file", err)
	}
	hash := sha256.New()
	read, readErr := io.Copy(hash, file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return candidateStateUntrackedFile{}, newGitError(GitErrorOperation, "snapshot untracked file", errors.Join(readErr, closeErr))
	}
	if read != info.Size() {
		return candidateStateUntrackedFile{}, newGitError(GitErrorOperation, "snapshot untracked file", fmt.Errorf("candidate changed while reading %s", path))
	}
	return candidateStateUntrackedFile{
		Path: path, Kind: "regular", Executable: info.Mode().Perm()&0o111 != 0,
		Size: read, SHA256: hex.EncodeToString(hash.Sum(nil)),
	}, nil
}

func digestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (repo Repository) ResolveAncestor(reference string) (string, error) {
	head, err := repo.CleanHead()
	if err != nil {
		return "", err
	}
	ancestor, err := repo.resolveCommit(context.Background(), reference)
	if err != nil {
		return "", err
	}
	if err := repo.gitRun(context.Background(), "merge-base", "--is-ancestor", ancestor, head); err != nil {
		if gitExitStatus(err) == 1 {
			return "", newGitError(GitErrorNotAncestor, "resolve ancestor", fmt.Errorf("%s is not an ancestor of %s", ancestor, head))
		}
		return "", newGitError(GitErrorOperation, "resolve ancestor", err)
	}
	return ancestor, nil
}

func (repo Repository) IsAncestor(ancestor, descendant string) (bool, error) {
	ctx := context.Background()
	resolvedAncestor, err := repo.resolveExactCommit(ctx, ancestor)
	if err != nil {
		return false, err
	}
	resolvedDescendant, err := repo.resolveExactCommit(ctx, descendant)
	if err != nil {
		return false, err
	}
	return ancestryResult(repo.gitRun(ctx, "merge-base", "--is-ancestor", resolvedAncestor, resolvedDescendant))
}

func ancestryResult(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if gitExitStatus(err) == 1 {
		return false, nil
	}
	return false, newGitError(GitErrorOperation, "check exact Git ancestry", err)
}

func (repo Repository) Patch(base, candidate string, paths []string) ([]byte, error) {
	ctx := context.Background()
	resolvedBase, err := repo.resolveExactCommit(ctx, base)
	if err != nil {
		return nil, err
	}
	resolvedCandidate, err := repo.resolveExactCommit(ctx, candidate)
	if err != nil {
		return nil, err
	}
	normalizedPaths, err := repo.patchPaths(paths)
	if err != nil {
		return nil, err
	}
	arguments := []string{"diff", "--binary", "--no-ext-diff", resolvedBase, resolvedCandidate, "--"}
	arguments = append(arguments, normalizedPaths...)
	patch, err := repo.gitOutput(ctx, arguments...)
	if err != nil {
		return nil, newGitError(GitErrorOperation, "create binary Git patch", err)
	}
	return patch, nil
}

func (repo Repository) AddDetachedWorktree(ctx context.Context, path, revision string) error {
	worktree, err := repo.newWorktreePath(path)
	if err != nil {
		return err
	}
	resolvedRevision, err := repo.resolveExactCommit(ctx, revision)
	if err != nil {
		return err
	}
	if _, err := repo.gitOutput(ctx, "worktree", "add", "--quiet", "--detach", worktree, resolvedRevision); err != nil {
		return newGitError(GitErrorOperation, "add detached worktree", err)
	}
	if err := repo.validateDetachedWorktree(ctx, worktree); err != nil {
		return repo.cleanupAddedWorktree(worktree, err)
	}
	return nil
}

func (repo Repository) RemoveWorktree(ctx context.Context, path string) error {
	worktree, err := repo.existingWorktreePath(path)
	if err != nil {
		return err
	}
	if err := repo.validateDetachedWorktree(ctx, worktree); err != nil {
		return err
	}
	if _, err := repo.gitOutput(ctx, "worktree", "remove", "--force", worktree); err != nil {
		return newGitError(GitErrorOperation, "remove detached worktree", err)
	}
	if _, err := os.Lstat(worktree); !errors.Is(err, os.ErrNotExist) {
		return newGitError(GitErrorOperation, "remove detached worktree", errors.New("worktree path remains after removal"))
	}
	return nil
}

func (repo Repository) candidateChanges(ctx context.Context) ([]string, error) {
	commands := [][]string{
		{"diff", "--name-only", "-z", "--no-renames", "--"},
		{"diff", "--cached", "--name-only", "-z", "--no-renames", "--"},
		{"ls-files", "-z", "--others", "--exclude-standard"},
	}
	changed := []string{}
	for _, arguments := range commands {
		output, err := repo.gitOutput(ctx, arguments...)
		if err != nil {
			return nil, err
		}
		changed = append(changed, repo.includedPaths(output)...)
	}
	return uniqueSorted(changed), nil
}

func (repo Repository) includedPaths(output []byte) []string {
	paths := []string{}
	for _, raw := range bytes.Split(output, []byte{0}) {
		path := filepath.ToSlash(string(raw))
		if path != "" && !repo.IsExcluded(path) {
			paths = append(paths, path)
		}
	}
	return paths
}

func (repo Repository) resolveExactCommit(ctx context.Context, revision string) (string, error) {
	if !exactRevision(revision) {
		return "", newGitError(GitErrorInvalidReference, "resolve exact Git revision", fmt.Errorf("revision must be an exact Git object ID: %q", revision))
	}
	return repo.resolveCommit(ctx, revision)
}

func (repo Repository) resolveCommit(ctx context.Context, reference string) (string, error) {
	if !validGitReference(reference) {
		return "", newGitError(GitErrorInvalidReference, "resolve Git reference", fmt.Errorf("invalid reference %q", reference))
	}
	output, err := repo.gitOutput(ctx, "rev-parse", "--verify", "--end-of-options", reference+"^{commit}")
	if err != nil {
		return "", newGitError(GitErrorInvalidReference, "resolve Git reference", err)
	}
	revision, err := exactCommitOutput(output)
	if err != nil {
		return "", newGitError(GitErrorInvalidReference, "resolve Git reference", err)
	}
	return revision, nil
}

func validGitReference(reference string) bool {
	return reference != "" && strings.TrimSpace(reference) == reference && !strings.HasPrefix(reference, "-") && !strings.ContainsAny(reference, " \t\r\n\x00")
}

func exactCommitOutput(output []byte) (string, error) {
	if !bytes.HasSuffix(output, []byte("\n")) {
		return "", errors.New("git did not return one exact commit")
	}
	revision := string(bytes.TrimSuffix(output, []byte("\n")))
	if !exactRevision(revision) {
		return "", errors.New("git did not return one exact commit")
	}
	return revision, nil
}

func (repo Repository) patchPaths(paths []string) ([]string, error) {
	normalized := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		candidate, err := repo.NormalizePath(path)
		if err != nil || filepath.ToSlash(path) != candidate || seen[candidate] {
			return nil, newGitError(GitErrorInvalidPath, "validate patch paths", fmt.Errorf("invalid repository path %q", path))
		}
		seen[candidate] = true
		normalized = append(normalized, candidate)
	}
	return normalized, nil
}

func (repo Repository) newWorktreePath(path string) (string, error) {
	worktree, err := repo.worktreePath(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(worktree); !errors.Is(err, os.ErrNotExist) {
		return "", newGitError(GitErrorInvalidPath, "add detached worktree", fmt.Errorf("worktree path must be new: %s", worktree))
	}
	return worktree, nil
}

func (repo Repository) existingWorktreePath(path string) (string, error) {
	worktree, err := repo.worktreePath(path)
	if err != nil {
		return "", err
	}
	if !isDirectory(worktree) {
		return "", newGitError(GitErrorInvalidPath, "remove detached worktree", fmt.Errorf("worktree path is missing or not a directory: %s", worktree))
	}
	return worktree, nil
}

func (repo Repository) worktreePath(path string) (string, error) {
	if path == "" || strings.TrimSpace(path) != path || strings.ContainsRune(path, 0) {
		return "", newGitError(GitErrorInvalidPath, "validate worktree path", fmt.Errorf("invalid worktree path %q", path))
	}
	worktree, err := repo.absoluteWorktreePath(path)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(worktree))
	if err != nil || !isDirectory(parent) {
		return "", newGitError(GitErrorInvalidPath, "validate worktree path", fmt.Errorf("worktree parent is missing or not a directory: %s", filepath.Dir(worktree)))
	}
	worktree = filepath.Join(parent, filepath.Base(worktree))
	if repo.gitAdministrationPath(worktree) || sameRepositoryPath(worktree, repo.Root) {
		return "", newGitError(GitErrorInvalidPath, "validate worktree path", fmt.Errorf("worktree path is unsafe: %s", worktree))
	}
	return worktree, nil
}

func (repo Repository) absoluteWorktreePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		normalized, err := repo.NormalizePath(path)
		if err != nil || filepath.ToSlash(path) != normalized {
			return "", newGitError(GitErrorInvalidPath, "validate worktree path", fmt.Errorf("worktree path must be canonical and contained: %q", path))
		}
		path = filepath.Join(repo.Root, filepath.FromSlash(normalized))
	}
	worktree, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", newGitError(GitErrorInvalidPath, "validate worktree path", err)
	}
	return worktree, nil
}

func (repo Repository) gitAdministrationPath(path string) bool {
	relative, err := filepath.Rel(repo.Root, path)
	if err != nil {
		return false
	}
	return relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator))
}

func (repo Repository) validateDetachedWorktree(ctx context.Context, path string) error {
	registered, detached, err := repo.worktreeState(ctx, path)
	if err != nil {
		return err
	}
	if !registered || !detached {
		return newGitError(GitErrorInvalidPath, "validate detached worktree", fmt.Errorf("worktree is not a registered detached workspace: %s", path))
	}
	return nil
}

func (repo Repository) worktreeState(ctx context.Context, path string) (bool, bool, error) {
	output, err := repo.gitOutput(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, false, newGitError(GitErrorOperation, "list worktrees", err)
	}
	for _, worktree := range parseWorktreeRecords(output) {
		if sameRepositoryPath(path, worktree.Path) {
			return true, worktree.Detached, nil
		}
	}
	return false, false, nil
}

func (repo Repository) cleanupAddedWorktree(path string, original error) error {
	if cleanupErr := repo.RemoveWorktree(context.Background(), path); cleanupErr != nil {
		return newGitError(GitErrorOperation, "clean up detached worktree", errors.Join(original, cleanupErr))
	}
	return original
}

func (repo Repository) gitOutput(ctx context.Context, arguments ...string) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	command := exec.CommandContext(ctx, "git", append([]string{"-c", "core.longpaths=true", "-C", repo.Root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return nil, gitCommandError(arguments, err)
	}
	return output, nil
}

func (repo Repository) gitRun(ctx context.Context, arguments ...string) error {
	_, err := repo.gitOutput(ctx, arguments...)
	return err
}

func gitCommandError(arguments []string, err error) error {
	invocation := "git " + strings.Join(arguments, " ")
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if diagnostic := strings.TrimSpace(string(exitError.Stderr)); diagnostic != "" {
			return fmt.Errorf("%s: %s: %w", invocation, diagnostic, err)
		}
	}
	return fmt.Errorf("%s: %w", invocation, err)
}

func gitExitStatus(err error) int {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

type worktreeRecord struct {
	Path     string
	Detached bool
}

func parseWorktreeRecords(output []byte) []worktreeRecord {
	records := []worktreeRecord{}
	record := worktreeRecord{}
	for _, raw := range bytes.Split(output, []byte{0}) {
		field := string(raw)
		if field == "" {
			if record.Path != "" {
				records = append(records, record)
			}
			record = worktreeRecord{}
			continue
		}
		if strings.HasPrefix(field, "worktree ") {
			record.Path = strings.TrimPrefix(field, "worktree ")
		}
		if field == "detached" {
			record.Detached = true
		}
	}
	if record.Path != "" {
		records = append(records, record)
	}
	return records
}

func sameRepositoryPath(left, right string) bool {
	left, leftErr := filepath.Abs(left)
	right, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(left) == filepath.Clean(right)
}

func newGitError(kind GitErrorKind, operation string, cause error) *GitError {
	return &GitError{Kind: kind, Operation: operation, Cause: cause}
}
