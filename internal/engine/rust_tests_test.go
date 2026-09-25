package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestRustNativeGateRetainsRealValidatedEvidence(t *testing.T) {
	t.Parallel()
	root := nativeRustEngineRepository(t)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"changed\":true}\n", 0o600)
	commitEngineCandidate(t, root, "change content")
	engine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	engine.Runner = nativeRustGateRunner{}
	report, err := engine.MergeGate(t.Context(), "main")
	if err != nil || HasFindings(report) || report.GateRunPolicy == nil || report.GateRunPolicy.Status != "passed" {
		t.Fatalf("Rust gate report: %+v, %v", report, err)
	}
	outcome := readGateCommandOutcome(t, root, report.GateRunPolicy.ReportPath, "focused")
	if outcome.Status != gaterun.Passed || len(outcome.Attempts) != 1 || outcome.Attempts[0].ExitStatus != 0 {
		t.Fatalf("native gate outcome: %+v", outcome)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(report.GateRunPolicy.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	var published gaterun.Report
	if err := json.Unmarshal(data, &published); err != nil {
		t.Fatal(err)
	}
	if len(published.TestEvidence) == 0 || len(published.TestEvidence[0].Artifacts) != 1 || published.TestEvidence[0].Artifacts[0].Type != policy.NextestJUnitArtifact {
		t.Fatalf("missing native evidence: %+v", published.TestEvidence)
	}
}

func TestRustNativeReceiptsReuseOnlyTheSameSelectionAndInputs(t *testing.T) {
	t.Parallel()
	root := nativeRustEngineRepository(t)
	engine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := engine.Repository.Config.Tests.Suites[0]
	suite.Reusable = true
	engine.Repository.Config.Tests.Suites[0] = suite
	controller, err := newTestReceiptController(engine, []policy.TestSuite{suite}, false)
	if err != nil {
		t.Fatal(err)
	}
	output := &bytes.Buffer{}
	commandRunner := &testReceiptRunner{delegate: runner.OSRunner{Stdout: output, Stderr: output}, controller: controller}
	run := testpolicy.RunWithEvidence(t.Context(), engine.Repository, commandRunner, testpolicy.Plan{Suites: []policy.TestSuite{suite}}, nil)
	if len(run.Findings) != 0 || len(run.Executions) != 1 || run.Executions[0].ReceiptPath == "" {
		t.Fatalf("native receipt publication: %+v, reasons=%v; %s", run, controller.reasons, output)
	}
	controller.allowReuse = true
	reused, hit, err := controller.ReuseSuite(suite, 1)
	if err != nil || !hit || !reused.Reused || len(reused.Artifacts) != 1 {
		t.Fatalf("native receipt reuse: %+v, %v, %v", reused, hit, err)
	}
	suite.Rust.Filter = "test(=other)"
	changed, err := newTestReceiptController(engine, []policy.TestSuite{suite}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := changed.ReuseSuite(suite, 1); err != nil || hit {
		t.Fatalf("changed selector reused evidence: hit=%v, %v", hit, err)
	}
	suite.Rust.Filter = ""
	writeEngineFile(t, root, "content/lib.rs", "#[test]\nfn behavior() { assert_eq!(2 + 2, 5); }\n", 0o600)
	changed, err = newTestReceiptController(engine, []policy.TestSuite{suite}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := changed.ReuseSuite(suite, 1); err != nil || hit {
		t.Fatalf("changed source reused evidence: hit=%v, %v", hit, err)
	}
}

func TestRustHistoricalSuiteComparisonKeepsTypedSelectors(t *testing.T) {
	t.Parallel()
	original := policy.TestSuite{Name: "rust", Rust: &policy.RustTestSuite{Manifest: "Cargo.toml", Packages: []string{"core"}, Features: []string{"default"}}}
	for _, alter := range []func(*policy.RustTestSuite){
		func(value *policy.RustTestSuite) { value.Manifest = "nested/Cargo.toml" },
		func(value *policy.RustTestSuite) { value.Packages[0] = "other" },
		func(value *policy.RustTestSuite) { value.Features[0] = "optional" },
		func(value *policy.RustTestSuite) { value.Filter = "test(selected)" },
		func(value *policy.RustTestSuite) { value.NoDefaultFeatures = true },
	} {
		changed := cloneReceiptSuite(original)
		alter(changed.Rust)
		if sameTestSuiteSemantics(original, changed) || !sameTestSuiteSemantics(original, cloneReceiptSuite(original)) {
			t.Fatal("historical suite comparison lost native Cargo selection")
		}
	}
}

func TestRustDirectTestTelemetryPreservesNativeExitStatus(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		source   string
		exit     int
		category runner.FailureCategory
	}{
		"assertion": {"#[test]\nfn behavior() { assert_eq!(2 + 2, 5); }\n", 100, runner.FailureCommandExit},
		"empty":     {"pub fn answer() -> u32 { 42 }\n", 4, runner.FailureOperational},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := nativeRustEngineRepository(t)
			writeEngineFile(t, root, "content/lib.rs", test.source, 0o600)
			engine, err := Open(root, enginePolicyRoot(t), "")
			if err != nil {
				t.Fatal(err)
			}
			engine.Output = io.Discard
			engine.Runner = runner.OSRunner{}
			engine.BeginExecution(time.Now(), 0)
			report, err := engine.Test(t.Context(), testpolicy.Request{Suites: []string{"focused"}})
			if err != nil || !HasFindings(report) || len(report.TestCommands) != 1 {
				t.Fatalf("native direct report: %+v, %v", report, err)
			}
			command := report.TestCommands[0]
			if command.Result.ExitStatus != test.exit || command.FailureCategory != test.category {
				t.Fatalf("instrumentation lost native outcome: %+v", command)
			}
			telemetry := engine.AttachExecution(report).Execution
			if len(telemetry.Commands) != 1 || telemetry.Commands[0].ExitStatus != test.exit {
				t.Fatalf("native telemetry is inconsistent: %+v", telemetry)
			}
		})
	}
}

func nativeRustEngineRepository(t *testing.T) string {
	t.Helper()
	root := contentRepository(t, nil)
	data, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	configuration := strings.Replace(string(data), `"argv":["go","test","./..."]`, `"rust":{}`, 1)
	writeEngineFile(t, root, policy.ConfigFilename, configuration, 0o600)
	writeEngineFile(t, root, "Cargo.toml", "[package]\nname='native-tests'\nversion='1.0.0'\nedition='2024'\n[workspace]\n[lib]\npath='content/lib.rs'\n", 0o600)
	writeEngineFile(t, root, "Cargo.lock", "version=4\n[[package]]\nname='native-tests'\nversion='1.0.0'\n", 0o600)
	writeEngineFile(t, root, "content/lib.rs", "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n", 0o600)
	return root
}

type nativeRustGateRunner struct{}

func (nativeRustGateRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, err := (nativeRustGateRunner{}).RunWithWriters(ctx, root, command, io.Discard, io.Discard)
	return err
}

func (nativeRustGateRunner) RunWithWriters(ctx context.Context, root string, command policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	if command.Managed {
		return (runner.OSRunner{}).RunWithWriters(ctx, root, command, stdout, stderr)
	}
	return runner.Result{}, nil
}
