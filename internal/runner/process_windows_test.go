//go:build windows

package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsJobUsesKillOnClose(t *testing.T) {
	job, err := createWindowsJob()
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(job)
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	if err := windows.QueryInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
		t.Fatal(err)
	}
	if info.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE == 0 {
		t.Fatal("Job Object lacks kill-on-close")
	}
}

func TestWindowsSuspendedLeaderIsAssignedToJobBeforeResume(t *testing.T) {
	job, err := createWindowsJob()
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(job)
	command, streams, err := startSuspendedWindowsCommand(HostCommand{
		Path: os.Args[0], Argv: []string{os.Args[0], "-test.run=^TestWindowsHostCancellationHelper$"},
		Environment: windowsHostTestEnvironment("cancel", map[string]string{"CODE_POLISHY_READY": filepath.Join(t.TempDir(), "ready")}), Stdout: io.Discard, Stderr: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = windows.TerminateJobObject(job, 1)
		_ = command.Wait()
		_ = streams.wait()
	}()
	if err := assignAndResumeWindowsCommand(job, command); err != nil {
		t.Fatal(err)
	}
	processes := struct {
		Assigned uint32
		Count    uint32
		IDs      [1]uintptr
	}{}
	if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&processes)), uint32(unsafe.Sizeof(processes)), nil); err != nil {
		t.Fatal(err)
	}
	if processes.Assigned != 1 || processes.Count != 1 || processes.IDs[0] != uintptr(command.Process.Pid) {
		t.Fatalf("job membership=%+v leader=%d", processes, command.Process.Pid)
	}
}

func TestWindowsHostRuntimePreservesExitAndRejectsStartupFailure(t *testing.T) {
	result, err := Run(context.Background(), HostCommand{
		Path: os.Args[0], Argv: []string{os.Args[0], "-test.run=^TestWindowsHostExitHelper$"},
		Environment: windowsHostTestEnvironment("exit", nil), Stdout: io.Discard, Stderr: io.Discard,
	})
	if err == nil || !result.Started || !result.Quiescent || result.ExitStatus != 23 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	missing, err := Run(context.Background(), HostCommand{Path: filepath.Join(t.TempDir(), "missing.exe"), Argv: []string{"missing.exe"}})
	if err == nil || missing.Started || missing.Quiescent || missing.ExitStatus != -1 {
		t.Fatalf("missing result=%+v error=%v", missing, err)
	}
}

func TestWindowsHostRuntimeCleansLeaderFirstDescendantsAndHeldOutput(t *testing.T) {
	root := t.TempDir()
	ready, survivor := filepath.Join(root, "ready"), filepath.Join(root, "survivor")
	result, err := Run(context.Background(), HostCommand{
		Path: os.Args[0], Argv: []string{os.Args[0], "-test.run=^TestWindowsHostTreeHelper$"},
		Environment: windowsHostTestEnvironment("leader", map[string]string{"CODE_POLISHY_READY": ready, "CODE_POLISHY_SURVIVOR": survivor}),
		Stdout:      io.Discard, Stderr: io.Discard,
	})
	if err != nil || !result.Started || !result.Quiescent || result.ExitStatus != 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	time.Sleep(700 * time.Millisecond)
	if _, err := os.Stat(survivor); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("grandchild survived Job cleanup: %v", err)
	}
}

func TestWindowsHostRuntimeEscalatesCancellation(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "ready")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		result HostResult
		err    error
	}, 1)
	go func() {
		result, err := Run(ctx, HostCommand{Path: os.Args[0], Argv: []string{os.Args[0], "-test.run=^TestWindowsHostCancellationHelper$"}, Environment: windowsHostTestEnvironment("cancel", map[string]string{"CODE_POLISHY_READY": ready}), Stdout: io.Discard, Stderr: io.Discard})
		done <- struct {
			result HostResult
			err    error
		}{result, err}
	}()
	waitForWindowsMarker(t, ready)
	cancel()
	outcome := <-done
	if !errors.Is(outcome.err, context.Canceled) || !outcome.result.Started || !outcome.result.Quiescent {
		t.Fatalf("result=%+v error=%v", outcome.result, outcome.err)
	}
}

func TestWindowsHostExitHelper(t *testing.T) {
	if os.Getenv("CODE_POLISHY_WINDOWS_HELPER") == "exit" {
		os.Exit(23)
	}
}

func TestWindowsHostTreeHelper(t *testing.T) {
	role := os.Getenv("CODE_POLISHY_WINDOWS_HELPER")
	if role != "leader" && role != "child" && role != "grandchild" {
		return
	}
	if role == "grandchild" {
		time.Sleep(500 * time.Millisecond)
		_ = os.WriteFile(os.Getenv("CODE_POLISHY_SURVIVOR"), []byte("survived"), 0o600)
		for {
			time.Sleep(time.Hour)
		}
	}
	next := "child"
	if role == "child" {
		next = "grandchild"
	}
	command := exec.Command(os.Args[0], "-test.run=^TestWindowsHostTreeHelper$")
	command.Env = windowsHostTestEnvironment(next, nil)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if role == "child" {
		_ = os.WriteFile(os.Getenv("CODE_POLISHY_READY"), []byte("ready"), 0o600)
		for {
			time.Sleep(time.Hour)
		}
	}
	waitForWindowsMarker(t, os.Getenv("CODE_POLISHY_READY"))
}

func TestWindowsHostCancellationHelper(t *testing.T) {
	if os.Getenv("CODE_POLISHY_WINDOWS_HELPER") != "cancel" {
		return
	}
	signal.Ignore(os.Interrupt)
	_ = os.WriteFile(os.Getenv("CODE_POLISHY_READY"), []byte("ready"), 0o600)
	for {
		time.Sleep(time.Hour)
	}
}

func windowsHostTestEnvironment(role string, additions map[string]string) []string {
	replacements := map[string]string{"CODE_POLISHY_WINDOWS_HELPER": role}
	for key, value := range additions {
		replacements[key] = value
	}
	result := make([]string, 0, len(os.Environ())+len(replacements))
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found {
			if _, replace := replacements[name]; replace {
				continue
			}
		}
		result = append(result, entry)
	}
	for key, value := range replacements {
		result = append(result, key+"="+value)
	}
	return result
}

func waitForWindowsMarker(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("marker %s was not created", path)
}
