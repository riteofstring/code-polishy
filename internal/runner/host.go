package runner

import (
	"context"
	"io"
)

type HostCommand struct {
	Path        string
	Argv        []string
	Directory   string
	Environment []string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

type HostResult struct {
	Started    bool
	Quiescent  bool
	ExitStatus int
}

func Run(ctx context.Context, command HostCommand) (HostResult, error) {
	return runHostCommand(ctx, command)
}
