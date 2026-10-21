package engine

import (
	"bytes"
	"cmp"
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
	"github.com/riteofstring/code-polishy/internal/rustfacts"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestRustCargoCommandsEnterGateForNestedManifests(t *testing.T) {
	t.Parallel()
	root := nativeRustEngineRepository(t)
	manifests := []string{
		"content/native/app/Cargo.toml",
		"content/native-app/Cargo.toml",
		"content/with space/" + strings.Repeat("é", 80) + "/Cargo.toml",
	}
	for _, manifest := range manifests {
		writeEngineFile(t, root, manifest, "[package]\nname='nested'\nversion='1.0.0'\nedition='2024'\n[workspace]\n[lib]\npath='lib.rs'\n", 0o600)
		writeEngineFile(t, root, filepath.Join(filepath.Dir(manifest), "lib.rs"), "pub fn answer() -> u32 { 42 }\n", 0o600)
	}
	initializeEngineGitRepository(t, root)
	engine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	files, err := engine.Repository.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	resolution := rustfacts.PlanResolution(engine.Repository, manifests, true)
	compilation := rustfacts.PlanCompilation(engine.Repository, resolution.Discovery, files)
	if len(resolution.Discovery.Findings) != 0 || len(compilation.Findings) != 0 || len(compilation.Commands) != len(manifests) {
		t.Fatalf("Cargo command planning failed: %+v, %+v", resolution, compilation)
	}
	commands := mergeGateCheckCommands(gaterun.SupplyChain, resolution.Commands)
	commands = append(commands, mergeGateCheckCommands(gaterun.Check, compilation.Commands)...)
	identity, err := gateRunIdentity(engine, gaterun.CheckpointGate, "main", strings.Repeat("a", 40), strings.Repeat("b", 40), "changed", commands, testGateRunBehaviorReview(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, command := range identity.Commands {
		if seen[command.Name] {
			t.Fatalf("different Cargo commands share an identity: %s", command.Name)
		}
		seen[command.Name] = true
	}
}

func TestRustNativeGateRetainsRealValidatedEvidence(t *testing.T) {
	t.Parallel()
	testRustNativeGate(t, false)
}

func TestRustDoctestGateRetainsRealValidatedEvidence(t *testing.T) {
	t.Parallel()
	testRustNativeGate(t, true)
}

func testRustNativeGate(t *testing.T, doctests bool) {
	t.Helper()
	root := nativeRustEngineRepository(t)
	artifactType := policy.NextestJUnitArtifact
	if doctests {
		enableRustDoctests(t, root)
		artifactType = policy.RustdocArtifact
	}
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
	if len(published.TestEvidence) == 0 || len(published.TestEvidence[0].Artifacts) != 1 || published.TestEvidence[0].Artifacts[0].Type != artifactType {
		t.Fatalf("missing native evidence: %+v", published.TestEvidence)
	}
}

func TestRustNativeReceiptsReuseOnlyTheSameSelectionAndInputs(t *testing.T) {
	t.Parallel()
	testRustNativeReceipt(t, false)
}

func TestRustDoctestReceiptsReuseOnlyTheSameSelectionAndInputs(t *testing.T) {
	t.Parallel()
	testRustNativeReceipt(t, true)
}

func testRustNativeReceipt(t *testing.T, doctests bool) {
	t.Helper()
	root := nativeRustEngineRepository(t)
	if doctests {
		enableRustDoctests(t, root)
	}
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
	reusedRun := testpolicy.RunWithEvidence(t.Context(), engine.Repository, commandRunner, testpolicy.Plan{Suites: []policy.TestSuite{suite}}, nil)
	if len(reusedRun.Findings) != 0 || len(reusedRun.Executions) != 1 || !reusedRun.Executions[0].Reused || reusedRun.Executions[0].ReceiptPath == "" || len(reusedRun.Executions[0].Artifacts) != 1 {
		t.Fatalf("native receipt reuse: %+v", reusedRun)
	}
	suite.Rust.Doctests = !doctests
	changed, err := newTestReceiptController(engine, []policy.TestSuite{suite}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, hit, err := changed.ReuseSuite(suite, 1); err != nil || hit {
		t.Fatalf("changed selector reused evidence: hit=%v, %v", hit, err)
	}
	suite.Rust.Doctests = doctests
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
		func(value *policy.RustTestSuite) { value.Doctests = true },
	} {
		changed := cloneReceiptSuite(original)
		alter(changed.Rust)
		if sameTestSuiteSemantics(original, changed) || !sameTestSuiteSemantics(original, cloneReceiptSuite(original)) {
			t.Fatal("historical suite comparison lost native Cargo selection")
		}
	}
}

func enableRustDoctests(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, policy.ConfigFilename, strings.Replace(string(data), `"rust":{}`, `"rust":{"doctests":true}`, 1), 0o600)
	writeEngineFile(t, root, "content/lib.rs", "/// ```\n/// assert_eq!(2 + 2, 4);\n/// ```\npub fn answer() {}\n", 0o600)
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
			if len(telemetry.Commands) != 2 || telemetry.Commands[0].Name != "focused-workspace" || telemetry.Commands[1].ExitStatus != test.exit {
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

func (nativeRustGateRunner) runPlain(ctx context.Context, root string, command policy.Command) error {
	_, err := runner.RunStreamed(ctx, (nativeRustGateRunner{}), root, command, io.Discard, io.Discard)
	return err
}

func (nativeRustGateRunner) runStreamed(ctx context.Context, root string, command policy.Command, stdout, stderr io.Writer) (runner.Result, error) {
	if command.Managed {
		return runner.RunStreamed(ctx, runner.OSRunner{}, root, command, stdout, stderr)
	}
	return runner.Result{}, nil
}

func (double nativeRustGateRunner) Execute(ctx context.Context, invocation runner.Invocation) (runner.Result, runner.Output, error) {
	if invocation.Stdout != nil || invocation.Stderr != nil {
		var stdout, stderr bytes.Buffer
		result, err := double.runStreamed(ctx, invocation.Root, invocation.Command, io.MultiWriter(&stdout, cmp.Or[io.Writer](invocation.Stdout, io.Discard)), io.MultiWriter(&stderr, cmp.Or[io.Writer](invocation.Stderr, io.Discard)))
		return result, runner.Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
	}
	return runner.Result{}, runner.Output{}, double.runPlain(ctx, invocation.Root, invocation.Command)
}
