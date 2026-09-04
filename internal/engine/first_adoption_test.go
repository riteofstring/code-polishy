package engine

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestMergeGateUsesCandidatePolicyForFirstAdoption(t *testing.T) {
	t.Parallel()
	root, base, policyEngine := firstAdoptionEngine(t, policy.ConfigFilename)
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	reason := firstAdoptionMergeGateReason(policy.ConfigFilename, base)
	requireFirstAdoptionPlan(t, plan, policy.ConfigFilename, reason)
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	requireFirstAdoptionReport(t, root, report, reason)
	requireFirstAdoptionCommands(t, commandRunner.commands)
}

func TestMergeGateFirstAdoptionHonorsCustomConfigurationPath(t *testing.T) {
	t.Parallel()
	const configPath = "policy/code-polishy.json"
	_, base, policyEngine := firstAdoptionEngine(t, configPath)
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	requireFirstAdoptionPlan(t, plan, configPath, firstAdoptionMergeGateReason(configPath, base))
}

func TestMergeGateRejectsInvalidBaseConfigurationInsteadOfTreatingItAsAbsent(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name      string
		prepare   func(t *testing.T, root string)
		wantError string
	}{
		{
			name: "malformed", wantError: "parse base configuration",
			prepare: func(t *testing.T, root string) {
				writeEngineFile(t, root, policy.ConfigFilename, "{\n", 0o600)
			},
		},
		{
			name: "symlink", wantError: "path is not a regular file",
			prepare: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, policy.ConfigFilename)); err != nil {
					t.Fatal(err)
				}
				writeEngineFile(t, root, "base-policy.json", "{}\n", 0o600)
				if err := os.Symlink("base-policy.json", filepath.Join(root, policy.ConfigFilename)); err != nil {
					t.Skipf("create configuration symlink: %v", err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := contentRepository(t, nil)
			installBehaviorReviewTestGuidance(t, root)
			candidate, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
			if err != nil {
				t.Fatal(err)
			}
			testCase.prepare(t, root)
			initializeEngineGitRepository(t, root)
			gitBehaviorReview(t, root, "switch", "-c", "candidate")
			if err := os.Remove(filepath.Join(root, policy.ConfigFilename)); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			writeEngineFile(t, root, policy.ConfigFilename, string(candidate), 0o600)
			gitBehaviorReview(t, root, "add", "--all")
			gitBehaviorReview(t, root, "commit", "-m", "candidate policy")
			policyEngine, err := Open(root, enginePolicyRoot(t), "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := policyEngine.PlanMergeGateExecution("main"); err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func firstAdoptionEngine(t *testing.T, configPath string) (string, string, *Engine) {
	t.Helper()
	root := contentRepository(t, nil)
	installBehaviorReviewTestGuidance(t, root)
	defaultPath := filepath.Join(root, policy.ConfigFilename)
	candidate, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(defaultPath); err != nil {
		t.Fatal(err)
	}
	initializeEngineGitRepository(t, root)
	base := gitEngineOutput(t, root, "rev-parse", "HEAD")
	gitBehaviorReview(t, root, "switch", "-c", "candidate")
	writeEngineFile(t, root, configPath, string(candidate), 0o600)
	writeEngineFile(t, root, "content/data.json", "{\"adopted\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "--all")
	gitBehaviorReview(t, root, "commit", "-m", "adopt policy")
	policyEngine, err := Open(root, enginePolicyRoot(t), configPath)
	if err != nil {
		t.Fatal(err)
	}
	return root, base, policyEngine
}

func requireFirstAdoptionPlan(t *testing.T, plan MergeGateExecutionPlan, configPath, reason string) {
	t.Helper()
	if !plan.FirstAdoption || plan.BaseConfigurationPath != configPath || plan.Level != testpolicy.MergeLevelFull ||
		!slices.Equal(plan.Reasons, []string{reason}) || plan.BehaviorReview.status.State != BehaviorReviewNotRun {
		t.Fatalf("plan = %+v", plan)
	}
}

func requireFirstAdoptionReport(t *testing.T, root string, report Report, reason string) {
	t.Helper()
	if len(report.Findings) != 0 || report.MergePolicy == nil || report.MergePolicy.Level != testpolicy.MergeLevelFull ||
		!slices.Contains(report.Notes, reason) || report.GateRunPolicy == nil || report.GateRunPolicy.ReportPath == "" {
		t.Fatalf("report = %+v", report)
	}
	reportPath := report.GateRunPolicy.ReportPath
	if !filepath.IsAbs(reportPath) {
		reportPath = filepath.Join(root, filepath.FromSlash(reportPath))
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	artifact := gaterun.Report{}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(artifact.Notes, reason) {
		t.Fatalf("gate report notes = %v", artifact.Notes)
	}
}

func requireFirstAdoptionCommands(t *testing.T, commands []string) {
	t.Helper()
	for _, name := range []string{"content-check", "focused", "full", "content-build", "offline-supply"} {
		if !slices.Contains(commands, name) {
			t.Fatalf("first-adoption gate omitted %q: %v", name, commands)
		}
	}
}
