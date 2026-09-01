package runner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const successfulCommandTimeoutSeconds = 30

func TestOSRunnerExecutesArgumentArray(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	script := filepath.Join(root, "record.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s' \"$1\" > result\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := OSRunner{}
	command := policy.Command{Name: "record", Argv: []string{"./record.sh", "literal;not-shell"}, Cwd: ".", TimeoutSeconds: successfulCommandTimeoutSeconds}
	if err := runner.Run(context.Background(), root, command); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "result"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "literal;not-shell" {
		t.Fatalf("result = %q", data)
	}
}

func TestOSRunnerCapturesStructuredOutputThroughTheCommonBoundary(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	script := filepath.Join(root, "report.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nprintf 'report'\nprintf 'diagnostic' >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var configuredStdout, configuredStderr bytes.Buffer
	result, output, err := (OSRunner{Stdout: &configuredStdout, Stderr: &configuredStderr}).RunWithOutput(context.Background(), root, policy.Command{
		Name: "report", Argv: []string{"./report.sh"}, Cwd: ".", TimeoutSeconds: successfulCommandTimeoutSeconds,
	})
	if err != nil || result.ExitStatus != 0 || string(output.Stdout) != "report" || string(output.Stderr) != "diagnostic" {
		t.Fatalf("result=%+v output=%+v error=%v", result, output, err)
	}
	if configuredStdout.String() != "report" || configuredStderr.String() != "diagnostic" {
		t.Fatalf("configured output stdout=%q stderr=%q", configuredStdout.String(), configuredStderr.String())
	}
}

func TestOSRunnerKeepsStructuredProtocolOutputOutOfHumanOutput(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	script := filepath.Join(root, "protocol.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nprintf 'response'\nprintf 'diagnostic' >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var configuredStdout, configuredStderr bytes.Buffer
	result, output, err := (OSRunner{Stdout: &configuredStdout, Stderr: &configuredStderr}).RunStructured(context.Background(), root, policy.Command{
		Name: "protocol", Argv: []string{"./protocol.sh"}, Cwd: ".", TimeoutSeconds: successfulCommandTimeoutSeconds,
	})
	if err != nil || result.ExitStatus != 0 || string(output.Stdout) != "response" || string(output.Stderr) != "diagnostic" {
		t.Fatalf("result=%+v output=%+v error=%v", result, output, err)
	}
	if configuredStdout.Len() != 0 || configuredStderr.String() != "diagnostic" {
		t.Fatalf("configured output stdout=%q stderr=%q", configuredStdout.String(), configuredStderr.String())
	}
}

func TestOSRunnerRunWithWritersUsesOnlyTheRequestedDestinations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	script := filepath.Join(root, "redirect.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nprintf 'report'\nprintf 'diagnostic' >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var configuredStdout, configuredStderr, requestedStdout, requestedStderr bytes.Buffer
	result, err := (OSRunner{Stdout: &configuredStdout, Stderr: &configuredStderr}).RunWithWriters(
		context.Background(),
		root,
		policy.Command{Name: "redirect", Argv: []string{"./redirect.sh"}, Cwd: ".", TimeoutSeconds: successfulCommandTimeoutSeconds},
		&requestedStdout,
		&requestedStderr,
	)
	if err != nil || result.ExitStatus != 0 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if requestedStdout.String() != "report" || requestedStderr.String() != "diagnostic" {
		t.Fatalf("requested output stdout=%q stderr=%q", requestedStdout.String(), requestedStderr.String())
	}
	if configuredStdout.Len() != 0 || configuredStderr.Len() != 0 {
		t.Fatalf("configured output stdout=%q stderr=%q", configuredStdout.String(), configuredStderr.String())
	}
}

func TestOSRunnerBoundsStructuredOutputCapture(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "large-report.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nhead -c 8388609 /dev/zero\nhead -c 65537 /dev/zero >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, output, err := (OSRunner{}).RunWithOutput(context.Background(), root, policy.Command{
		Name: "large-report", Argv: []string{"./large-report.sh"}, Cwd: ".", TimeoutSeconds: successfulCommandTimeoutSeconds,
	})
	if err == nil || !strings.Contains(err.Error(), "captured structured output") {
		t.Fatalf("large structured output error = %v", err)
	}

	if result.ExitStatus != 0 || len(output.Stdout) != maximumStructuredOutputBytes || len(output.Stderr) != maximumStructuredDiagnosticBytes {
		t.Fatalf("large structured output result=%+v stdout=%d stderr=%d", result, len(output.Stdout), len(output.Stderr))
	}
}

func TestOSRunnerRejectsEscapingWorkingDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	err := (OSRunner{}).Run(context.Background(), root, policy.Command{Name: "escape", Argv: []string{"true"}, Cwd: "escape", TimeoutSeconds: 5})
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected containment error, got %v", err)
	}
}

func TestOSRunnerTimesOut(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	script := filepath.Join(root, "wait.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nsleep 5\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	status, err := (OSRunner{}).RunWithStatus(context.Background(), root, policy.Command{Name: "wait", Argv: []string{"./wait.sh"}, Cwd: ".", TimeoutSeconds: 1})
	if err == nil || status != 124 || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("status=%d error=%v", status, err)
	}
}

func TestOSRunnerClassifiesObservedFailures(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "failure.sh"), []byte("#!/bin/sh\nexit 17\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wait.sh"), []byte("#!/bin/sh\nsleep 5\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name     string
		ctx      context.Context
		command  policy.Command
		category FailureCategory
	}{
		{
			name: "command exit", ctx: context.Background(),
			command: policy.Command{Name: "exit", Argv: []string{"./failure.sh"}, Cwd: ".", TimeoutSeconds: 5}, category: FailureCommandExit,
		},
		{
			name: "timeout", ctx: context.Background(),
			command: policy.Command{Name: "timeout", Argv: []string{"./wait.sh"}, Cwd: ".", TimeoutSeconds: 1}, category: FailureTimeout,
		},
		{
			name: "canceled", ctx: canceled,
			command: policy.Command{Name: "canceled", Argv: []string{"./failure.sh"}, Cwd: ".", TimeoutSeconds: 5}, category: FailureCanceled,
		},
		{
			name: "environment", ctx: context.Background(),
			command: policy.Command{Name: "environment", Argv: []string{"./absent.sh"}, Cwd: ".", TimeoutSeconds: 5}, category: FailureEnvironment,
		},
		{
			name: "resource", ctx: context.Background(),
			command: policy.Command{Name: "resource", Argv: []string{"./failure.sh"}, Cwd: ".", ExclusiveResources: []string{"z", "a"}, TimeoutSeconds: 5}, category: FailureResource,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := (OSRunner{}).RunWithResult(testCase.ctx, root, testCase.command)
			if err == nil || result.FailureCategory != testCase.category || FailureCategoryFor(testCase.ctx, result, err) != testCase.category {
				t.Fatalf("result=%+v error=%v category=%q", result, err, FailureCategoryFor(testCase.ctx, result, err))
			}
		})
	}
}

func TestFailureCategoryForFallsBackOnlyToObservedFacts(t *testing.T) {
	if category := FailureCategoryFor(context.Background(), Result{ExitStatus: 3}, errors.New("failed")); category != FailureCommandExit {
		t.Fatalf("exit category = %q", category)
	}
	if category := FailureCategoryFor(context.Background(), Result{}, errors.New("failed")); category != FailureOperational {
		t.Fatalf("unknown category = %q", category)
	}
}

func TestOSRunnerReportsProcessStatus(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	script := filepath.Join(root, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nexit 17\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	status, err := (OSRunner{}).RunWithStatus(context.Background(), root, policy.Command{
		Name: "fail", Argv: []string{"./fail.sh"}, Cwd: ".", TimeoutSeconds: 5,
	})
	if err == nil || status != 17 {
		t.Fatalf("status=%d error=%v", status, err)
	}
}

func TestOSRunnerOnlyForwardsRequestedSensitiveEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODE_POLISHY_TEST_SECRET", "sensitive")
	script := filepath.Join(root, "environment.sh")
	contents := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s' \"${CODE_POLISHY_TEST_SECRET:-missing}\" > result\n"
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(name string, environment []string) string {
		t.Helper()
		command := policy.Command{Name: name, Argv: []string{"./environment.sh"}, Cwd: ".", Environment: environment, TimeoutSeconds: successfulCommandTimeoutSeconds}
		if err := (OSRunner{}).Run(context.Background(), root, command); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, "result"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if got := run("without-secret", nil); got != "missing" {
		t.Fatalf("unrequested secret was inherited: %q", got)
	}
	if got := run("with-secret", []string{"CODE_POLISHY_TEST_SECRET"}); got != "sensitive" {
		t.Fatalf("requested secret was not inherited: %q", got)
	}
}

func TestOSRunnerBuildsASealedCommandEnvironment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", "/unsafe-home")
	t.Setenv("CODE_POLISHY_TEST_SECRET", "sensitive")
	script := filepath.Join(root, "environment.sh")
	contents := "#!/bin/sh\nprintf '%s|%s|%s|%s' \"${CODE_POLISHY_TEST_SECRET:-missing}\" \"${HOME}\" \"${TMPDIR}\" \"${PATH}\" > result\n"
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := (OSRunner{}).Run(context.Background(), root, policy.Command{
		Name: "sealed-environment", Argv: []string{"./environment.sh"}, Cwd: ".", SealedEnvironment: true,
		TimeoutSeconds: successfulCommandTimeoutSeconds,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := os.ReadFile(filepath.Join(root, "result"))
	if err != nil {
		t.Fatal(err)
	}
	values := strings.Split(string(result), "|")
	if len(values) != 4 || values[0] != "missing" || values[1] == "/unsafe-home" || values[2] == "" || values[3] != "" {
		t.Fatalf("sealed environment = %q", result)
	}
}

func TestOSRunnerCancellationCancelsDescendants(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(root, "ready")
	release := filepath.Join(root, "release")
	survivor := filepath.Join(root, "survivor")
	script := filepath.Join(root, "descendant.sh")
	contents := "#!/bin/sh\n(: > ready; while [ ! -e release ]; do sleep 0.05; done; : > survivor) &\nwait\n"
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- (OSRunner{}).Run(ctx, root, policy.Command{
			Name: "descendant", Argv: []string{"./descendant.sh"}, Cwd: ".", TimeoutSeconds: successfulCommandTimeoutSeconds,
		})
	}()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			cancel()
			<-result
			t.Fatal(err)
		}
		select {
		case err := <-result:
			t.Fatalf("descendant fixture exited before starting: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err := <-result; err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("descendant command was not canceled: %v", err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(survivor); err == nil {
			t.Fatal("descendant survived governed command cancellation")
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOSRunnerSuccessWithInheritedOutputCancelsDescendantsBeforeReleasingResources(t *testing.T) {
	root := t.TempDir()
	survivor := filepath.Join(root, "survivor")
	script := filepath.Join(root, "background.sh")
	contents := "#!/bin/sh\n(sleep 1; : > survivor) &\nexit 0\n"
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	resource := "success-cleanup-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	result, output, err := (OSRunner{}).RunWithOutput(context.Background(), root, policy.Command{
		Name: "background", Argv: []string{"./background.sh"}, Cwd: ".", ExclusiveResources: []string{resource}, TimeoutSeconds: successfulCommandTimeoutSeconds,
	})
	if err != nil {
		t.Fatalf("background command: %v", err)
	}
	if result.ExitStatus != 0 || len(output.Stdout) != 0 || len(output.Stderr) != 0 {
		t.Fatalf("background result=%+v output=%+v", result, output)
	}
	after := filepath.Join(root, "after-background.sh")
	if err := os.WriteFile(after, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := (OSRunner{}).Run(context.Background(), root, policy.Command{
		Name: "after-background", Argv: []string{"./after-background.sh"}, Cwd: ".", ExclusiveResources: []string{resource}, TimeoutSeconds: successfulCommandTimeoutSeconds,
	}); err != nil {
		t.Fatalf("resource remained locked after successful cleanup: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := os.Stat(survivor); err == nil {
		t.Fatal("descendant survived successful governed command")
	}
}

func TestEnvironmentForwardsThePolicyOwnedStaticcheckCache(t *testing.T) {
	t.Setenv("STATICCHECK_CACHE", t.TempDir())
	forwarded := Environment(nil)
	if !slices.Contains(forwarded, "STATICCHECK_CACHE="+os.Getenv("STATICCHECK_CACHE")) {
		t.Fatalf("environment omitted STATICCHECK_CACHE: %v", forwarded)
	}
}

func TestOSRunnerPrependsPinnedToolDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	toolDirectory := filepath.Join(root, "toolchain")
	if err := os.MkdirAll(toolDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(toolDirectory, "policy-go-helper")
	contents := "#!/usr/bin/env bash\nset -euo pipefail\ncommand -v policy-go-helper > result\n"
	if err := os.WriteFile(helper, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	commandRunner := OSRunner{PathEntries: []string{toolDirectory}}
	command := policy.Command{Name: "find-helper", Argv: []string{"policy-go-helper"}, Cwd: ".", TimeoutSeconds: successfulCommandTimeoutSeconds}
	if err := commandRunner.Run(context.Background(), root, command); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "result"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != helper {
		t.Fatalf("helper path = %q", data)
	}
}

func TestExclusiveResourcesSerializeAcrossProcessesAndReleaseAfterFailureOrCancellation(t *testing.T) {
	resource := "performance-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	firstRoot, secondRoot := resourceFixtureRepository(t), resourceFixtureRepository(t)
	firstMarker := filepath.Join(t.TempDir(), "first")
	secondMarker := filepath.Join(t.TempDir(), "second")
	first := startResourceHelper(t, firstRoot, firstMarker, resource, "1", "0")
	waitForResourceMarker(t, firstMarker)
	second := startResourceHelper(t, secondRoot, secondMarker, resource, "0", "0")
	ensureResourceMarkerAbsent(t, secondMarker, 200*time.Millisecond)
	if err := first.Wait(); err != nil {
		t.Fatalf("first repository command failed: %v", err)
	}
	if err := second.Wait(); err != nil {
		t.Fatalf("second repository command failed: %v", err)
	}
	waitForResourceMarker(t, secondMarker)

	failingMarker := filepath.Join(t.TempDir(), "failing")
	failing := startResourceHelper(t, resourceFixtureRepository(t), failingMarker, resource, "0", "17")
	if err := failing.Wait(); err == nil {
		t.Fatal("failing resource holder succeeded")
	}
	waitForResourceMarker(t, failingMarker)
	afterFailure := filepath.Join(t.TempDir(), "after-failure")
	afterFailureCommand := startResourceHelper(t, resourceFixtureRepository(t), afterFailure, resource, "0", "0")
	if err := afterFailureCommand.Wait(); err != nil {
		t.Fatalf("resource remained locked after failure: %v", err)
	}
	waitForResourceMarker(t, afterFailure)

	cancelRoot := resourceFixtureRepository(t)
	contextWithTimeout, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	command := resourceCommand("cancelled", resource)
	command.TimeoutSeconds = 1
	t.Setenv("CODE_POLISHY_RESOURCE_MARKER", filepath.Join(t.TempDir(), "cancelled"))
	t.Setenv("CODE_POLISHY_RESOURCE_SLEEP", "5")
	t.Setenv("CODE_POLISHY_RESOURCE_EXIT", "0")
	result, err := (OSRunner{}).RunWithResult(contextWithTimeout, cancelRoot, command)
	if err == nil || result.ExitStatus != 124 {
		t.Fatalf("cancelled command result=%+v error=%v", result, err)
	}
	afterCancellation := filepath.Join(t.TempDir(), "after-cancellation")
	afterCancellationCommand := startResourceHelper(t, resourceFixtureRepository(t), afterCancellation, resource, "0", "0")
	if err := afterCancellationCommand.Wait(); err != nil {
		t.Fatalf("resource remained locked after cancellation: %v", err)
	}
	waitForResourceMarker(t, afterCancellation)
}

func TestResourceLockNamespaceIsStablePerUserAndWaitsReportPeriodically(t *testing.T) {
	resource := "performance-progress-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if resourceLockNamespaceForUser(1000) == resourceLockNamespaceForUser(1001) {
		t.Fatal("resource lock namespace is not partitioned by Unix user")
	}
	root := resourceFixtureRepository(t)
	marker := filepath.Join(t.TempDir(), "holder")
	holder := startResourceHelper(t, root, marker, resource, "2", "0")
	waitForResourceMarker(t, marker)
	updates := make(chan time.Duration, 2)
	contextWithProgress := WithResourceWaitObserver(context.Background(), func(elapsed time.Duration) {
		updates <- elapsed
	})
	t.Setenv("CODE_POLISHY_RESOURCE_MARKER", filepath.Join(t.TempDir(), "waiter"))
	t.Setenv("CODE_POLISHY_RESOURCE_SLEEP", "0")
	t.Setenv("CODE_POLISHY_RESOURCE_EXIT", "0")
	result, err := (OSRunner{}).RunWithResult(contextWithProgress, root, resourceCommand("waiter", resource))
	if err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err != nil {
		t.Fatalf("resource holder failed: %v", err)
	}
	if result.ResourceWait < resourceWaitProgressInterval {
		t.Fatalf("resource wait = %s, want at least %s", result.ResourceWait, resourceWaitProgressInterval)
	}
	select {
	case elapsed := <-updates:
		if elapsed < resourceWaitProgressInterval {
			t.Fatalf("reported resource wait = %s, want at least %s", elapsed, resourceWaitProgressInterval)
		}
	default:
		t.Fatal("resource wait emitted no periodic progress update")
	}
}

func TestResourceProcessHelper(t *testing.T) {
	if os.Getenv("CODE_POLISHY_RESOURCE_HELPER") != "1" {
		return
	}
	command := resourceCommand("resource-helper", os.Getenv("CODE_POLISHY_RESOURCE_NAME"))
	if _, err := (OSRunner{}).RunWithResult(context.Background(), os.Getenv("CODE_POLISHY_RESOURCE_ROOT"), command); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func resourceFixtureRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	script := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s' \"$$\" >\"${CODE_POLISHY_RESOURCE_MARKER}\"\nsleep \"${CODE_POLISHY_RESOURCE_SLEEP}\"\nexit \"${CODE_POLISHY_RESOURCE_EXIT}\"\n"
	if err := os.WriteFile(filepath.Join(root, "hold.sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func resourceCommand(name, resource string) policy.Command {
	return policy.Command{
		Name: name, Argv: []string{"./hold.sh"}, Cwd: ".", ExclusiveResources: []string{resource}, TimeoutSeconds: successfulCommandTimeoutSeconds,
		Environment: []string{"CODE_POLISHY_RESOURCE_MARKER", "CODE_POLISHY_RESOURCE_SLEEP", "CODE_POLISHY_RESOURCE_EXIT"},
	}
}

func startResourceHelper(t *testing.T, root, marker, resource, sleep, exit string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestResourceProcessHelper$")
	command.Env = append(os.Environ(),
		"CODE_POLISHY_RESOURCE_HELPER=1",
		"CODE_POLISHY_RESOURCE_ROOT="+root,
		"CODE_POLISHY_RESOURCE_MARKER="+marker,
		"CODE_POLISHY_RESOURCE_NAME="+resource,
		"CODE_POLISHY_RESOURCE_SLEEP="+sleep,
		"CODE_POLISHY_RESOURCE_EXIT="+exit,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	return command
}

func waitForResourceMarker(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("resource marker %s was not written", marker)
}

func ensureResourceMarkerAbsent(t *testing.T, marker string, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("resource marker %s appeared before its predecessor released the lock", marker)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestToolOutputUsesRestrictedEnvironment(t *testing.T) {
	t.Setenv("CODE_POLISHY_UNFORWARDED_SECRET", "hidden")
	root := t.TempDir()
	script := filepath.Join(root, "tool-output.sh")
	contents := "#!/usr/bin/env bash\nset -euo pipefail\nprintf '%s' \"${CODE_POLISHY_UNFORWARDED_SECRET:-missing}\"\n"
	if err := os.WriteFile(script, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := ToolOutput(script)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "missing" {
		t.Fatalf("unexpected forwarded environment: %q", output)
	}
}

func TestGoModuleVersionReadsTheBinaryRatherThanRunningIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	goTool := filepath.Join(root, "go")
	contents := "#!/usr/bin/env bash\n" +
		"printf '%s: go1.26.6\\n\\tpath\\tgolang.org/x/vuln/cmd/govulncheck\\n" +
		"\\tmod\\tgolang.org/x/vuln\\tv1.3.0\\th1:example\\n' \"$3\"\n"
	if err := os.WriteFile(goTool, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "govulncheck")
	if err := os.WriteFile(binary, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := GoModuleVersion(goTool, binary); got != "1.3.0" {
		t.Fatalf("GoModuleVersion = %q, want 1.3.0", got)
	}

	if got := GoModuleVersion(filepath.Join(root, "absent"), binary); got != "" {
		t.Fatalf("a missing toolchain reported %q", got)
	}
}
