package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/riteofstring/code-polishy/internal/runner"
)

// TaskSessionOptions is the complete caller-owned authority for one disposable
// implementation worktree. The worker cannot change these values after the
// session begins.
type TaskSessionOptions struct {
	RepoRoot        string
	PolicyRoot      string
	ConfigPath      string
	Modules         []string
	AllowedPaths    []string
	AllowedNewPaths []string
	OutputDir       string
	Promote         bool
	Command         []string
	Executable      string
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
}

// TaskSessionResult reports the observable terminal state of a native task
// session. Artifacts remain outside the repository regardless of outcome.
type TaskSessionResult struct {
	Status        string
	ExitStatus    int
	OutputDir     string
	Workspace     string
	TrustedBase   string
	CandidateHead string
}

type nativeTaskSession struct {
	options                  TaskSessionOptions
	result                   TaskSessionResult
	sourceBranch             string
	policyBinary             string
	policyDigest             string
	scopeDigest              string
	commandDigest            string
	environment              GovernedEnvironmentReceipt
	environmentReceiptDigest string
	environmentPathsDigest   string
	frozenCommand            []string
	sourceCommonDir          string
	worktreeAdded            bool
	retainWorktree           bool
}

// RunTaskSession executes a fresh task session entirely through Go and the
// native host process boundary. Git is the only external repository adapter;
// no shell or Unix process utility participates in orchestration.
func RunTaskSession(ctx context.Context, options TaskSessionOptions) (TaskSessionResult, error) {
	session := &nativeTaskSession{options: options}
	if err := session.prepare(ctx); err != nil {
		_ = session.cleanup(context.Background())
		return session.result, err
	}
	result, runErr := session.run(ctx)
	if cleanupErr := session.cleanup(context.Background()); cleanupErr != nil {
		session.retainWorktree = true
		if result.Status == "" {
			result.Status = "cleanup-pending"
		} else {
			result.Status += "-cleanup-pending"
		}
		session.result = result
		_ = session.writeReceipt()
		return result, fmt.Errorf("task result is recorded but disposable cleanup failed: %w", cleanupErr)
	}
	return result, runErr
}

func (session *nativeTaskSession) prepare(ctx context.Context) error {
	if len(session.options.Modules) == 0 || len(session.options.Command) == 0 || session.options.Command[0] == "" {
		return errors.New("native task session requires at least one module and one worker command")
	}
	if err := session.prepareFreshSource(ctx); err != nil {
		return err
	}
	if err := session.prepareFreshAuthority(); err != nil {
		return err
	}
	return session.createFreshWorkspace(ctx)
}

func (session *nativeTaskSession) prepareFreshSource(ctx context.Context) error {
	root, err := filepath.EvalSymlinks(session.options.RepoRoot)
	if err != nil {
		return fmt.Errorf("resolve task repository: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve task repository: %w", err)
	}
	session.options.RepoRoot = root
	session.frozenCommand = append([]string{}, session.options.Command...)
	top, err := session.gitText(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil || !samePath(top, root) {
		return fmt.Errorf("target must be the root of a Git worktree: %s", root)
	}
	clean, err := session.gitText(ctx, root, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return err
	}
	if clean != "" {
		return errors.New("target worktree must be clean before an isolated task session")
	}
	return session.loadFreshSourceIdentity(ctx, root)
}

func (session *nativeTaskSession) loadFreshSourceIdentity(ctx context.Context, root string) error {
	commonDirectory, err := session.gitText(ctx, root, "rev-parse", "--git-common-dir")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(commonDirectory) {
		commonDirectory = filepath.Join(root, commonDirectory)
	}
	session.sourceCommonDir, err = filepath.EvalSymlinks(commonDirectory)
	if err != nil {
		return fmt.Errorf("resolve source Git directory: %w", err)
	}
	session.result.TrustedBase, err = session.gitText(ctx, root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return err
	}
	session.sourceBranch, _ = session.gitText(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if session.options.Promote && session.sourceBranch == "" {
		return errors.New("--promote requires the source repository to be on a branch")
	}
	return nil
}

func (session *nativeTaskSession) prepareFreshAuthority() error {
	if err := session.prepareOutput(); err != nil {
		return err
	}
	if err := session.freezeExecutable(); err != nil {
		return err
	}
	if err := session.freezeAuthority(); err != nil {
		return err
	}
	if err := session.freezeEnvironment(); err != nil {
		return err
	}
	return nil
}

func (session *nativeTaskSession) createFreshWorkspace(ctx context.Context) error {
	root := session.options.RepoRoot
	session.result.Status = "preflight"
	if err := session.writeReceipt(); err != nil {
		return err
	}
	if err := session.checkBoundary(root, filepath.Join(session.result.OutputDir, "preflight.log")); err != nil {
		session.result.Status = "invalid"
		_ = session.writeReceipt()
		return fmt.Errorf("task-session preflight failed: %w", err)
	}
	if _, err := session.git(ctx, root, nil, "worktree", "add", "--quiet", "--detach", session.result.Workspace, session.result.TrustedBase); err != nil {
		return err
	}
	session.worktreeAdded = true
	if err := session.validateWorkspaceIdentity(ctx); err != nil {
		session.retainWorktree = true
		return err
	}
	candidate, err := session.gitText(ctx, session.result.Workspace, "rev-parse", "--verify", "HEAD")
	session.result.CandidateHead = candidate
	return err
}

func (session *nativeTaskSession) prepareOutput() error {
	output := session.options.OutputDir
	var err error
	if output == "" {
		output, err = os.MkdirTemp("", "code-polishy-task.*")
		if err != nil {
			return fmt.Errorf("create task artifact directory: %w", err)
		}
	} else {
		if !filepath.IsAbs(output) {
			output, err = filepath.Abs(output)
			if err != nil {
				return fmt.Errorf("resolve task artifact directory: %w", err)
			}
		}
		if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("--output-dir must name a new path: %s", output)
		}
		if err := os.Mkdir(output, 0o700); err != nil {
			return fmt.Errorf("create task artifact directory: %w", err)
		}
	}
	output, err = filepath.EvalSymlinks(output)
	if err != nil {
		return fmt.Errorf("resolve task artifact directory: %w", err)
	}
	if pathWithin(session.options.RepoRoot, output) {
		return fmt.Errorf("task-session artifacts must stay outside the target repository: %s", output)
	}
	session.result.OutputDir = output
	session.result.Workspace = filepath.Join(output, "worktree")
	return nil
}

func (session *nativeTaskSession) freezeExecutable() error {
	executable := session.options.Executable
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolve policy executable: %w", err)
		}
	}
	executable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve policy executable: %w", err)
	}
	name := "trusted-code-polishy"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	session.policyBinary = filepath.Join(session.result.OutputDir, name)
	if err := copyRegularFile(executable, session.policyBinary, 0o500); err != nil {
		return fmt.Errorf("freeze policy executable: %w", err)
	}
	session.policyDigest, err = digestFile(session.policyBinary)
	return err
}

func (session *nativeTaskSession) freezeAuthority() error {
	scope := make([]string, 0, 2*(len(session.options.Modules)+len(session.options.AllowedPaths)+len(session.options.AllowedNewPaths)))
	for _, value := range session.options.Modules {
		scope = append(scope, "--module", value)
	}
	for _, value := range session.options.AllowedPaths {
		scope = append(scope, "--allow-path", value)
	}
	for _, value := range session.options.AllowedNewPaths {
		scope = append(scope, "--allow-new-path", value)
	}
	var err error
	session.scopeDigest, err = writeNULArtifact(filepath.Join(session.result.OutputDir, "scope.argv"), scope)
	if err != nil {
		return err
	}
	session.commandDigest, err = writeNULArtifact(filepath.Join(session.result.OutputDir, "command.argv"), session.frozenCommand)
	return err
}

func (session *nativeTaskSession) freezeEnvironment() error {
	policyEngine, err := Open(session.options.RepoRoot, session.options.PolicyRoot, session.options.ConfigPath)
	if err != nil {
		return err
	}
	session.environment, err = FreezeGovernedEnvironment(policyEngine.Repository)
	if err != nil {
		return err
	}
	data, err := MarshalGovernedEnvironment(session.environment)
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(session.result.OutputDir, "governed-environment.json")
	if err := writeAtomic(receiptPath, append(data, '\n'), 0o600); err != nil {
		return err
	}
	paths := strings.Join(session.environment.PathEntries, "\n") + "\n"
	pathsPath := filepath.Join(session.result.OutputDir, "governed-environment.paths")
	if err := writeAtomic(pathsPath, []byte(paths), 0o600); err != nil {
		return err
	}
	session.environmentReceiptDigest, err = digestFile(receiptPath)
	if err != nil {
		return err
	}
	session.environmentPathsDigest, err = digestFile(pathsPath)
	return err
}

func (session *nativeTaskSession) run(ctx context.Context) (TaskSessionResult, error) {
	session.result.Status = "running"
	if err := session.writeReceipt(); err != nil {
		return session.result, err
	}
	logFile, err := os.OpenFile(filepath.Join(session.result.OutputDir, "command.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return session.result, err
	}
	defer logFile.Close()
	stdout := io.MultiWriter(writerOrDiscard(session.options.Stdout), logFile)
	stderr := io.MultiWriter(writerOrDiscard(session.options.Stderr), logFile)
	command := session.options.Command
	result, runErr := runner.Run(ctx, runner.HostCommand{
		Path: command[0], Argv: command, Directory: session.result.Workspace,
		Environment: session.workerEnvironment(), Stdin: session.options.Stdin,
		Stdout: stdout, Stderr: stderr,
	})
	session.result.ExitStatus = result.ExitStatus
	if done, terminalResult, terminalErr := session.recordWorkerTerminalState(ctx, result, runErr); done {
		return terminalResult, terminalErr
	}
	if err := session.validateCompletedTask(); err != nil {
		return session.result, err
	}
	return session.finishCompletedTask()
}

func (session *nativeTaskSession) recordWorkerTerminalState(ctx context.Context, result runner.HostResult, runErr error) (bool, TaskSessionResult, error) {
	if identityErr := session.validateWorkspaceIdentity(context.Background()); identityErr != nil {
		session.retainWorktree = true
		session.result.Status = "workspace-untrusted"
		_ = session.writeReceipt()
		return true, session.result, identityErr
	}
	session.result.CandidateHead, _ = session.gitText(context.Background(), session.result.Workspace, "rev-parse", "--verify", "HEAD")
	if ctx.Err() != nil {
		session.retainWorktree = true
		session.result.Status = "interrupted"
		_ = session.snapshot(context.Background())
		_ = session.writeReceipt()
		return true, session.result, ctx.Err()
	}
	if runErr != nil || result.ExitStatus != 0 {
		session.result.Status = "command-failed"
		_ = session.snapshot(context.Background())
		_ = session.writeReceipt()
		if runErr != nil && (!result.Started || !result.Quiescent) {
			return true, session.result, runErr
		}
		return true, session.result, fmt.Errorf("task command failed with status %d", result.ExitStatus)
	}
	return false, session.result, nil
}

func (session *nativeTaskSession) validateCompletedTask() error {
	if err := session.validateFrozenState(); err != nil {
		session.retainWorktree = true
		session.result.Status = "authority-changed"
		_ = session.snapshot(context.Background())
		_ = session.writeReceipt()
		return err
	}
	if err := session.sourceUnchanged(context.Background()); err != nil {
		session.retainWorktree = true
		session.result.Status = "source-changed"
		_ = session.snapshot(context.Background())
		_ = session.writeReceipt()
		return err
	}
	if err := session.checkBoundary(session.result.Workspace, filepath.Join(session.result.OutputDir, "boundary.log")); err != nil {
		session.result.Status = "boundary-rejected"
		_ = session.snapshot(context.Background())
		_ = session.writeReceipt()
		return err
	}
	return nil
}

func (session *nativeTaskSession) finishCompletedTask() (TaskSessionResult, error) {
	if session.options.Promote {
		if err := session.promote(context.Background()); err != nil {
			session.retainWorktree = true
			session.result.Status = "promotion-rejected"
			_ = session.snapshot(context.Background())
			_ = session.writeReceipt()
			return session.result, err
		}
		session.result.Status = "promoted"
	} else {
		session.result.Status = "passed"
	}
	if err := session.writeReceipt(); err != nil {
		return session.result, err
	}
	return session.result, nil
}

func (session *nativeTaskSession) workerEnvironment() []string {
	environment := append([]string{}, os.Environ()...)
	pathValue := strings.Join(session.environment.PathEntries, string(os.PathListSeparator))
	if ambient := os.Getenv("PATH"); ambient != "" {
		pathValue += string(os.PathListSeparator) + ambient
	}
	values := map[string]string{
		"CODE_POLISHY_TASK_SESSION": "1", "CODE_POLISHY_TASK_BASE": session.result.TrustedBase,
		"CODE_POLISHY_TASK_SOURCE_BRANCH":           session.sourceBranch,
		"CODE_POLISHY_TASK_MODULES":                 strings.Join(session.options.Modules, ","),
		"CODE_POLISHY_TASK_FROZEN_BINARY":           session.policyBinary,
		"CODE_POLISHY_TASK_ARTIFACT_DIR":            session.result.OutputDir,
		"CODE_POLISHY_GOVERNED_ENVIRONMENT_RECEIPT": filepath.Join(session.result.OutputDir, "governed-environment.json"),
		"CODE_POLISHY_REPO_ROOT":                    session.result.Workspace, "PATH": pathValue,
	}
	for key, value := range values {
		environment = replaceEnvironment(environment, key, value)
	}
	return environment
}

func (session *nativeTaskSession) checkBoundary(root, logPath string) error {
	report, err := CheckChangeBoundary(root, session.options.PolicyRoot, session.options.ConfigPath, session.result.TrustedBase,
		session.options.Modules, session.options.AllowedPaths, session.options.AllowedNewPaths)
	if err != nil {
		return err
	}
	var output strings.Builder
	if report.ChangeBoundary != nil {
		fmt.Fprintf(&output, "CHANGE BOUNDARY BASE: %s\n", report.ChangeBoundary.Base)
		fmt.Fprintf(&output, "CHANGE BOUNDARY MODULES: %s\n", strings.Join(report.ChangeBoundary.Modules, ","))
	}
	for _, finding := range report.Findings {
		fmt.Fprintf(&output, "FAIL %s %s %s\n", finding.Check, finding.Path, finding.Message)
	}
	if err := writeAtomic(logPath, []byte(output.String()), 0o600); err != nil {
		return err
	}
	if len(report.Findings) != 0 {
		return fmt.Errorf("task changes crossed the trusted boundary (%d findings)", len(report.Findings))
	}
	return nil
}

func (session *nativeTaskSession) validateFrozenState() error {
	digest, err := digestFile(session.policyBinary)
	if err != nil || digest != session.policyDigest {
		return errors.New("the frozen policy executable changed while the task ran")
	}
	scope, err := digestFile(filepath.Join(session.result.OutputDir, "scope.argv"))
	if err != nil || scope != session.scopeDigest {
		return errors.New("the frozen task scope changed while the task ran")
	}
	command, err := digestFile(filepath.Join(session.result.OutputDir, "command.argv"))
	if err != nil || command != session.commandDigest {
		return errors.New("the frozen task command changed while the task ran")
	}
	return nil
}

func (session *nativeTaskSession) sourceUnchanged(ctx context.Context) error {
	root, err := filepath.EvalSymlinks(session.options.RepoRoot)
	if err != nil || !samePath(root, session.options.RepoRoot) {
		return errors.New("original repository identity changed while the task ran")
	}
	common, commonErr := session.gitText(ctx, session.options.RepoRoot, "rev-parse", "--git-common-dir")
	if commonErr != nil {
		return errors.New("original repository identity changed while the task ran")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(session.options.RepoRoot, common)
	}
	common, commonErr = filepath.EvalSymlinks(common)
	if commonErr != nil || !samePath(common, session.sourceCommonDir) {
		return errors.New("original repository identity changed while the task ran")
	}
	head, err := session.gitText(ctx, session.options.RepoRoot, "rev-parse", "--verify", "HEAD")
	if err != nil || head != session.result.TrustedBase {
		return errors.New("original repository changed while the task ran")
	}
	status, err := session.gitText(ctx, session.options.RepoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || status != "" {
		return errors.New("original repository changed while the task ran")
	}
	return nil
}

func (session *nativeTaskSession) promote(ctx context.Context) error {
	status, err := session.gitText(ctx, session.result.Workspace, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil || status != "" {
		return errors.New("--promote requires a clean committed task worktree")
	}
	if _, err := session.git(ctx, session.result.Workspace, nil, "merge-base", "--is-ancestor", session.result.TrustedBase, session.result.CandidateHead); err != nil {
		return errors.New("candidate HEAD is not descended from the trusted base")
	}
	branch, _ := session.gitText(ctx, session.options.RepoRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if branch != session.sourceBranch {
		return errors.New("original branch changed while the task ran")
	}
	if err := session.sourceUnchanged(ctx); err != nil {
		return err
	}
	_, err = session.git(ctx, session.options.RepoRoot, session.options.Stdout, "merge", "--ff-only", session.result.CandidateHead)
	return err
}

func (session *nativeTaskSession) snapshot(ctx context.Context) error {
	if !session.worktreeAdded {
		return nil
	}
	if err := session.validateWorkspaceIdentity(ctx); err != nil {
		return err
	}
	status, _ := session.gitText(ctx, session.result.Workspace, "status", "--short", "--untracked-files=all")
	_ = writeAtomic(filepath.Join(session.result.OutputDir, "candidate-status.txt"), []byte(status+"\n"), 0o600)
	commits, _ := session.gitText(ctx, session.result.Workspace, "log", "--oneline", "--decorate", session.result.TrustedBase+"..HEAD")
	_ = writeAtomic(filepath.Join(session.result.OutputDir, "candidate-commits.txt"), []byte(commits+"\n"), 0o600)
	patch, _ := session.gitBytes(ctx, session.result.Workspace, "diff", "--binary", session.result.TrustedBase, "--")
	return writeAtomic(filepath.Join(session.result.OutputDir, "candidate.patch"), patch, 0o600)
}

func (session *nativeTaskSession) cleanup(ctx context.Context) error {
	if session.retainWorktree || !session.worktreeAdded {
		return nil
	}
	if err := session.validateWorkspaceIdentity(ctx); err != nil {
		session.retainWorktree = true
		return err
	}
	if _, err := session.git(ctx, session.options.RepoRoot, nil, "worktree", "remove", "--force", session.result.Workspace); err == nil {
		session.worktreeAdded = false
		return nil
	} else {
		return err
	}
}

func (session *nativeTaskSession) validateWorkspaceIdentity(ctx context.Context) error {
	pointerPath, admin, err := session.validateWorkspacePointers()
	if err != nil {
		return err
	}
	backPointer, err := os.ReadFile(filepath.Join(admin, "gitdir"))
	if err != nil || !bytes.HasSuffix(backPointer, []byte("\n")) ||
		!samePath(strings.TrimSpace(string(backPointer)), pointerPath) {
		return errors.New("task workspace Git back-pointer does not match the workspace")
	}
	return session.validateRegisteredWorkspace(ctx)
}

func (session *nativeTaskSession) validateWorkspacePointers() (string, string, error) {
	if err := session.validateWorkspacePath(); err != nil {
		return "", "", err
	}
	pointerPath := filepath.Join(session.result.Workspace, ".git")
	admin, err := session.readWorkspaceAdmin(pointerPath)
	return pointerPath, admin, err
}

func (session *nativeTaskSession) validateWorkspacePath() error {
	if session.result.Workspace != filepath.Join(session.result.OutputDir, "worktree") {
		return errors.New("task workspace no longer matches its canonical artifact path")
	}
	resolved, err := filepath.EvalSymlinks(session.result.Workspace)
	if err != nil || !samePath(resolved, session.result.Workspace) {
		return errors.New("task workspace is missing or resolves through another path")
	}
	return nil
}

func (session *nativeTaskSession) readWorkspaceAdmin(pointerPath string) (string, error) {
	pointerInfo, err := os.Lstat(pointerPath)
	if err != nil || !pointerInfo.Mode().IsRegular() || pointerInfo.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("task workspace has an untrusted Git pointer")
	}
	pointer, err := os.ReadFile(pointerPath)
	if err != nil || !bytes.HasSuffix(pointer, []byte("\n")) {
		return "", errors.New("task workspace has a malformed Git pointer")
	}
	line := strings.TrimSuffix(string(pointer), "\n")
	if strings.ContainsAny(line, "\r\x00") || !strings.HasPrefix(line, "gitdir: ") {
		return "", errors.New("task workspace has a malformed Git pointer")
	}
	admin := strings.TrimPrefix(line, "gitdir: ")
	if !filepath.IsAbs(admin) {
		admin = filepath.Join(session.result.Workspace, admin)
	}
	admin, err = filepath.EvalSymlinks(admin)
	if err != nil || !pathWithin(filepath.Join(session.sourceCommonDir, "worktrees"), admin) {
		return "", errors.New("task workspace Git administration escaped the source repository")
	}
	return admin, nil
}

func (session *nativeTaskSession) validateRegisteredWorkspace(ctx context.Context) error {
	inventory, err := session.gitBytes(ctx, session.options.RepoRoot, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	wanted := "worktree " + session.result.Workspace
	for _, record := range bytes.Split(inventory, []byte{0}) {
		if samePath(strings.TrimPrefix(string(record), "worktree "), session.result.Workspace) && strings.HasPrefix(string(record), "worktree ") {
			return nil
		}
	}
	return fmt.Errorf("task workspace is not the registered worktree %s", wanted)
}

func (session *nativeTaskSession) writeReceipt() error {
	fields := [][2]string{
		{"version", "6"}, {"status", session.result.Status}, {"sourceRoot", session.options.RepoRoot},
		{"sourceBranch", session.sourceBranch}, {"trustedBase", session.result.TrustedBase}, {"candidateHead", session.result.CandidateHead},
		{"config", session.options.ConfigPath}, {"promote", fmt.Sprintf("%t", session.options.Promote)},
		{"policyBinary", session.policyBinary}, {"policyDigest", session.policyDigest},
		{"environmentReceipt", filepath.Join(session.result.OutputDir, "governed-environment.json")},
		{"environmentReceiptDigest", session.environmentReceiptDigest},
		{"environmentPaths", filepath.Join(session.result.OutputDir, "governed-environment.paths")},
		{"environmentPathsDigest", session.environmentPathsDigest},
		{"environmentDigest", session.environment.Digest}, {"scopeDigest", session.scopeDigest},
		{"modules", strings.Join(session.options.Modules, ",")}, {"exactPaths", strings.Join(session.options.AllowedPaths, ",")},
		{"newPaths", strings.Join(session.options.AllowedNewPaths, ",")}, {"workspace", session.result.Workspace},
		{"commandDigest", session.commandDigest},
	}
	var receipt strings.Builder
	for _, field := range fields {
		if strings.ContainsAny(field[1], "\r\n\x00") {
			return fmt.Errorf("task-session receipt field %s is not one line", field[0])
		}
		fmt.Fprintf(&receipt, "%s=%s\n", field[0], field[1])
	}
	return writeAtomic(filepath.Join(session.result.OutputDir, "session.txt"), []byte(receipt.String()), 0o600)
}

func (session *nativeTaskSession) git(ctx context.Context, directory string, stdout io.Writer, arguments ...string) (runner.HostResult, error) {
	argv := append([]string{"git"}, arguments...)
	return runner.Run(ctx, runner.HostCommand{Path: "git", Argv: argv, Directory: directory, Environment: os.Environ(), Stdout: stdout, Stderr: session.options.Stderr})
}

func (session *nativeTaskSession) gitBytes(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	var output bytes.Buffer
	result, err := session.git(ctx, directory, &output, arguments...)
	if err != nil && (!result.Started || !result.Quiescent) {
		return nil, err
	}
	if result.ExitStatus != 0 {
		return nil, fmt.Errorf("git %s failed with status %d", arguments[0], result.ExitStatus)
	}
	return output.Bytes(), nil
}

func (session *nativeTaskSession) gitText(ctx context.Context, directory string, arguments ...string) (string, error) {
	data, err := session.gitBytes(ctx, directory, arguments...)
	return strings.TrimSpace(string(data)), err
}
