package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestGateStyleBaselineDiagnosticsRequireExactBaseSuiteSemantics(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		update    func(string) string
		wantSuite string
	}{
		{
			name: "suite argv changed",
			update: func(config string) string {
				return strings.Replace(config, `"argv":["go","test","./..."]`, `"argv":["go","test","./candidate"]`, 1)
			},
			wantSuite: "focused",
		},
		{
			name: "suite environment changed",
			update: func(config string) string {
				return strings.Replace(config, `"argv":["go","test","./..."]`, `"argv":["go","test","./..."],"environment":["BASELINE_DIAGNOSTIC"]`, 1)
			},
			wantSuite: "focused",
		},
		{
			name: "suite removed from base configuration",
			update: func(config string) string {
				return strings.Replace(config, `"name":"focused"`, `"name":"candidate-focused"`, 1)
			},
			wantSuite: "candidate-focused",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := contentRepository(t, nil)
			initializeEngineGitRepository(t, root)
			config, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
			if err != nil {
				t.Fatal(err)
			}
			writeEngineFile(t, root, policy.ConfigFilename, testCase.update(string(config)), 0o600)
			commitEngineCandidate(t, root, "candidate configuration")
			policyEngine, err := Open(root, root, "")
			if err != nil {
				t.Fatal(err)
			}
			commandRunner := &baselineCompatibilityRunner{candidateRoot: policyEngine.Repository.Root}
			policyEngine.Runner = commandRunner
			selection, err := policyEngine.SelectBase("main")
			if err != nil {
				t.Fatal(err)
			}
			report, err := policyEngine.test(t.Context(), testpolicy.Request{Changed: selection}, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Findings) != 1 || len(report.TestCommands) != 2 || len(report.TestDiagnostics) != 1 {
				t.Fatalf("report=%+v", report)
			}
			diagnostic := report.TestDiagnostics[0]
			if diagnostic.Suite != testCase.wantSuite || diagnostic.State != TestDiagnosticBaselineUnavailable || diagnostic.BaselineReplay != nil {
				t.Fatalf("diagnostic=%+v", diagnostic)
			}
			if commandRunner.candidateRuns != 2 || commandRunner.baselineRuns != 0 {
				t.Fatalf("candidate runs=%d baseline runs=%d", commandRunner.candidateRuns, commandRunner.baselineRuns)
			}
		})
	}
}

type baselineCompatibilityRunner struct {
	candidateRoot string
	candidateRuns int
	baselineRuns  int
}

func (baselineRunner *baselineCompatibilityRunner) Run(ctx context.Context, root string, command policy.Command) error {
	_, err := baselineRunner.RunWithResult(ctx, root, command)
	return err
}

func (baselineRunner *baselineCompatibilityRunner) RunWithResult(_ context.Context, root string, _ policy.Command) (runner.Result, error) {
	if root == baselineRunner.candidateRoot {
		baselineRunner.candidateRuns++
		return runner.Result{ExitStatus: 17, FailureCategory: runner.FailureCommandExit}, errors.New("candidate suite failed")
	}
	baselineRunner.baselineRuns++
	return runner.Result{}, nil
}
