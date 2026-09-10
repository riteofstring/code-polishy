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
	"time"

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

func TestMergeGatePublishesAssessedOSVReportExit(t *testing.T) {
	root, report, err := runOSVReportExitMergeGate(t, true)
	if err != nil || HasFindings(report) || len(report.Assessed) != 1 || report.GateRunPolicy == nil || report.GateRunPolicy.Status != "passed" {
		t.Fatalf("merge gate report = %+v, error = %v", report, err)
	}
	outcome := loadPublishedOSVOutcome(t, root, report)
	if outcome.Status != gaterun.Passed || len(outcome.Attempts) != 1 || outcome.Attempts[0].ExitStatus != 1 || !outcome.Attempts[0].ReportBearing {
		t.Fatalf("OSV command outcome = %+v", outcome)
	}
}

func TestMergeGateLetsPolicyRejectUnassessedOSVReportExit(t *testing.T) {
	root, report, err := runOSVReportExitMergeGate(t, false)
	if err != nil || len(report.Findings) != 1 || report.Findings[0].Check != "supplyChain.osvVulnerability" || report.GateRunPolicy == nil || report.GateRunPolicy.Status != "failed" {
		t.Fatalf("merge gate report = %+v, error = %v", report, err)
	}
	outcome := loadPublishedOSVOutcome(t, root, report)
	if outcome.Status != gaterun.Passed || len(outcome.Attempts) != 1 || outcome.Attempts[0].ExitStatus != 1 || !outcome.Attempts[0].ReportBearing {
		t.Fatalf("OSV command outcome = %+v", outcome)
	}
}

func runOSVReportExitMergeGate(t *testing.T, assessed bool) (string, Report, error) {
	t.Helper()
	root := contentRepository(t, nil)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	configurationPath := filepath.Join(root, policy.ConfigFilename)
	configuration, err := os.ReadFile(configurationPath)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, policy.ConfigFilename, string(configuration)+"\n", 0o600)
	commitEngineCandidate(t, root, "change policy")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	policyEngine.Repository.Config.ActivePolicyModules = []policy.ActivePolicyModule{{Name: "osv", Root: "."}}
	if assessed {
		policyEngine.Repository.Config.SupplyChain.VulnerabilityAssessments = []policy.VulnerabilityAssessment{{
			ID: "assessed-osv", Ecosystem: "npm", Advisory: "GHSA-abcd-1234-5678", Package: "example", AffectedVersion: "1.2.3",
			Scope: "content/data.json", Severity: "high", Status: "not-affected", Basis: "unreachable",
			Reason: "the affected code path is unreachable", Impact: "the vulnerable capability is not shipped",
			Evidence: "https://example.test/evidence", Tracking: "https://example.test/tracking", Owner: "content",
			ApprovedBy: "security", Approval: "https://example.test/approval", Reviewed: policy.Date{Time: today},
			Expires: policy.Date{Time: today.AddDate(0, 0, 7)},
		}}
	}
	policyEngine.Runner = assessedOSVGateRunner{}
	report, err := policyEngine.MergeGate(t.Context(), "main")
	return root, report, err
}

func loadPublishedOSVOutcome(t *testing.T, root string, report Report) gaterun.CommandOutcome {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(report.GateRunPolicy.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	gateReport := gaterun.Report{}
	if err := json.Unmarshal(data, &gateReport); err != nil {
		t.Fatal(err)
	}
	published, err := gaterun.LoadReport(root, gateReport.Identity)
	if err != nil {
		t.Fatalf("load published gate report: %v", err)
	}
	outcome, found := gateCommandOutcome(published.Commands, "osv-scan-root")
	if !found {
		t.Fatal("published gate report omitted OSV command outcome")
	}
	return outcome
}

func gateCommandOutcome(outcomes []gaterun.CommandOutcome, name string) (gaterun.CommandOutcome, bool) {
	for _, outcome := range outcomes {
		if outcome.Name == name {
			return outcome, true
		}
	}
	return gaterun.CommandOutcome{}, false
}

type assessedOSVGateRunner struct{}

func (assessedOSVGateRunner) Run(context.Context, string, policy.Command) error {
	return nil
}

func (assessedOSVGateRunner) RunWithOutput(_ context.Context, _ string, command policy.Command) (runner.Result, runner.Output, error) {
	if command.ReportProtocol != policy.OSVVulnerabilityReportProtocol {
		return runner.Result{ExitStatus: 0}, runner.Output{}, nil
	}
	payload := []byte(`{"results":[{"source":{"path":"content/data.json","type":"lockfile"},"packages":[{"package":{"name":"example","version":"1.2.3","ecosystem":"npm"},"groups":[{"ids":["GHSA-abcd-1234-5678"],"max_severity":"high"}]}]}]}`)
	return runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}, runner.Output{Stdout: payload}, errors.New("OSV-Scanner found vulnerabilities")
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
