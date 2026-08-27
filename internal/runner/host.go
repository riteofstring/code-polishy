package runner

import (
	"context"
	"io"
)

// HostCommand is the complete, already-resolved process contract presented to
// the platform runtime. Argv includes argv[0]; Environment is the complete
// environment rather than a list of additions to the current process.
type HostCommand struct {
	Path        string
	Argv        []string
	Directory   string
	Environment []string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

// HostResult distinguishes startup failure, which has no process status, from
// the exact status of a process that started and was reaped. Quiescent confirms
// that containment, inherited output, and the process-tree witness are quiet.
type HostResult struct {
	Started    bool
	Quiescent  bool
	ExitStatus int
}

// Run starts one contained process tree and returns only after the leader is
// reaped, inherited output is drained, and the platform containment is quiet.
func Run(ctx context.Context, command HostCommand) (HostResult, error) {
	return runHostCommand(ctx, command)
}

// Handoff transfers the delivery process to command using the native platform
// operation. It returns only when validation or startup fails.
func Handoff(ctx context.Context, command HostCommand) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return handoffHostCommand(command)
}
