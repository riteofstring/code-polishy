package engine

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestRustReceiptCannotHideNewExternalWorkspace(t *testing.T) {
	t.Parallel()
	root := nativeRustEngineRepository(t)
	manifest, err := os.ReadFile(filepath.Join(root, "Cargo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, "Cargo.toml", strings.Replace(string(manifest), "[workspace]\n", "", 1), 0o600)
	engine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := engine.Repository.Config.Tests.Suites[0]
	suite.Reusable = true
	controller, err := newTestReceiptController(engine, []policy.TestSuite{suite}, true)
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &testReceiptRunner{delegate: runner.OSRunner{}, controller: controller}
	plan := testpolicy.Plan{Suites: []policy.TestSuite{suite}}
	initial := testpolicy.RunWithEvidence(t.Context(), engine.Repository, commandRunner, plan, nil)
	if len(initial.Findings) != 0 || initial.Executions[0].ReceiptPath == "" {
		t.Fatalf("initial native receipt: %+v", initial)
	}
	parent := filepath.Dir(root)
	writeEngineFile(t, parent, "Cargo.toml", "[workspace]\nmembers=['"+filepath.Base(root)+"']\nresolver='3'\n", 0o600)
	writeEngineFile(t, parent, "Cargo.lock", "version=4\n[[package]]\nname='native-tests'\nversion='1.0.0'\n", 0o600)
	changed := testpolicy.RunWithEvidence(t.Context(), engine.Repository, commandRunner, plan, nil)
	if len(changed.Findings) == 0 || changed.Executions[0].Reused || !strings.Contains(changed.Executions[0].FailureMessage, "workspace root") {
		t.Fatalf("receipt concealed an external workspace without changed repository inputs: %+v", changed)
	}
}

func TestRustGateRetryRecordsWorkspaceRevalidation(t *testing.T) {
	t.Parallel()
	root := nativeRustEngineRepository(t)
	writeEngineFile(t, root, "content/lib.rs", "#[test]\nfn retry_behavior() { let marker = std::path::Path::new(env!(\"CARGO_MANIFEST_DIR\")).join(\".tools/attempt\"); let previous = marker.exists(); std::fs::write(marker, b\"attempted\").unwrap(); assert!(previous); }\n", 0o600)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"changed\":true}\n", 0o600)
	commitEngineCandidate(t, root, "change content")
	engine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	engine.Output = io.Discard
	engine.Runner = nativeRustGateRunner{}
	report, err := engine.MergeGate(t.Context(), "main")
	if err != nil || HasFindings(report) || report.GateRunPolicy == nil || report.GateRunPolicy.Status != "passed" {
		t.Fatalf("native retry gate: %+v, %v", report, err)
	}
	workspace := readGateCommandOutcome(t, root, report.GateRunPolicy.ReportPath, "focused-workspace")
	suite := readGateCommandOutcome(t, root, report.GateRunPolicy.ReportPath, "focused")
	if len(workspace.Attempts) != 2 || workspace.Status != gaterun.Passed || len(suite.Attempts) != 2 || suite.Status != gaterun.Passed {
		t.Fatalf("retry skipped workspace discovery: workspace=%+v suite=%+v", workspace, suite)
	}
}

func TestRustReusableViewCannotDropDiscoveredBuildTarget(t *testing.T) {
	t.Parallel()
	root := nativeRustEngineRepository(t)
	writeEngineFile(t, root, "build.rs", "fn main() { panic!(\"required build script\"); }\n", 0o600)
	engine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := engine.Repository.Config.Tests.Suites[0]
	suite.Reusable = true
	controller, err := newTestReceiptController(engine, []policy.TestSuite{suite}, true)
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &testReceiptRunner{delegate: runner.OSRunner{}, controller: controller}
	result := testpolicy.RunWithEvidence(t.Context(), engine.Repository, commandRunner, testpolicy.Plan{Suites: []policy.TestSuite{suite}}, nil)
	if len(result.Findings) == 0 || !strings.Contains(result.Executions[0].FailureMessage, "workspace differs") || result.Executions[0].ReceiptPath != "" {
		t.Fatalf("view silently dropped the build target: %+v", result)
	}
	suite.ExtraInputs = []string{"build.rs"}
	controller, err = newTestReceiptController(engine, []policy.TestSuite{suite}, false)
	if err != nil {
		t.Fatal(err)
	}
	commandRunner.controller = controller
	declared := testpolicy.RunWithEvidence(t.Context(), engine.Repository, commandRunner, testpolicy.Plan{Suites: []policy.TestSuite{suite}}, nil)
	if len(declared.Findings) == 0 || strings.Contains(declared.Executions[0].FailureMessage, "workspace differs") {
		t.Fatalf("declared build target did not reach compilation: %+v", declared)
	}
}
