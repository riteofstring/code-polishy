package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

func TestMergeGateAcceptsPassingFocusedRetryAndRetainsBothAttempts(t *testing.T) {
	root, report, commandRunner := runIntermittentMergeGate(t)
	if len(report.Findings) != 0 || report.GateRunPolicy == nil || report.GateRunPolicy.Status != "passed" ||
		len(report.TestDiagnostics) != 1 || report.TestDiagnostics[0].State != TestDiagnosticIntermittentObserved {
		t.Fatalf("report = %+v", report)
	}
	want := [][]string{{"go", "test", "./..."}, {"go", "test", "./...", "-run", "TestFailed"}}
	if !reflect.DeepEqual(commandRunner.focused, want) {
		t.Fatalf("focused commands = %v, want %v", commandRunner.focused, want)
	}
	outcome := readGateCommandOutcome(t, root, report.GateRunPolicy.ReportPath, "focused")
	if outcome.Status != gaterun.Passed || len(outcome.Attempts) != 2 || outcome.Attempts[0].Status != gaterun.Failed ||
		outcome.Attempts[1].Status != gaterun.Passed || outcome.Attempts[1].Diagnostic || outcome.ReceiptPath == "" {
		t.Fatalf("focused outcome = %+v", outcome)
	}
}

func runIntermittentMergeGate(t *testing.T) (string, Report, *intermittentGateRunner) {
	t.Helper()
	root := contentRepository(t, nil)
	configurationPath := filepath.Join(root, policy.ConfigFilename)
	configuration, err := os.ReadFile(configurationPath)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(
		string(configuration),
		`"argv":["go","test","./..."]`,
		`"argv":["go","test","./..."],"retryArgv":["go","test","./...","-run","TestFailed"]`,
		1,
	)
	writeEngineFile(t, root, policy.ConfigFilename, configured, 0o600)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"changed\":true}\n", 0o600)
	commitEngineCandidate(t, root, "change content")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &intermittentGateRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	return root, report, commandRunner
}

func readGateCommandOutcome(t *testing.T, root, reportPath, name string) gaterun.CommandOutcome {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(reportPath)))
	if err != nil {
		t.Fatal(err)
	}
	gateReport := gaterun.Report{}
	if err := json.Unmarshal(data, &gateReport); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range gateReport.Commands {
		if outcome.Name == name {
			return outcome
		}
	}
	t.Fatalf("%s outcome is absent", name)
	return gaterun.CommandOutcome{}
}

type intermittentGateRunner struct {
	focused [][]string
}

func (testRunner *intermittentGateRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, err := testRunner.RunWithResult(ctx, root, command)
	return err
}

func (testRunner *intermittentGateRunner) RunWithResult(_ context.Context, _ string, command policy.Command) (runner.Result, error) {
	if command.Name != "focused" {
		return runner.Result{ExitStatus: 0}, nil
	}
	testRunner.focused = append(testRunner.focused, append([]string{}, command.Argv...))
	if len(testRunner.focused) == 1 {
		return runner.Result{ExitStatus: 17, FailureCategory: runner.FailureCommandExit}, errors.New("intermittent failure")
	}
	return runner.Result{ExitStatus: 0}, nil
}

func TestMergeGateRecordsValidatedTestArtifacts(t *testing.T) {
	root := contentRepository(t, nil)
	configurationPath := filepath.Join(root, policy.ConfigFilename)
	configuration, err := os.ReadFile(configurationPath)
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(configuration), `"argv":["go","test","./..."]`, `"argv":["go","test","./..."],"artifacts":[{"path":"junit.xml","type":"junit","required":true}]`, 1)
	writeEngineFile(t, root, policy.ConfigFilename, configured, 0o600)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "content/data.json", "{\"changed\":true}\n", 0o600)
	commitEngineCandidate(t, root, "change content")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	policyEngine.Runner = artifactWritingEngineRunner{}
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.GateRunPolicy == nil {
		t.Fatalf("report = %+v", report)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(report.GateRunPolicy.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	gateReport := gaterun.Report{}
	if err := json.Unmarshal(data, &gateReport); err != nil {
		t.Fatal(err)
	}
	if len(gateReport.TestEvidence) == 0 || len(gateReport.TestEvidence[0].Artifacts) != 1 {
		t.Fatalf("test evidence = %+v", gateReport.TestEvidence)
	}
	artifact := gateReport.TestEvidence[0].Artifacts[0]
	if artifact.Path != "focused/junit.xml" || artifact.SHA256 == "" {
		t.Fatalf("artifact = %+v", artifact)
	}
	if _, err := os.Stat(filepath.Join(root, testartifact.RootName, gateReport.ExecutionID, "focused", "junit.xml")); err != nil {
		t.Fatalf("retained artifact is unavailable: %v", err)
	}
}

type artifactWritingEngineRunner struct{}

func (artifactWritingEngineRunner) Run(_ context.Context, _ string, command policy.Command) error {
	if command.TestArtifactDirectory == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(command.TestArtifactDirectory, "junit.xml"), []byte("<testsuite tests=\"1\"/>\n"), 0o600)
}

func TestGateRunIdentityChangesWithEffectiveAmbientEnvironment(t *testing.T) {
	root := contentRepository(t, nil)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commands := []MergeGateExecutionCommand{{
		Category: gaterun.OrdinaryTest, Kind: "unit", Scope: "repository", Cost: "quick",
		Command: policy.Command{Name: "unit", Argv: []string{"true"}, Cwd: ".", TimeoutSeconds: 30},
	}}
	previous, present := os.LookupEnv("CGO_ENABLED")
	defer func() {
		if present {
			_ = os.Setenv("CGO_ENABLED", previous)
		} else {
			_ = os.Unsetenv("CGO_ENABLED")
		}
	}()
	if err := os.Setenv("CGO_ENABLED", "0"); err != nil {
		t.Fatal(err)
	}
	first, err := gateRunIdentity(policyEngine, gaterun.MergeGate, "main", strings.Repeat("a", 40), strings.Repeat("b", 40), "recommended", commands, testGateRunBehaviorReview(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("CGO_ENABLED", "1"); err != nil {
		t.Fatal(err)
	}
	second, err := gateRunIdentity(policyEngine, gaterun.MergeGate, "main", strings.Repeat("a", 40), strings.Repeat("b", 40), "recommended", commands, testGateRunBehaviorReview(), nil)
	if err != nil {
		t.Fatal(err)
	}
	firstDigest, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	secondDigest, err := second.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if firstDigest == secondDigest {
		t.Fatal("effective ambient environment did not invalidate the gate-run identity")
	}
}

func testGateRunBehaviorReview() gaterun.BehaviorReview {
	return gaterun.BehaviorReview{
		State: gaterun.BehaviorReviewNotRun, RequiredBoundary: gaterun.BehaviorReviewOnRequest,
		SelectedFeatures: []gaterun.BehaviorReviewFeatureSelection{}, SelectionDigest: strings.Repeat("0", 64),
	}
}

func TestWorkingTreeCandidateDigestChangesWithContent(t *testing.T) {
	root := documentationRepository(t)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "README.md", "# First\n", 0o600)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	firstSelection, err := policyEngine.Repository.SelectBase("main")
	if err != nil {
		t.Fatal(err)
	}
	first, err := workingTreeCandidateDigest(root, firstSelection)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, "README.md", "# Second\n", 0o600)
	secondSelection, err := policyEngine.Repository.SelectBase("main")
	if err != nil {
		t.Fatal(err)
	}
	second, err := workingTreeCandidateDigest(root, secondSelection)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("working-tree content change did not change the candidate identity")
	}
}
