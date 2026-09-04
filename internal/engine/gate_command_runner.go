package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/artifactsecurity"
	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/testartifact"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

type gateProofCaptureRunner struct {
	delegate runner.Runner
	stdout   io.Writer
	stderr   io.Writer
}

func (commandRunner *gateProofCaptureRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, _, err := commandRunner.RunWithOutput(ctx, root, command)
	return err
}

func (commandRunner *gateProofCaptureRunner) RunWithOutput(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	fmt.Fprintf(commandRunner.stdout, "\n=== %s in %s ===\n", command.Name, root)
	return runGateCommand(ctx, commandRunner.delegate, root, command, commandRunner.stdout, commandRunner.stderr, true)
}

func (controller *gateRunController) replayBehaviorReview(ctx context.Context, engine *Engine, base string, selection behaviorreview.ReviewSelection) error {
	commandRunner := controller.runner
	index := commandRunner.next
	if index >= len(commandRunner.expected) || commandRunner.expected[index].Category != gaterun.BehaviorProof {
		err := fmt.Errorf("gate behavior proof phase is outside its artifact plan")
		commandRunner.err = err
		return err
	}
	commandRunner.next++
	command := commandRunner.expected[index].Command
	fmt.Fprintf(commandRunner.progress, "RUN %s\n", command.Name)
	commandLog, err := commandRunner.run.OpenCommandLog(index, gaterun.LogOptions{})
	if err != nil {
		commandRunner.err = err
		return err
	}
	started := time.Now()
	captured := &gateProofCaptureRunner{delegate: engine.Runner, stdout: commandLog.Stdout(), stderr: commandLog.Stderr()}
	_, replayErr := behaviorreview.ReplayGateReceipt(ctx, engine.Repository, captured, behaviorreview.ValidateGateReceiptOptions{Base: base, Selection: selection})
	duration := time.Since(started)
	logResult, logErr := commandLog.Close()
	if logErr != nil {
		commandRunner.err = logErr
		return errors.Join(replayErr, logErr)
	}
	status := gaterun.Passed
	category := runner.FailureCategory("")
	exitStatus := 0
	if replayErr != nil {
		status = gaterun.Failed
		category = behaviorProofFailureCategory(ctx, replayErr)
		exitStatus = 1
		if category != runner.FailureCommandExit {
			exitStatus = -1
		}
	}
	outcome, recordErr := commandRunner.run.RecordAttempt(index, gaterun.AttemptInput{
		Status: status, FailureCategory: gaterun.FailureCategory(category), ExitStatus: exitStatus, Duration: duration,
	}, logResult)
	if recordErr != nil {
		commandRunner.err = recordErr
		return errors.Join(replayErr, recordErr)
	}
	attempt := outcome.Attempts[len(outcome.Attempts)-1]
	if replayErr == nil {
		fmt.Fprintf(commandRunner.progress, "PASS %s (%s)\n", command.Name, conciseDuration(duration))
		return nil
	}
	fmt.Fprintf(commandRunner.progress, "FAIL %s [%s]\n", command.Name, category)
	if tail := failureTail(logResult); tail != "" {
		fmt.Fprintln(commandRunner.progress, tail)
	}
	fmt.Fprintln(commandRunner.progress, "LOG", attempt.LogPath)
	return replayErr
}

func behaviorProofFailureCategory(ctx context.Context, err error) runner.FailureCategory {
	if category := runner.FailureCategoryFor(ctx, runner.Result{ExitStatus: 1}, err); category == runner.FailureTimeout || category == runner.FailureCanceled {
		return category
	}
	if errors.Is(err, behaviorreview.ErrOperational) || errors.Is(err, behaviorreview.ErrCleanup) {
		return runner.FailureOperational
	}
	return runner.FailureCommandExit
}

func (commandRunner *gateArtifactRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, err := commandRunner.RunWithResult(ctx, root, command)
	return err
}

func (commandRunner *gateArtifactRunner) RunWithResult(ctx context.Context, root string, command policy.Command) (runner.Result, error) {
	result, _, err := commandRunner.runNext(ctx, root, command, false)
	return result, err
}

func (commandRunner *gateArtifactRunner) RunWithOutput(ctx context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	return commandRunner.runNext(ctx, root, command, true)
}

func (commandRunner *gateArtifactRunner) ManagesTestArtifacts() bool { return true }

func (commandRunner *gateArtifactRunner) TestArtifacts(name string, attempt int) []testartifact.Record {
	return append([]testartifact.Record{}, commandRunner.artifacts[testLogKey{name: name, attempt: attempt}]...)
}

func (commandRunner *gateArtifactRunner) ReuseSuite(suite policy.TestSuite, attempt int) (testpolicy.SuiteExecution, bool, error) {
	if commandRunner.receipts == nil || attempt != 1 {
		return testpolicy.SuiteExecution{}, false, nil
	}
	index := commandRunner.next
	if index >= len(commandRunner.expected) || commandRunner.expected[index].Category != gaterun.OrdinaryTest ||
		commandRunner.expected[index].Command.Name != suite.Name {
		return testpolicy.SuiteExecution{}, false, nil
	}
	execution, reusable, err := commandRunner.receipts.ReuseSuite(suite, attempt)
	if err != nil || !reusable {
		return testpolicy.SuiteExecution{}, reusable, err
	}
	identity := commandRunner.receipts.identities[suite.Name]
	identitySHA256, err := identity.Digest()
	if err != nil {
		return testpolicy.SuiteExecution{}, false, err
	}
	if _, err := commandRunner.run.RecordSuiteReuse(index, gaterun.SuiteReuse{
		IdentitySHA256: identitySHA256, ReceiptPath: execution.ReceiptPath,
		ReceiptSHA256: execution.ReceiptSHA256, DurationMillis: execution.Result.ExecutionDuration.Milliseconds(),
	}); err != nil {
		commandRunner.err = err
		return testpolicy.SuiteExecution{}, false, err
	}
	commandRunner.next++
	commandRunner.artifacts[testLogKey{name: suite.Name, attempt: attempt}] = append([]testartifact.Record{}, execution.Artifacts...)
	return execution, true, nil
}

func (commandRunner *gateArtifactRunner) RecordSuite(execution testpolicy.SuiteExecution) error {
	if commandRunner.receipts == nil {
		return nil
	}
	return commandRunner.receipts.RecordSuite(execution)
}

func (commandRunner *gateArtifactRunner) ReceiptNotes() []string {
	if commandRunner.receipts == nil {
		return nil
	}
	return commandRunner.receipts.Notes()
}

func (commandRunner *gateArtifactRunner) PrepareSuiteView(suite policy.TestSuite) (string, func() error, error) {
	if commandRunner.receipts == nil {
		return "", nil, errors.New("test receipt controller is unavailable")
	}
	return commandRunner.receipts.PrepareSuiteView(suite)
}

func (commandRunner *gateArtifactRunner) TestDiagnosticRunner() runner.Runner {
	return &gateDiagnosticRunner{parent: commandRunner}
}

func (commandRunner *gateArtifactRunner) runNext(ctx context.Context, root string, command policy.Command, captureOutput bool) (runner.Result, runner.Output, error) {
	if commandRunner.err != nil {
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, commandRunner.err
	}
	index := commandRunner.next
	if index < len(commandRunner.expected) && !samePolicyCommand(commandRunner.expected[index].Command, command) && artifactsecurity.IsCleanupCommand(command) {
		for candidate := index + 1; candidate < len(commandRunner.expected); candidate++ {
			if samePolicyCommand(commandRunner.expected[candidate].Command, command) {
				index = candidate
				break
			}
		}
	}
	if index >= len(commandRunner.expected) || !samePolicyCommand(commandRunner.expected[index].Command, command) {
		commandRunner.err = fmt.Errorf("gate started a command outside its artifact plan at position %d", commandRunner.next+1)
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, commandRunner.err
	}
	commandRunner.next = index + 1
	if receipt, ok := commandRunner.reusable[index]; ok {
		if _, err := commandRunner.run.RecordReuse(index, receipt); err != nil {
			commandRunner.err = err
			return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
		}
		fmt.Fprintf(commandRunner.progress, "REUSE %s\n", command.Name)
		return runner.Result{ExitStatus: 0}, runner.Output{}, nil
	}
	return commandRunner.execute(ctx, root, index, command, captureOutput, false)
}

func (commandRunner *gateArtifactRunner) execute(ctx context.Context, root string, index int, command policy.Command, captureOutput, diagnostic bool) (runner.Result, runner.Output, error) {
	fmt.Fprintf(commandRunner.progress, "RUN %s\n", command.Name)
	actual, err := commandRunner.prepareCommand(index, command)
	if err != nil {
		commandRunner.err = err
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	commandLog, err := commandRunner.run.OpenCommandLog(index, gaterun.LogOptions{})
	if err != nil {
		commandRunner.err = err
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, err
	}
	result, output, runErr := runGateCommand(ctx, commandRunner.delegate, root, actual, commandLog.Stdout(), commandLog.Stderr(), captureOutput)
	artifacts := []testartifact.Record{}
	if commandRunner.expected[index].Category == gaterun.OrdinaryTest {
		var artifactErr error
		artifacts, artifactErr = commandRunner.artifactExecution.ValidateCommand(actual)
		runErr = errors.Join(runErr, artifactErr)
	}
	logResult, logErr := commandLog.Close()
	if logErr != nil {
		commandRunner.err = logErr
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, output, errors.Join(runErr, logErr)
	}
	category := runner.FailureCategoryFor(ctx, result, runErr)
	status := gaterun.Passed
	gateCategory := gaterun.FailureCategory("")
	if runErr != nil {
		status = gaterun.Failed
		gateCategory = gaterun.FailureCategory(category)
		result.FailureCategory = category
		if commandRunner.expected[index].Category == gaterun.OrdinaryTest {
			commandRunner.failedTests[command.Name] = index
		}
	}
	outcome, recordErr := commandRunner.run.RecordAttempt(index, gaterun.AttemptInput{
		Status: status, FailureCategory: gateCategory, ExitStatus: result.ExitStatus,
		Duration: result.ExecutionDuration, ResourceWait: result.ResourceWait, Diagnostic: diagnostic,
	}, logResult)
	if recordErr != nil {
		commandRunner.err = recordErr
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, output, errors.Join(runErr, recordErr)
	}
	attempt := outcome.Attempts[len(outcome.Attempts)-1]
	if commandRunner.expected[index].Category == gaterun.OrdinaryTest {
		commandRunner.logPaths[testLogKey{name: command.Name, attempt: attempt.Number}] = attempt.LogPath
		commandRunner.artifacts[testLogKey{name: command.Name, attempt: attempt.Number}] = append([]testartifact.Record{}, artifacts...)
	}
	if runErr == nil {
		fmt.Fprintf(commandRunner.progress, "PASS %s (%s)\n", command.Name, conciseDuration(result.ExecutionDuration))
		return result, output, nil
	}
	fmt.Fprintf(commandRunner.progress, "FAIL %s [%s]\n", command.Name, category)
	if tail := failureTail(logResult); tail != "" {
		fmt.Fprintln(commandRunner.progress, tail)
	}
	fmt.Fprintln(commandRunner.progress, "LOG", attempt.LogPath)
	return result, output, runErr
}

func (commandRunner *gateArtifactRunner) prepareCommand(index int, command policy.Command) (policy.Command, error) {
	if commandRunner.expected[index].Category != gaterun.OrdinaryTest {
		return command, nil
	}
	suite := policy.TestSuite{Name: command.Name, Artifacts: command.TestArtifacts}
	return commandRunner.artifactExecution.PrepareCommand(suite, command)
}

func (commandRunner *gateArtifactRunner) runDiagnostic(ctx context.Context, root string, command policy.Command) (runner.Result, error) {
	index, ok := commandRunner.failedTests[command.Name]
	if !ok || !samePolicyCommand(commandRunner.expected[index].Command, command) {
		err := fmt.Errorf("test diagnostic command %q does not match a failed planned suite", command.Name)
		commandRunner.err = errors.Join(commandRunner.err, err)
		return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, err
	}
	result, _, err := commandRunner.execute(ctx, root, index, command, false, true)
	return result, err
}

func (diagnostic *gateDiagnosticRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, err := diagnostic.RunWithResult(ctx, root, command)
	return err
}

func (diagnostic *gateDiagnosticRunner) RunWithResult(ctx context.Context, root string, command policy.Command) (runner.Result, error) {
	return diagnostic.parent.runDiagnostic(ctx, root, command)
}

func (diagnostic *gateDiagnosticRunner) ManagesTestArtifacts() bool { return true }

func (diagnostic *gateDiagnosticRunner) TestArtifacts(name string, attempt int) []testartifact.Record {
	return diagnostic.parent.TestArtifacts(name, attempt)
}

func runGateCommand(ctx context.Context, commandRunner runner.Runner, root string, command policy.Command, stdout, stderr io.Writer, captureOutput bool) (runner.Result, runner.Output, error) {
	if streamed, ok := commandRunner.(runner.StreamRunner); ok {
		if !captureOutput {
			result, err := streamed.RunWithWriters(ctx, root, command, stdout, stderr)
			return result, runner.Output{}, err
		}
		capturedStdout := &gateOutputBuffer{limit: 8 << 20}
		capturedStderr := &gateOutputBuffer{limit: 64 << 10}
		result, err := streamed.RunWithWriters(ctx, root, command, io.MultiWriter(stdout, capturedStdout), io.MultiWriter(stderr, capturedStderr))
		if capturedStdout.truncated || capturedStderr.truncated {
			err = errors.Join(err, fmt.Errorf("captured command output exceeds its structured output limit"))
			result.FailureCategory = runner.FailureCategoryFor(ctx, result, err)
		}
		return result, runner.Output{Stdout: capturedStdout.Bytes(), Stderr: capturedStderr.Bytes()}, err
	}
	if captureOutput {
		captured, ok := commandRunner.(runner.OutputRunner)
		if !ok {
			return runner.Result{ExitStatus: -1, FailureCategory: runner.FailureOperational}, runner.Output{}, fmt.Errorf("gate command %q requires captured output", command.Name)
		}
		result, output, err := captured.RunWithOutput(ctx, root, command)
		_, _ = stdout.Write(output.Stdout)
		_, _ = stderr.Write(output.Stderr)
		return result, output, err
	}
	if observed, ok := commandRunner.(interface {
		RunWithResult(context.Context, string, policy.Command) (runner.Result, error)
	}); ok {
		result, err := observed.RunWithResult(ctx, root, command)
		return result, runner.Output{}, err
	}
	started := time.Now()
	err := commandRunner.Run(ctx, root, command)
	return runner.Result{ExecutionDuration: time.Since(started)}, runner.Output{}, err
}

type gateOutputBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (buffer *gateOutputBuffer) Write(data []byte) (int, error) {
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		if len(data) < remaining {
			remaining = len(data)
		}
		buffer.buffer.Write(data[:remaining])
	}
	if len(data) > remaining {
		buffer.truncated = true
	}
	return len(data), nil
}

func (buffer *gateOutputBuffer) Bytes() []byte {
	return append([]byte{}, buffer.buffer.Bytes()...)
}

func failureTail(log gaterun.LogResult) string {
	data := log.StderrTail
	if len(data) == 0 {
		data = log.Stderr
	}
	if len(data) == 0 {
		data = log.StdoutTail
	}
	if len(data) == 0 {
		data = log.Stdout
	}
	if len(data) == 0 {
		return ""
	}
	const maximum = 4096
	if len(data) > maximum {
		data = data[len(data)-maximum:]
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return strings.Join(lines, "\n")
}

func conciseDuration(duration time.Duration) string {
	if duration <= 0 {
		return "0s"
	}
	return duration.Round(10 * time.Millisecond).String()
}
