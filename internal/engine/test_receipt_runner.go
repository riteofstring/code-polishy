package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/testartifact"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
	"github.com/riteofstring/code-polishy/internal/testreceipt"
)

type testReceiptController struct {
	root       string
	suites     []policy.TestSuite
	identities map[string]testreceipt.Identity
	reasons    map[string]string
	allowReuse bool
	output     io.Writer
	verbose    bool
}

type testReceiptRunner struct {
	delegate   runner.Runner
	controller *testReceiptController
}

func newTestReceiptController(engine *Engine, suites []policy.TestSuite, allowReuse bool) (*testReceiptController, error) {
	controller := &testReceiptController{
		root: engine.Repository.Root, suites: append([]policy.TestSuite{}, suites...), identities: map[string]testreceipt.Identity{}, reasons: map[string]string{},
		allowReuse: allowReuse, output: engine.Output, verbose: engine.Verbose,
	}
	if controller.output == nil {
		controller.output = io.Discard
	}
	for _, suite := range suites {
		identity, reason, err := engine.suiteReceiptIdentity(suite)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			controller.reasons[suite.Name] = reason
			continue
		}
		controller.identities[suite.Name] = identity
	}
	return controller, nil
}

type testReceiptDecision struct {
	Suite    string
	Action   string
	Prior    string
	Reason   string
	Reusable *testreceipt.Reusable
}

func (controller *testReceiptController) Decisions() []testReceiptDecision {
	if controller == nil {
		return nil
	}
	decisions := make([]testReceiptDecision, 0, len(controller.suites))
	for _, suite := range controller.suites {
		decision := testReceiptDecision{Suite: suite.Name, Action: "execute", Prior: "-"}
		identity, eligible := controller.identities[suite.Name]
		if !controller.allowReuse {
			decision.Reason = "reuse was not requested"
		} else if !eligible {
			decision.Reason = controller.reasons[suite.Name]
		} else if reusable, err := testreceipt.Load(controller.root, identity); err == nil {
			decision.Action = "reuse"
			decision.Prior = conciseDuration(time.Duration(reusable.Receipt.DurationMillis) * time.Millisecond)
			decision.Reason = "complete identity matches"
			decision.Reusable = &reusable
		} else {
			decision.Reason = testReceiptDecisionReason(err)
			controller.reasons[suite.Name] = decision.Reason
		}
		decisions = append(decisions, decision)
	}
	return decisions
}

func testReceiptDecisionReason(err error) string {
	switch {
	case errors.Is(err, testreceipt.ErrMissing):
		return "no matching receipt"
	case errors.Is(err, testreceipt.ErrExpired):
		return "matching receipt expired"
	case errors.Is(err, testreceipt.ErrInvalid):
		return "matching receipt is invalid"
	default:
		return fmt.Sprintf("receipt unavailable: %v", err)
	}
}

func (controller *testReceiptController) RenderPlan() {
	if controller == nil || !controller.verbose {
		return
	}
	for _, decision := range controller.Decisions() {
		fmt.Fprintf(controller.output, "TEST RECEIPT PLAN suite=%q action=%s prior=%s reason=%q\n", decision.Suite, decision.Action, decision.Prior, decision.Reason)
	}
}

func (controller *testReceiptController) ReuseSuite(suite policy.TestSuite, attempt int) (testpolicy.SuiteExecution, bool, error) {
	if controller == nil || !controller.allowReuse || attempt != 1 {
		return testpolicy.SuiteExecution{}, false, nil
	}
	identity, eligible := controller.identities[suite.Name]
	if !eligible {
		return testpolicy.SuiteExecution{}, false, nil
	}
	reusable, err := testreceipt.Load(controller.root, identity)
	if err != nil {
		if errors.Is(err, testreceipt.ErrMissing) || errors.Is(err, testreceipt.ErrInvalid) || errors.Is(err, testreceipt.ErrExpired) {
			controller.reasons[suite.Name] = err.Error()
			return testpolicy.SuiteExecution{}, false, nil
		}
		return testpolicy.SuiteExecution{}, false, err
	}
	duration := time.Duration(reusable.Receipt.DurationMillis) * time.Millisecond
	fmt.Fprintf(controller.output, "REUSE %s (%s)\n", suite.Name, duration)
	return testpolicy.SuiteExecution{
		Result: runner.Result{ExitStatus: 0, ExecutionDuration: duration}, ResultKnown: true, Attempt: attempt,
		Artifacts: append([]testartifact.Record{}, reusable.Receipt.Artifacts...), Reused: true,
		ReceiptPath: reusable.Path, ReceiptSHA256: reusable.Receipt.SHA256,
	}, true, nil
}

func (controller *testReceiptController) RecordSuite(execution testpolicy.SuiteExecution) error {
	if controller == nil || execution.Failed() || execution.Reused || execution.Attempt != 1 {
		return nil
	}
	identity, eligible := controller.identities[execution.Suite.Name]
	if !eligible {
		return nil
	}
	receipt, err := testreceipt.RecordPassed(controller.root, identity, execution.Result.ExecutionDuration, execution.Artifacts)
	if err != nil {
		controller.reasons[execution.Suite.Name] = fmt.Sprintf("the passing result could not be cached: %v", err)
		return nil
	}
	if controller.verbose {
		fmt.Fprintf(controller.output, "TEST RECEIPT suite=%q path=%q sha256=%q\n", execution.Suite.Name, receipt.Path, receipt.Receipt.SHA256)
	}
	return nil
}

func (controller *testReceiptController) Notes() []string {
	if controller == nil || !controller.verbose {
		return nil
	}
	notes := []string{}
	for name, reason := range controller.reasons {
		notes = append(notes, fmt.Sprintf("test receipt for %s was not reused: %s", name, reason))
	}
	sort.Strings(notes)
	return notes
}

func (commandRunner *testReceiptRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, err := commandRunner.RunWithResult(ctx, root, command)
	return err
}

func (commandRunner *testReceiptRunner) RunWithResult(ctx context.Context, root string, command policy.Command) (runner.Result, error) {
	if observed, ok := commandRunner.delegate.(interface {
		RunWithResult(context.Context, string, policy.Command) (runner.Result, error)
	}); ok {
		return observed.RunWithResult(ctx, root, command)
	}
	started := time.Now()
	err := commandRunner.delegate.Run(ctx, root, command)
	return runner.Result{ExecutionDuration: time.Since(started)}, err
}

func (commandRunner *testReceiptRunner) ReuseSuite(suite policy.TestSuite, attempt int) (testpolicy.SuiteExecution, bool, error) {
	return commandRunner.controller.ReuseSuite(suite, attempt)
}

func (commandRunner *testReceiptRunner) RecordSuite(execution testpolicy.SuiteExecution) error {
	return commandRunner.controller.RecordSuite(execution)
}

func (commandRunner *testReceiptRunner) ReceiptNotes() []string {
	return commandRunner.controller.Notes()
}
