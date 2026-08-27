//go:build unix

package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHostRuntimePreservesProcessContract(t *testing.T) {
	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	result, err := Run(context.Background(), HostCommand{
		Path:        os.Args[0],
		Argv:        []string{"code polishy helper 雪", "-test.run=^TestHostRuntimeContractHelper$", "argument-λ"},
		Directory:   root,
		Environment: hostTestEnvironment(t, "contract", map[string]string{"CODE_POLISHY_ENVIRONMENT": "environment-η"}),
		Stdin:       strings.NewReader("input-π"),
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	if err == nil || !result.Started || result.ExitStatus != 23 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if stdout.String() != resolvedRoot+"|argument-λ|input-π" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "diagnostic-δ" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestHostRuntimeDistinguishesStartupFailure(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent")
	_, _ = Run(context.Background(), HostCommand{Path: absent, Argv: []string{"absent"}, Environment: os.Environ()})
	before := openDescriptorCount(t)
	result, err := Run(context.Background(), HostCommand{
		Path: absent, Argv: []string{"absent"},
		Environment: os.Environ(), Stdout: io.Discard, Stderr: io.Discard,
	})
	if err == nil || result.Started || result.ExitStatus != -1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if after := openDescriptorCount(t); after != before {
		t.Fatalf("startup failure leaked descriptors: before=%d after=%d", before, after)
	}
}

func TestHostRuntimeContractHelper(t *testing.T) {
	if os.Getenv("CODE_POLISHY_HOST_HELPER") != "contract" {
		return
	}
	if os.Getenv("CODE_POLISHY_ENVIRONMENT") != "environment-η" {
		fmt.Fprint(os.Stderr, "environment was not preserved")
		os.Exit(69)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(70)
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(71)
	}
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "argv=%q", os.Args)
		os.Exit(72)
	}
	fmt.Fprintf(os.Stdout, "%s|%s|%s", workingDirectory, os.Args[2], input)
	fmt.Fprint(os.Stderr, "diagnostic-δ")
	os.Exit(23)
}

func TestHostRuntimeRejectsIncompleteCommand(t *testing.T) {
	result, err := Run(context.Background(), HostCommand{})
	if err == nil || result.Started || result.ExitStatus != -1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestHostRuntimeDetectsProcessGroupEscape(t *testing.T) {
	environment := hostTestEnvironment(t, "escape", nil)
	var stdout, stderr bytes.Buffer
	result, err := Run(context.Background(), HostCommand{
		Path:        os.Args[0],
		Argv:        []string{"code-polishy escape helper", "-test.run=^TestHostRuntimeEscapeHelper$"},
		Environment: environment,
		Stdout:      &stdout,
		Stderr:      &stderr,
	})
	escapedProcessID, conversionErr := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if conversionErr != nil {
		t.Fatalf("escaped process id %q: %v", stdout.String(), conversionErr)
	}
	defer func() {
		if killErr := syscall.Kill(escapedProcessID, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			t.Errorf("kill escaped helper: %v", killErr)
		}
	}()
	if !errors.Is(err, errProcessGroupEscape) || !result.Started || result.Quiescent || result.ExitStatus != 0 {
		t.Fatalf("result=%+v error=%v stderr=%q", result, err, stderr.String())
	}
}

func TestHostRuntimeCancellationAndEscalation(t *testing.T) {
	t.Run("cooperative", func(t *testing.T) {
		root := t.TempDir()
		ready := filepath.Join(root, "ready")
		cooperated := filepath.Join(root, "cooperated")
		environment := hostTestEnvironment(t, "cooperative", map[string]string{"CODE_POLISHY_READY": ready, "CODE_POLISHY_COOPERATED": cooperated})
		contextWithCancel, cancel := context.WithCancel(context.Background())
		defer cancel()
		resultChannel := make(chan struct {
			result HostResult
			err    error
		}, 1)
		go func() {
			result, err := Run(contextWithCancel, HostCommand{
				Path: os.Args[0], Argv: []string{"code-polishy signal helper", "-test.run=^TestHostRuntimeSignalHelper$"},
				Environment: environment,
				Stdout:      io.Discard, Stderr: io.Discard,
			})
			resultChannel <- struct {
				result HostResult
				err    error
			}{result, err}
		}()
		waitForHostMarker(t, ready)
		cancel()
		outcome := <-resultChannel
		if !errors.Is(outcome.err, context.Canceled) || !outcome.result.Started || !outcome.result.Quiescent || outcome.result.ExitStatus != 0 {
			t.Fatalf("result=%+v error=%v", outcome.result, outcome.err)
		}
		if _, err := os.Stat(cooperated); err != nil {
			t.Fatalf("cooperative process did not handle TERM: %v", err)
		}
	})

	t.Run("ignored term", func(t *testing.T) {
		ready := filepath.Join(t.TempDir(), "ready")
		environment := hostTestEnvironment(t, "ignore-term", map[string]string{"CODE_POLISHY_READY": ready})
		contextWithCancel, cancel := context.WithCancel(context.Background())
		defer cancel()
		resultChannel := make(chan struct {
			result HostResult
			err    error
		}, 1)
		go func() {
			result, err := Run(contextWithCancel, HostCommand{
				Path: os.Args[0], Argv: []string{"code-polishy signal helper", "-test.run=^TestHostRuntimeSignalHelper$"},
				Environment: environment,
				Stdout:      io.Discard, Stderr: io.Discard,
			})
			resultChannel <- struct {
				result HostResult
				err    error
			}{result, err}
		}()
		waitForHostMarker(t, ready)
		cancel()
		outcome := <-resultChannel
		if !errors.Is(outcome.err, context.Canceled) || !outcome.result.Started || !outcome.result.Quiescent || outcome.result.ExitStatus != -1 {
			t.Fatalf("result=%+v error=%v", outcome.result, outcome.err)
		}
	})
}

func TestHostRuntimeCleansLeaderFirstProcessTreeBeforeReturn(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(root, "tree-ready")
	survivor := filepath.Join(root, "survivor")
	lockPath := filepath.Join(root, "tree.lock")
	var stdout bytes.Buffer
	result, err := Run(context.Background(), HostCommand{
		Path: os.Args[0], Argv: []string{"code-polishy tree helper", "-test.run=^TestHostRuntimeTreeHelper$"},
		Environment: hostTestEnvironment(t, "tree-leader", map[string]string{
			"CODE_POLISHY_READY": ready, "CODE_POLISHY_SURVIVOR": survivor, "CODE_POLISHY_LOCK": lockPath,
		}),
		Stdout: &stdout, Stderr: io.Discard,
	})
	if err != nil || !result.Started || !result.Quiescent || result.ExitStatus != 0 || stdout.String() != "leader exited\n" {
		t.Fatalf("result=%+v error=%v stdout=%q", result, err, stdout.String())
	}
	lock, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("descendant-held lock remained after Run: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := os.Stat(survivor); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("grandchild survived Run: %v", err)
	}
}

func TestHostRuntimeSignalHelper(t *testing.T) {
	mode := os.Getenv("CODE_POLISHY_HOST_HELPER")
	if mode != "cooperative" && mode != "ignore-term" {
		return
	}
	if mode == "ignore-term" {
		signal.Ignore(syscall.SIGTERM)
		writeHostMarker(os.Getenv("CODE_POLISHY_READY"))
		for {
			time.Sleep(time.Hour)
		}
	}
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	writeHostMarker(os.Getenv("CODE_POLISHY_READY"))
	<-term
	writeHostMarker(os.Getenv("CODE_POLISHY_COOPERATED"))
	os.Exit(0)
}

func TestHostRuntimeTreeHelper(t *testing.T) {
	role := os.Getenv("CODE_POLISHY_HOST_HELPER")
	if role != "tree-leader" && role != "tree-child" && role != "tree-grandchild" {
		return
	}
	if role == "tree-grandchild" {
		time.Sleep(time.Second)
		writeHostMarker(os.Getenv("CODE_POLISHY_SURVIVOR"))
		for {
			time.Sleep(time.Hour)
		}
	}
	var inherited []*os.File
	if role == "tree-leader" {
		lock, err := os.OpenFile(os.Getenv("CODE_POLISHY_LOCK"), os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil || syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) != nil {
			os.Exit(73)
		}
		inherited = []*os.File{duplicateHostDescriptor(3), lock}
	} else {
		inherited = []*os.File{duplicateHostDescriptor(3), duplicateHostDescriptor(4)}
	}
	nextRole := "tree-child"
	if role == "tree-child" {
		nextRole = "tree-grandchild"
	}
	command := exec.Command(os.Getenv("CODE_POLISHY_TEST_EXECUTABLE"), "-test.run=^TestHostRuntimeTreeHelper$")
	command.Env = replaceEnvironment(os.Environ(), map[string]string{"CODE_POLISHY_HOST_HELPER": nextRole})
	command.ExtraFiles = inherited
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		os.Exit(74)
	}
	if role == "tree-child" {
		writeHostMarker(os.Getenv("CODE_POLISHY_READY"))
		for {
			time.Sleep(time.Hour)
		}
	}
	waitForHostMarkerProcess(os.Getenv("CODE_POLISHY_READY"))
	fmt.Fprintln(os.Stdout, "leader exited")
	os.Exit(0)
}

func TestHostRuntimeEscapeHelper(t *testing.T) {
	if os.Getenv("CODE_POLISHY_HOST_HELPER") != "escape" {
		return
	}
	witnessDescriptor, err := syscall.Dup(3)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(69)
	}
	witness := os.NewFile(uintptr(witnessDescriptor), "process-tree-witness")
	command := exec.Command(os.Getenv("CODE_POLISHY_TEST_EXECUTABLE"), "-test.run=^TestHostRuntimeEscapedDescendant$")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CODE_POLISHY_HOST_HELPER=") {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, "CODE_POLISHY_HOST_HELPER=escaped-descendant")
	command.ExtraFiles = []*os.File{witness}
	command.Stdin = nil
	command.Stdout = nil
	command.Stderr = nil
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(70)
	}
	fmt.Fprintln(os.Stdout, command.Process.Pid)
	os.Exit(0)
}

func TestHostRuntimeEscapedDescendant(t *testing.T) {
	if os.Getenv("CODE_POLISHY_HOST_HELPER") != "escaped-descendant" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}

func hostTestEnvironment(t *testing.T, helper string, additions map[string]string) []string {
	t.Helper()
	stubDirectory := t.TempDir()
	for _, name := range []string{"ps", "setsid"} {
		if err := os.WriteFile(filepath.Join(stubDirectory, name), []byte("#!/bin/sh\nexit 97\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	overrides := map[string]string{
		"PATH":                         stubDirectory + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CODE_POLISHY_HOST_HELPER":     helper,
		"CODE_POLISHY_TEST_EXECUTABLE": os.Args[0],
	}
	for name, value := range additions {
		overrides[name] = value
	}
	return replaceEnvironment(os.Environ(), overrides)
}

func replaceEnvironment(environment []string, overrides map[string]string) []string {
	result := make([]string, 0, len(environment)+len(overrides))
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, replaced := overrides[name]; !replaced {
			result = append(result, entry)
		}
	}
	for name, value := range overrides {
		result = append(result, name+"="+value)
	}
	return result
}

func duplicateHostDescriptor(descriptor int) *os.File {
	duplicate, err := syscall.Dup(descriptor)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(76)
	}
	return os.NewFile(uintptr(duplicate), "host-runtime-inherited")
}

func writeHostMarker(path string) {
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(77)
	}
}

func waitForHostMarker(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for helper marker %s", path)
}

func waitForHostMarkerProcess(path string) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(78)
}

func openDescriptorCount(t *testing.T) int {
	t.Helper()
	count := 0
	for descriptor := 0; descriptor < 256; descriptor++ {
		_, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(descriptor), uintptr(syscall.F_GETFD), 0)
		if errno == 0 {
			count++
		}
	}
	return count
}
