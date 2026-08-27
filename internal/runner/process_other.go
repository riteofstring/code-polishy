//go:build !unix && !windows

package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

func runHostCommand(ctx context.Context, specification HostCommand) (HostResult, error) {
	if specification.Path == "" || len(specification.Argv) == 0 {
		return HostResult{ExitStatus: -1}, errors.New("host command requires a path and argv")
	}
	command := exec.CommandContext(ctx, specification.Path)
	command.Args = append([]string{}, specification.Argv...)
	command.Dir = specification.Directory
	command.Env = append([]string{}, specification.Environment...)
	command.Stdin = specification.Stdin
	result, err := runContainedCommand(command, specification.Stdout, specification.Stderr)
	return result, err
}

func runContainedCommand(command *exec.Cmd, stdout, stderr io.Writer) (HostResult, error) {
	command.Stdout = stdout
	command.Stderr = stderr
	command.WaitDelay = commandCleanupDelay
	runErr := command.Run()
	result := HostResult{ExitStatus: -1}
	if command.ProcessState != nil {
		result.Started = true
		result.ExitStatus = command.ProcessState.ExitCode()
	}
	return result, runErr
}

func handoffHostCommand(specification HostCommand) error {
	result, err := runHostCommand(context.Background(), specification)
	if err != nil {
		return err
	}
	if result.ExitStatus != 0 {
		return fmt.Errorf("handoff command exited with status %d", result.ExitStatus)
	}
	return nil
}

func lookPathOnHost(name string) (string, error) {
	return exec.LookPath(name)
}
