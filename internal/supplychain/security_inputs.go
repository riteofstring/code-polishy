package supplychain

import (
	"context"
	"errors"
	"fmt"
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

type auditInputSnapshot struct {
	data       []byte
	digest     string
	path       string
	policyRoot string
}

func CaptureAuditSnapshot(repo repository.Repository, command policy.Command) (runner.InputSnapshot, error) {
	files := SecurityScanInputs(command)
	if len(files) != 1 {
		return nil, errors.New("audit does not declare a complete input")
	}
	data, err := repo.ReadContainedFile(files[0], 128<<20)
	if err != nil {
		return nil, fmt.Errorf("security input %s: %w", files[0], err)
	}
	return &auditInputSnapshot{data: data, digest: gitEvidenceDigest(data), path: files[0], policyRoot: repo.PolicyRoot}, nil
}

func (snapshot *auditInputSnapshot) Identity(command policy.Command) (string, error) {
	files := SecurityScanInputs(command)
	if len(files) != 1 || files[0] != snapshot.path {
		return "", errors.New("audit command does not match its input snapshot")
	}
	return snapshot.digest, nil
}

func (snapshot *auditInputSnapshot) Execute(ctx context.Context, commandRunner runner.Runner, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if _, err := snapshot.Identity(invocation.Command); err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
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
	if err := os.WriteFile(path, snapshot.data, 0o400); err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	command, err := (javascript.Bundle{PolicyRoot: snapshot.policyRoot}).AuditCommand(root, filepath.ToSlash(directory))
	if err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	invocation.Command.Argv = command.Argv
	return commandRunner.Execute(ctx, invocation)
}
