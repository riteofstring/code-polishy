//go:build windows

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
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsJobPollInterval = 10 * time.Millisecond

type jobBasicAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
}

func runHostCommand(ctx context.Context, specification HostCommand) (result HostResult, resultErr error) {
	if specification.Path == "" || len(specification.Argv) == 0 {
		return HostResult{ExitStatus: -1}, errors.New("host command requires a path and argv")
	}
	job, err := createWindowsJob()
	if err != nil {
		return HostResult{ExitStatus: -1}, err
	}
	defer windows.CloseHandle(job)
	command, streams, err := startSuspendedWindowsCommand(specification)
	if err != nil {
		return HostResult{ExitStatus: -1}, err
	}
	result.Started, result.ExitStatus = true, -1
	if err := assignAndResumeWindowsCommand(job, command); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = command.Wait()
		return result, errors.Join(err, streams.wait())
	}
	waitErr, cancellationErr := waitWindowsCommand(ctx, job, command)
	if command.ProcessState != nil {
		result.ExitStatus = command.ProcessState.ExitCode()
	}
	terminateErr := windows.TerminateJobObject(job, 1)
	if errors.Is(terminateErr, windows.ERROR_ACCESS_DENIED) {
		terminateErr = nil
	}
	quietErr := waitForEmptyWindowsJob(job)
	streamErr := streams.wait()
	result.Quiescent = terminateErr == nil && quietErr == nil && streamErr == nil
	return result, errors.Join(cancellationErr, waitErr, terminateErr, quietErr, streamErr)
}

func startSuspendedWindowsCommand(specification HostCommand) (*exec.Cmd, *windowsOutputStreams, error) {
	command := exec.Command(specification.Path)
	command.Args = append([]string{}, specification.Argv...)
	command.Dir = specification.Directory
	command.Env = append([]string{}, specification.Environment...)
	command.Stdin = specification.Stdin
	streams := windowsOutputStreams{}
	stdout, err := streams.add(specification.Stdout)
	if err != nil {
		return nil, nil, err
	}
	stderr, err := streams.add(specification.Stderr)
	if err != nil {
		streams.closeWriters()
		return nil, nil, errors.Join(err, streams.wait())
	}
	command.Stdout, command.Stderr = stdout, stderr
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP}
	if err := command.Start(); err != nil {
		streams.closeWriters()
		return nil, nil, errors.Join(err, streams.wait())
	}
	streams.closeWriters()
	return command, &streams, nil
}

func assignAndResumeWindowsCommand(job windows.Handle, command *exec.Cmd) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(command.Process.Pid))
	if err == nil {
		defer windows.CloseHandle(process)
		err = windows.AssignProcessToJobObject(job, process)
	}
	if err != nil {
		_ = command.Process.Kill()
		return fmt.Errorf("assign process to Job Object: %w", err)
	}
	thread, err := suspendedProcessThread(uint32(command.Process.Pid))
	if err != nil {
		return err
	}
	_, resumeErr := windows.ResumeThread(thread)
	windows.CloseHandle(thread)
	if resumeErr != nil {
		return fmt.Errorf("resume contained process: %w", resumeErr)
	}
	return nil
}

func waitWindowsCommand(ctx context.Context, job windows.Handle, command *exec.Cmd) (error, error) {
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	var waitErr, cancellationErr error
	select {
	case waitErr = <-wait:
	case <-ctx.Done():
		cancellationErr = ctx.Err()
		_ = windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(command.Process.Pid))
		select {
		case waitErr = <-wait:
		case <-time.After(commandCleanupDelay):
			_ = windows.TerminateJobObject(job, 1)
			waitErr = <-wait
		}
	}
	return waitErr, cancellationErr
}

func createWindowsJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create Job Object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("configure Job Object: %w", err)
	}
	return job, nil
}

func suspendedProcessThread(processID uint32) (windows.Handle, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID == processID {
			return windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		}
	}
	return 0, fmt.Errorf("find suspended primary thread for process %d: %w", processID, err)
}

func waitForEmptyWindowsJob(job windows.Handle) error {
	deadline := time.Now().Add(commandCleanupDelay)
	for {
		info := jobBasicAccounting{}
		if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
			return err
		}
		if info.ActiveProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("Windows Job Object did not become quiescent")
		}
		time.Sleep(windowsJobPollInterval)
	}
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
func lookPathOnHost(name string) (string, error) { return exec.LookPath(name) }

type windowsOutputStreams struct {
	writers []*os.File
	waiters sync.WaitGroup
	mu      sync.Mutex
	errs    []error
}

func (streams *windowsOutputStreams) add(output io.Writer) (*os.File, error) {
	if output == nil {
		return nil, nil
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	streams.writers = append(streams.writers, writer)
	streams.waiters.Add(1)
	go func() {
		defer streams.waiters.Done()
		defer reader.Close()
		if _, err := io.Copy(output, reader); err != nil {
			streams.mu.Lock()
			streams.errs = append(streams.errs, err)
			streams.mu.Unlock()
		}
	}()
	return writer, nil
}
func (streams *windowsOutputStreams) closeWriters() {
	for _, writer := range streams.writers {
		_ = writer.Close()
	}
}
func (streams *windowsOutputStreams) wait() error {
	streams.waiters.Wait()
	streams.mu.Lock()
	defer streams.mu.Unlock()
	return errors.Join(streams.errs...)
}
