//go:build windows

package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func launch(binary string, argv []string) (int, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	result, err := runner.Run(ctx, runner.HostCommand{
		Path: binary, Argv: argv, Environment: os.Environ(),
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	})
	if err == nil || result.Started && result.Quiescent {
		return result.ExitStatus, nil
	}
	return 0, err
}
