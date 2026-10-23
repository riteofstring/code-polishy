package supplychain

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func SecurityScanInputs(command policy.Command) []string {
	if !strings.HasPrefix(command.Name, "pnpm-audit-") || !command.SealedEnvironment || len(command.Paths) != 1 {
		return nil
	}
	return command.Paths
}

func RunAuditSnapshot(ctx context.Context, repo repository.Repository, commandRunner runner.Runner, invocation runner.Invocation, data []byte) (runner.Result, runner.Output, error) {
	root, err := os.MkdirTemp("", "code-polishy-audit-snapshot-")
	if err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	defer os.RemoveAll(root)
	directory := filepath.Dir(invocation.Command.Paths[0])
	path := filepath.Join(root, filepath.FromSlash(invocation.Command.Paths[0]))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	defer os.Chmod(path, 0o600)
	if err := os.WriteFile(path, data, 0o400); err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	command, err := (javascript.Bundle{PolicyRoot: repo.PolicyRoot}).AuditCommand(root, filepath.ToSlash(directory))
	if err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	invocation.Command.Argv = command.Argv
	return commandRunner.Execute(ctx, invocation)
}
