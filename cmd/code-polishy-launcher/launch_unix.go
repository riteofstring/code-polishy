//go:build unix

package main

import (
	"context"
	"os"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func launch(binary string, argv []string) (int, error) {
	err := runner.Handoff(context.Background(), runner.HostCommand{
		Path: binary, Argv: argv, Environment: os.Environ(),
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	})
	return 0, err
}
