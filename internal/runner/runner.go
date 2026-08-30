package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type Runner interface {
	Run(context.Context, string, policy.Command) error
}

type Output struct {
	Stdout []byte
	Stderr []byte
}

type OutputRunner interface {
	Runner
	RunWithOutput(context.Context, string, policy.Command) (Result, Output, error)
}

type OSRunner struct {
	Stdout      io.Writer
	Stderr      io.Writer
	PathEntries []string
}

type Result struct {
	ExitStatus        int
	ExecutionDuration time.Duration
	ResourceWait      time.Duration
	FailureCategory   FailureCategory
}

type FailureCategory string

const (
	FailureCommandExit FailureCategory = "command-exit"
	FailureTimeout     FailureCategory = "timeout"
	FailureCanceled    FailureCategory = "canceled"
	FailureEnvironment FailureCategory = "environment"
	FailureResource    FailureCategory = "resource"
	FailureOperational FailureCategory = "operational"
)

func FailureCategoryFor(ctx context.Context, result Result, err error) FailureCategory {
	if err == nil {
		return ""
	}
	if validFailureCategory(result.FailureCategory) {
		return result.FailureCategory
	}
	if category := contextFailureCategory(ctx); category != "" {
		return category
	}
	if result.ExitStatus > 0 {
		return FailureCommandExit
	}
	return FailureOperational
}

func validFailureCategory(category FailureCategory) bool {
	switch category {
	case FailureCommandExit, FailureTimeout, FailureCanceled, FailureEnvironment, FailureResource, FailureOperational:
		return true
	default:
		return false
	}
}

func contextFailureCategory(ctx context.Context) FailureCategory {
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return FailureTimeout
	case context.Canceled:
		return FailureCanceled
	default:
		return ""
	}
}

const (
	commandCleanupDelay              = 5 * time.Second
	maximumStructuredOutputBytes     = 8 << 20
	maximumStructuredDiagnosticBytes = 64 << 10
)

func (runner OSRunner) Run(parent context.Context, root string, specification policy.Command) error {
	_, err := runner.RunWithStatus(parent, root, specification)
	return err
}

func (runner OSRunner) RunWithStatus(parent context.Context, root string, specification policy.Command) (int, error) {
	result, err := runner.RunWithResult(parent, root, specification)
	return result.ExitStatus, err
}

func (runner OSRunner) RunWithResult(parent context.Context, root string, specification policy.Command) (Result, error) {
	return runner.run(parent, root, specification, runner.Stdout, runner.Stderr)
}

func (runner OSRunner) RunWithOutput(parent context.Context, root string, specification policy.Command) (Result, Output, error) {
	stdout := &boundedOutput{limit: maximumStructuredOutputBytes}
	stderr := &boundedOutput{limit: maximumStructuredDiagnosticBytes}
	result, err := runner.run(parent, root, specification, appendOutputWriter(runner.Stdout, stdout), appendOutputWriter(runner.Stderr, stderr))
	if stdout.truncated || stderr.truncated {
		err = errors.Join(err, fmt.Errorf("captured structured output for command %q exceeds its %d byte stream limit", specification.Name, captureLimit(stdout, stderr)))
		result.FailureCategory = FailureCategoryFor(parent, result, err)
	}
	return result, Output{Stdout: stdout.buffer.Bytes(), Stderr: stderr.buffer.Bytes()}, err
}

func captureLimit(stdout, stderr *boundedOutput) int {
	if stdout.truncated {
		return stdout.limit
	}
	return stderr.limit
}

func appendOutputWriter(configured io.Writer, captured io.Writer) io.Writer {
	if configured == nil {
		return captured
	}
	return io.MultiWriter(configured, captured)
}

func (runner OSRunner) run(parent context.Context, root string, specification policy.Command, stdout, stderr io.Writer) (Result, error) {
	workingDirectory, argv, err := runner.commandInvocation(root, specification)
	if err != nil {
		return Result{ExitStatus: -1, FailureCategory: FailureEnvironment}, err
	}
	lease, resourceWait, err := AcquireExclusiveResources(parent, specification.ExclusiveResources)
	if err != nil {
		category := contextFailureCategory(parent)
		if category == "" {
			category = FailureResource
		}
		return Result{ExitStatus: -1, ResourceWait: resourceWait, FailureCategory: category}, fmt.Errorf("acquire exclusive resources for command %q: %w", specification.Name, err)
	}
	defer lease.Release()
	return runner.runLeasedCommand(parent, specification, workingDirectory, argv, resourceWait, stdout, stderr)
}

func (runner OSRunner) commandInvocation(root string, specification policy.Command) (string, []string, error) {
	workingDirectory, err := containedPath(root, specification.Cwd)
	if err != nil {
		return "", nil, err
	}
	argv := append([]string{}, specification.Argv...)
	executable, err := runner.resolveExecutable(root, argv[0])
	if err != nil {
		return "", nil, err
	}
	argv[0] = executable
	return workingDirectory, argv, nil
}

func (runner OSRunner) resolveExecutable(root, executable string) (string, error) {
	if filepath.IsAbs(executable) {
		resolved, err := filepath.EvalSymlinks(executable)
		if err != nil {
			return "", fmt.Errorf("resolve executable %q: %w", executable, err)
		}
		return resolved, nil
	}
	if strings.ContainsAny(executable, "/\\") {
		return containedPath(root, executable)
	}
	resolved, err := lookPath(executable, runner.PathEntries)
	if err != nil {
		return "", fmt.Errorf("required executable %q is unavailable: %w", executable, err)
	}
	return resolved, nil
}

func (runner OSRunner) runLeasedCommand(parent context.Context, specification policy.Command, workingDirectory string, argv []string, resourceWait time.Duration, stdout, stderr io.Writer) (Result, error) {
	started := time.Now()
	timeout := time.Duration(specification.TimeoutSeconds) * time.Second
	contextWithTimeout, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	environment, cleanupEnvironment, err := commandEnvironment(specification, runner.PathEntries)
	if err != nil {
		return Result{ExitStatus: -1, ExecutionDuration: time.Since(started), ResourceWait: resourceWait, FailureCategory: FailureEnvironment}, err
	}
	defer cleanupEnvironment()
	hostResult, runErr := Run(contextWithTimeout, HostCommand{
		Path: argv[0], Argv: argv, Directory: workingDirectory, Environment: environment,
		Stdout: stdout, Stderr: stderr,
	})
	if runErr != nil {
		return commandFailureResult(hostResult, contextWithTimeout, specification.Name, timeout, started, resourceWait, runErr)
	}
	return Result{ExitStatus: hostResult.ExitStatus, ExecutionDuration: time.Since(started), ResourceWait: resourceWait}, nil
}

func commandFailureResult(hostResult HostResult, commandContext context.Context, name string, timeout time.Duration, started time.Time, resourceWait time.Duration, runErr error) (Result, error) {
	result := Result{ExitStatus: -1, ExecutionDuration: time.Since(started), ResourceWait: resourceWait}
	if commandContext.Err() == context.DeadlineExceeded {
		result.ExitStatus = 124
		result.FailureCategory = FailureTimeout
		return result, fmt.Errorf("command %q timed out after %s", name, timeout)
	}
	if commandContext.Err() == context.Canceled {
		result.FailureCategory = FailureCanceled
		return result, fmt.Errorf("command %q was canceled", name)
	}
	if hostResult.Started {
		result.ExitStatus = hostResult.ExitStatus
	}
	if hostResult.Started && hostResult.ExitStatus != 0 {
		result.FailureCategory = FailureCommandExit
	} else if !hostResult.Started {
		result.FailureCategory = FailureEnvironment
	} else {
		result.FailureCategory = FailureOperational
	}
	return result, fmt.Errorf("command %q failed: %w", name, runErr)
}

type boundedOutput struct {
	limit     int
	buffer    bytes.Buffer
	truncated bool
}

func (output *boundedOutput) Write(data []byte) (int, error) {
	remaining := output.limit - output.buffer.Len()
	if len(data) > remaining {
		output.truncated = true
		output.buffer.Write(data[:remaining])
		return len(data), nil
	}
	output.buffer.Write(data)
	return len(data), nil
}

func commandEnvironment(specification policy.Command, pathEntries []string) ([]string, func(), error) {
	if !specification.SealedEnvironment {
		return EnvironmentWithPath(specification.Environment, pathEntries), func() {}, nil
	}
	root, err := os.MkdirTemp("", "code-polishy-command-")
	if err != nil {
		return nil, nil, fmt.Errorf("create sealed command environment: %w", err)
	}
	home := filepath.Join(root, "home")
	temporary := filepath.Join(root, "tmp")
	for _, directory := range []string{home, temporary} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			_ = os.RemoveAll(root)
			return nil, nil, fmt.Errorf("create sealed command environment: %w", err)
		}
	}
	absentConfiguration := filepath.Join(home, "absent-npmrc")
	environment := []string{
		"PATH=",
		"HOME=" + home,
		"TMPDIR=" + temporary,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"),
		"XDG_STATE_HOME=" + filepath.Join(home, "state"),
		"npm_config_userconfig=" + absentConfiguration,
		"npm_config_globalconfig=" + absentConfiguration,
	}
	return environment, func() { _ = os.RemoveAll(root) }, nil
}

func Environment(requested []string) []string {
	baseline := []string{
		"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "USER", "LOGNAME", "SHELL",
		"LANG", "LANGUAGE", "LC_ALL", "TERM", "CI", "GITHUB_ACTIONS",
		"GOCACHE", "GOMODCACHE", "GOPATH", "GOENV",
		"STATICCHECK_CACHE",
		"CGO_ENABLED", "CC", "CXX",
	}
	allowed := append(baseline, requested...)
	environment := []string{}
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && slices.Contains(allowed, name) {
			environment = append(environment, entry)
		}
	}
	return environment
}

func EnvironmentWithPath(requested, prefixes []string) []string {
	environment := Environment(requested)
	if len(prefixes) == 0 {
		return environment
	}
	prefix := strings.Join(prefixes, string(os.PathListSeparator))
	for index, entry := range environment {
		if strings.HasPrefix(entry, "PATH=") {
			environment[index] = "PATH=" + prefix + string(os.PathListSeparator) + strings.TrimPrefix(entry, "PATH=")
			return environment
		}
	}
	return append(environment, "PATH="+prefix)
}

func ToolOutput(executable string, arguments ...string) ([]byte, error) {
	output := &lockedBuffer{}
	result, err := Run(context.Background(), HostCommand{
		Path: executable, Argv: append([]string{executable}, arguments...),
		Environment: Environment(nil), Stdout: output, Stderr: output,
	})
	if err != nil {
		return output.bytes(), err
	}
	if result.ExitStatus != 0 {
		return output.bytes(), fmt.Errorf("tool exited with status %d", result.ExitStatus)
	}
	return output.bytes(), nil
}

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(data)
}

func (buffer *lockedBuffer) bytes() []byte {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return append([]byte{}, buffer.buffer.Bytes()...)
}

func GoModuleVersion(goTool, binary string) string {
	output, err := ToolOutput(goTool, "version", "-m", binary)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "mod" {
			return strings.TrimPrefix(fields[2], "v")
		}
	}
	return ""
}

func lookPath(name string, prefixes []string) (string, error) {
	for _, prefix := range prefixes {
		candidate := filepath.Join(prefix, name)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return lookPathOnHost(name)
}

func containedPath(root, relative string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	if relative == "." {
		return resolvedRoot, nil
	}
	candidate := filepath.Join(resolvedRoot, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve repository path %q: %w", relative, err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("repository path resolves outside the repository: %s", relative)
	}
	return resolved, nil
}
