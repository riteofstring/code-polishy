//go:build unix

package main

import (
	"context"
	"os"

	"github.com/riteofstring/code-polishy/internal/runner"
)

// launch replaces the launcher with the release engine, so the release owns the
// terminal, the signals, and the exit status exactly as if it had been invoked
// directly. launch returns only when the release could not be executed at all.
func launch(binary string, argv []string) (int, error) {
	err := runner.Handoff(context.Background(), runner.HostCommand{
		Path: binary, Argv: argv, Environment: os.Environ(),
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	})
	return 0, err
}
