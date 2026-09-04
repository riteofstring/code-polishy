package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func TestEveryCommandHelpPageHasCompleteContract(t *testing.T) {
	publicCommands := []string{
		"version", "docs", "pack", "agents", "lock", "release-manifest", "change-boundary", "task-session",
		"check", "gate", "checkpoint-gate", "merge-gate", "behavior-review", "regression-proof",
		"test", "test-plan", "test-levels", "test-receipts", "verify", "architecture", "supply-chain",
		"dependency-review", "artifact-security", "doctor", "design-context", "format", "fix", "list-files",
	}
	for _, command := range publicCommands {
		t.Run(command, func(t *testing.T) {
			page, found := commandHelpFor(command)
			if !found {
				t.Fatalf("missing help page for %q", command)
			}
			output := &bytes.Buffer{}
			page.writeTo(output)
			for _, heading := range []string{"Usage:", "Selectors and arguments:", "Side effects:", "Exit status:", "Examples:"} {
				if !strings.Contains(output.String(), heading) {
					t.Fatalf("help page omitted %q: %q", heading, output.String())
				}
			}
		})
	}
}

func TestCommandHelpFormsSkipRepositoryInitialization(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	arguments := [][]string{
		{"--repo-root", missing, "--policy-root", missing, "merge-gate", "--help"},
		{"--repo-root", missing, "--policy-root", missing, "merge-gate", "-h"},
		{"--repo-root", missing, "--policy-root", missing, "help", "merge-gate"},
	}
	outputs := make([]string, 0, len(arguments))
	for _, invocation := range arguments {
		status, stdout, stderr := captureRunOutput(t, invocation)
		if status != 0 || stderr != "" {
			t.Fatalf("%v: status=%d stdout=%q stderr=%q", invocation, status, stdout, stderr)
		}
		outputs = append(outputs, stdout)
	}
	if outputs[0] != outputs[1] || outputs[0] != outputs[2] {
		t.Fatalf("help forms diverged: %q, %q, %q", outputs[0], outputs[1], outputs[2])
	}
	if !strings.Contains(outputs[0], "code-polishy merge-gate --base REF [--resume]") {
		t.Fatalf("merge-gate syntax missing: %q", outputs[0])
	}
}

func TestSupplementalHelpRequiresExplicitSelection(t *testing.T) {
	t.Parallel()
	testPage, found := commandHelpFor("test")
	if !found {
		t.Fatal("test help page is missing")
	}
	testOutput := &bytes.Buffer{}
	testPage.writeTo(testOutput)
	if !strings.Contains(testOutput.String(), "--supplemental is an explicit hardening selection") ||
		!strings.Contains(testOutput.String(), "requiredSupplementalKinds do not invoke it") {
		t.Fatalf("test help does not disclose explicit supplemental selection: %q", testOutput.String())
	}
	for _, command := range []string{"test-plan", "test-levels"} {
		page, found := commandHelpFor(command)
		if !found {
			t.Fatalf("%s help page is missing", command)
		}
		output := &bytes.Buffer{}
		page.writeTo(output)
		if !strings.Contains(output.String(), "supplemental row is informational and never selects supplemental execution") {
			t.Fatalf("%s help does not disclose informational supplemental output: %q", command, output.String())
		}
	}
}

func TestEvaluationCommandHelpDistinguishesDirectoriesAndModules(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"check", "architecture", "format", "fix", "list-files"} {
		page, found := commandHelpFor(command)
		if !found {
			t.Fatalf("%s help page is missing", command)
		}
		output := &bytes.Buffer{}
		page.writeTo(output)
		text := output.String()
		if !strings.Contains(text, "--files") || !strings.Contains(text, "directories") || !strings.Contains(text, "--module") {
			t.Fatalf("%s help does not distinguish file, directory, and module evaluation: %q", command, text)
		}
	}
}

func TestEveryCatalogCommandSupportsEarlyHelp(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for _, page := range commandHelpPages {
		t.Run(page.name, func(t *testing.T) {
			for _, helpOption := range []string{"--help", "-h"} {
				status, stdout, stderr := captureRunOutput(t, []string{
					"--repo-root", missing, "--policy-root", missing, page.name, helpOption,
				})
				if status != 0 || stderr != "" || !strings.Contains(stdout, "Usage:") {
					t.Fatalf("option=%s status=%d stdout=%q stderr=%q", helpOption, status, stdout, stderr)
				}
			}
		})
	}
}

func TestTaskSessionForwardsChildHelpAfterCommandBoundary(t *testing.T) {
	repositoryRoot, _ := newBehaviorReviewCLIRepository(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODE_POLISHY_TEST_TASK_SESSION_HELP_CHILD", "1")
	status, stdout, stderr := captureRunOutput(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", behaviorReviewCLIPolicyRoot(t),
		"task-session", "--module", "application", "--", executable,
		"-test.run=^TestTaskSessionChildHelp$", "--", "--help",
	})
	if status != 0 || stderr != "" || !strings.Contains(stdout, "CHILD HELP FORWARDED") ||
		!strings.Contains(stdout, "Task session passed") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if strings.Contains(stdout, "Run one task command in a disposable Git worktree") {
		t.Fatalf("task-session contextual help intercepted the child: %q", stdout)
	}
}

func TestTaskSessionChildHelp(t *testing.T) {
	if os.Getenv("CODE_POLISHY_TEST_TASK_SESSION_HELP_CHILD") != "1" {
		return
	}
	arguments := flag.Args()
	if len(arguments) != 1 || arguments[0] != "--help" {
		t.Fatalf("child arguments = %q", arguments)
	}
	fmt.Fprintln(os.Stdout, "CHILD HELP FORWARDED")
}

func TestDesignContextUsageSuggestsExplicitFiles(t *testing.T) {
	result, err := handleDesignContext(t.Context(), &engine.Engine{}, []string{"cmd/code-polishy/main.go", "cmd/code-polishy/help.go"})
	if err == nil || !isCommandInputError(err) || result.quiet {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	status, _, stderr := captureCommandUsageError(t, invocation{
		command: "design-context", arguments: []string{"cmd/code-polishy/main.go", "cmd/code-polishy/help.go"},
	}, err.Error())
	if status != 2 ||
		!strings.Contains(stderr, "Did you mean: code-polishy design-context --files cmd/code-polishy/main.go cmd/code-polishy/help.go?") ||
		!strings.Contains(stderr, "Usage:\n  code-polishy design-context --module NAME...") {
		t.Fatalf("status=%d stderr=%q", status, stderr)
	}
}

func TestInvalidSelectionReturnsCommandScopedUsageError(t *testing.T) {
	_, err := handleSelectionCommand("architecture")(t.Context(), &engine.Engine{}, []string{"--files"})
	if err == nil || !isCommandInputError(err) {
		t.Fatalf("err=%v", err)
	}
	status, _, stderr := captureCommandUsageError(t, invocation{command: "architecture", arguments: []string{"--files"}}, err.Error())
	if status != 2 || !strings.Contains(stderr, "Usage:\n  code-polishy architecture") {
		t.Fatalf("status=%d stderr=%q", status, stderr)
	}
}

func TestChangedTestCLIRendersRequestedAndResolvedBase(t *testing.T) {
	repositoryRoot, _ := newBehaviorReviewCLIRepository(t)
	gitBehaviorReviewCLI(t, repositoryRoot, "branch", "TASK_BASE", "main")
	exactBase := gitRevision(t, repositoryRoot, "TASK_BASE")
	status, stdout, stderr := runBehaviorReviewCLI(t, []string{
		"--repo-root", repositoryRoot, "--policy-root", behaviorReviewCLIPolicyRoot(t),
		"test", "--changed", "--base", "TASK_BASE",
	})
	want := "TEST SCOPE: merge-base(TASK_BASE, HEAD) = " + exactBase + " plus working tree"
	if status != 0 || stderr != "" || !strings.Contains(stdout, want) {
		t.Fatalf("status=%d stdout=%q stderr=%q want=%q", status, stdout, stderr, want)
	}
}

func gitRevision(t *testing.T, root, reference string) string {
	t.Helper()
	command := exec.Command("git", "-C", root, "rev-parse", reference)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s: %v", reference, err)
	}
	return strings.TrimSpace(string(output))
}

func captureRunOutput(t *testing.T, arguments []string) (int, string, string) {
	t.Helper()
	return captureOutput(t, func() int { return run(arguments) })
}

func captureCommandUsageError(t *testing.T, invocation invocation, message string) (int, string, string) {
	t.Helper()
	return captureOutput(t, func() int { return commandUsageErrorForInvocation(invocation, message) })
}

func captureOutput(t *testing.T, action func() int) (int, string, string) {
	t.Helper()
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdoutReader.Close()
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		stdoutWriter.Close()
		t.Fatal(err)
	}
	defer stderrReader.Close()
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutWriter, stderrWriter
	status := action()
	os.Stdout, os.Stderr = oldStdout, oldStderr
	if err := stdoutWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}
	return status, string(stdout), string(stderr)
}
