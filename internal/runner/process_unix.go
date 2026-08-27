//go:build unix

package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const (
	executableBusyRetryInterval = 25 * time.Millisecond
	executableBusyRetryTimeout  = time.Second
	processGroupPollInterval    = 10 * time.Millisecond
	processTreeWitnessDelay     = 100 * time.Millisecond
)

var errProcessGroupEscape = errors.New("governed command escaped its process group")

func runHostCommand(ctx context.Context, specification HostCommand) (HostResult, error) {
	// Unix reports ETXTBSY while an executable is still held open for writing.
	// Retry only that pre-start condition, with fresh command and pipe state.
	retryDeadline := time.Now().Add(executableBusyRetryTimeout)
	for {
		command, err := newHostExecCommand(specification)
		if err != nil {
			return HostResult{ExitStatus: -1}, err
		}
		containProcess(command)
		result, runErr := runContainedCommand(ctx, command, specification.Stdout, specification.Stderr)
		if result.Started || !errors.Is(runErr, syscall.ETXTBSY) || !time.Now().Before(retryDeadline) {
			return result, runErr
		}
		timer := time.NewTimer(executableBusyRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, errors.Join(runErr, ctx.Err())
		case <-timer.C:
		}
	}
}

func newHostExecCommand(specification HostCommand) (*exec.Cmd, error) {
	if specification.Path == "" || len(specification.Argv) == 0 {
		return nil, errors.New("host command requires a path and argv")
	}
	command := exec.Command(specification.Path)
	command.Args = append([]string{}, specification.Argv...)
	command.Dir = specification.Directory
	command.Env = append([]string{}, specification.Environment...)
	command.Stdin = specification.Stdin
	return command, nil
}

func handoffHostCommand(specification HostCommand) error {
	if specification.Path == "" || len(specification.Argv) == 0 {
		return errors.New("host command requires a path and argv")
	}
	if specification.Directory != "" {
		if err := os.Chdir(specification.Directory); err != nil {
			return fmt.Errorf("change handoff directory: %w", err)
		}
	}
	return syscall.Exec(specification.Path, specification.Argv, specification.Environment)
}

func lookPathOnHost(name string) (string, error) {
	return exec.LookPath(name)
}

// containProcess puts every governed command in a process group. The runner
// cleans that group after the immediate command exits as well as when its
// context cancels, so resource leases cover the whole process tree.
func containProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// runContainedCommand gives non-file output writers explicit pipes so Cmd.Wait
// can reap the immediate command before its descendants release inherited
// output descriptors. The governed process group is then terminated and
// awaited before the pipes are drained and the caller releases its resources.
func runContainedCommand(ctx context.Context, command *exec.Cmd, stdout, stderr io.Writer) (HostResult, error) {
	streams := commandOutputStreams{}
	stdoutPipe, err := streams.add(stdout)
	if err != nil {
		return HostResult{ExitStatus: -1}, err
	}
	stderrPipe, err := streams.add(stderr)
	if err != nil {
		streams.closeWriters()
		return HostResult{ExitStatus: -1}, errors.Join(err, streams.wait())
	}
	command.Stdout = stdoutPipe
	command.Stderr = stderrPipe
	witness, err := newProcessTreeWitness(command)
	if err != nil {
		streams.closeWriters()
		return HostResult{ExitStatus: -1}, errors.Join(err, streams.wait())
	}
	if err := command.Start(); err != nil {
		witness.closeWriter()
		streams.closeWriters()
		return HostResult{ExitStatus: -1}, errors.Join(err, witness.wait(), streams.wait())
	}
	witness.closeWriter()
	streams.closeWriters()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	var waitErr error
	var cancellationErr error
	cleanupDeadline := time.Time{}
	select {
	case waitErr = <-wait:
	case <-ctx.Done():
		cancellationErr = ctx.Err()
		cleanupDeadline = time.Now().Add(commandCleanupDelay)
		_ = signalProcessGroup(command.Process.Pid, syscall.SIGTERM)
		select {
		case waitErr = <-wait:
		case <-time.After(time.Until(cleanupDeadline)):
			_ = signalProcessGroup(command.Process.Pid, syscall.SIGKILL)
			waitErr = <-wait
		}
	}
	cleanupErr := cleanupProcessGroup(command.Process.Pid, cleanupDeadline, witness)
	witnessErr := witness.wait()
	streamErr := streams.wait()
	result := HostResult{
		Started:    true,
		Quiescent:  cleanupErr == nil && witnessErr == nil && streamErr == nil,
		ExitStatus: command.ProcessState.ExitCode(),
	}
	return result, errors.Join(cancellationErr, waitErr, cleanupErr, witnessErr, streamErr)
}

// processTreeWitness is inherited as descriptor 3 by the direct command. Unix
// fork/exec descendants retain it unless they deliberately discard the
// runner's containment contract. Once the governed process group is quiet, an
// open witness proves that a descendant moved to another process group.
type processTreeWitness struct {
	reader *os.File
	writer *os.File
	done   chan struct{}
	err    error
}

func newProcessTreeWitness(command *exec.Cmd) (*processTreeWitness, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create process-tree witness: %w", err)
	}
	witness := &processTreeWitness{reader: reader, writer: writer, done: make(chan struct{})}
	command.ExtraFiles = append(command.ExtraFiles, writer)
	go func() {
		_, readErr := io.Copy(io.Discard, reader)
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, os.ErrClosed) {
			readErr = fmt.Errorf("read process-tree witness: %w", readErr)
		} else {
			readErr = nil
		}
		witness.err = readErr
		close(witness.done)
	}()
	return witness, nil
}

func (witness *processTreeWitness) closeWriter() {
	_ = witness.writer.Close()
}

func (witness *processTreeWitness) wait() error {
	select {
	case <-witness.done:
		_ = witness.reader.Close()
		return witness.err
	case <-time.After(processTreeWitnessDelay):
		_ = witness.reader.Close()
		<-witness.done
		return errProcessGroupEscape
	}
}

func (witness *processTreeWitness) quiescent() bool {
	select {
	case <-witness.done:
		return true
	default:
		return false
	}
}

type commandOutputStreams struct {
	writers []*os.File
	waiters sync.WaitGroup
	mu      sync.Mutex
	errs    []error
}

func (streams *commandOutputStreams) add(output io.Writer) (*os.File, error) {
	if output == nil {
		return nil, nil
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create command output pipe: %w", err)
	}
	streams.writers = append(streams.writers, writer)
	streams.waiters.Add(1)
	go streams.drain(reader, output)
	return writer, nil
}

func (streams *commandOutputStreams) closeWriters() {
	for _, writer := range streams.writers {
		_ = writer.Close()
	}
}

func (streams *commandOutputStreams) wait() error {
	streams.waiters.Wait()
	streams.mu.Lock()
	defer streams.mu.Unlock()
	return errors.Join(streams.errs...)
}

func (streams *commandOutputStreams) drain(reader *os.File, output io.Writer) {
	defer streams.waiters.Done()
	defer reader.Close()
	buffer := make([]byte, 32*1024)
	var writeErr error
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 && writeErr == nil {
			written, err := output.Write(buffer[:count])
			if err == nil && written != count {
				err = io.ErrShortWrite
			}
			if err != nil {
				writeErr = fmt.Errorf("write command output: %w", err)
				streams.record(writeErr)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				streams.record(fmt.Errorf("read command output: %w", readErr))
			}
			return
		}
	}
}

func (streams *commandOutputStreams) record(err error) {
	streams.mu.Lock()
	defer streams.mu.Unlock()
	streams.errs = append(streams.errs, err)
}

// cleanupProcessGroup terminates and waits for descendants left behind by a
// command that has otherwise completed. It escalates after the one shared
// grace deadline but still waits for quiescence: releasing an exclusive
// resource while the group exists would permit overlapping governed work.
func cleanupProcessGroup(processID int, deadline time.Time, witness *processTreeWitness) error {
	if deadline.IsZero() {
		deadline = time.Now().Add(commandCleanupDelay)
	}
	if err := signalProcessGroup(processID, syscall.SIGTERM); err != nil && !retryableProcessGroupPermission(err, witness, deadline) {
		return processGroupSignalError(err, witness)
	}
	for {
		err := syscall.Kill(-processID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil {
			// Once the leader is reaped, its numeric process-group ID can be
			// reused before this poll observes ESRCH. EPERM names that unrelated
			// group, not a failure to clean the governed tree, when the inherited
			// witness has already reached EOF.
			if !retryableProcessGroupPermission(err, witness, deadline) {
				return processGroupSignalError(err, witness)
			}
		}
		if time.Now().After(deadline) {
			if err := signalProcessGroup(processID, syscall.SIGKILL); err != nil {
				return processGroupSignalError(err, witness)
			}
		}
		time.Sleep(processGroupPollInterval)
	}
}

func retryableProcessGroupPermission(err error, witness *processTreeWitness, deadline time.Time) bool {
	return errors.Is(err, syscall.EPERM) && !witness.quiescent() && time.Now().Before(deadline)
}

func processGroupSignalError(err error, witness *processTreeWitness) error {
	if errors.Is(err, syscall.EPERM) && witness.quiescent() {
		return nil
	}
	return err
}

func signalProcessGroup(processID int, signal syscall.Signal) error {
	err := syscall.Kill(-processID, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
