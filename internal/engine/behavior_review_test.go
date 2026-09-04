package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	agentpolicy "github.com/riteofstring/code-polishy/internal/agents"
	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/finalstate"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
	testpolicy "github.com/riteofstring/code-polishy/internal/testing"
)

func TestMergeGateBypassesBehaviorReviewForDocumentationCandidates(t *testing.T) {
	root := documentationRepository(t)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "README.md", "# Updated\n", 0o600)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if report.MergePolicy == nil || report.MergePolicy.Level != testpolicy.MergeLevelDocumentation ||
		slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool { return finding.Check == "policy.behaviorReview" }) {
		t.Fatalf("report = %+v", report)
	}
	if len(commandRunner.commands) != 0 {
		t.Fatalf("documentation merge gate commands = %v", commandRunner.commands)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".code-polishy-reports", "behavior-review")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("documentation merge gate created behavior-review artifacts: %v", statErr)
	}
}

func TestBehaviorReviewWithoutConfigurationStaysOptionalAndSkipsArtifacts(t *testing.T) {
	root, policyEngine, staleJournal := behaviorReviewWithoutConfigurationCandidate(t)
	plan, planErr := policyEngine.PlanMergeGateExecution("main")
	assertBehaviorReviewNotRunPlan(t, plan, planErr)
	status, statusErr := policyEngine.BehaviorReviewStatus(t.Context(), "main")
	assertBehaviorReviewNotRunStatus(t, status, statusErr)
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, mergeErr := policyEngine.MergeGate(t.Context(), "main")
	assertBehaviorReviewOptionalMergeGate(t, report, mergeErr, commandRunner)
	assertBehaviorReviewArtifactsUnchanged(t, root, staleJournal)
	planningReport, planningErr := policyEngine.TestPlan("main")
	assertBehaviorReviewNotRunPlanningReport(t, planningReport, planningErr)
}

func TestFirstAdoptionAppliesCandidateBehaviorReviewWithoutInventingBasePolicy(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	installRequiredBehaviorReviewPolicy(t, root, "merge")
	installBehaviorReviewTestGuidance(t, root)
	configPath := filepath.Join(root, policy.ConfigFilename)
	candidate, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "candidate")
	writeEngineFile(t, root, policy.ConfigFilename, string(candidate), 0o600)
	writeEngineFile(t, root, "content/data.json", "{\"adopted\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "--all")
	gitBehaviorReview(t, root, "commit", "-m", "adopt policy")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.FirstAdoption || !plan.BehaviorReview.required || !plan.BehaviorReview.status.FullCandidate ||
		len(plan.BehaviorReview.baseSelectedSuites) != 0 {
		t.Fatalf("plan = %+v", plan)
	}
}

func behaviorReviewWithoutConfigurationCandidate(t *testing.T) (string, *Engine, []byte) {
	t.Helper()
	root := contentRepository(t, nil)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "candidate")
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "candidate")
	staleJournal := []byte("{\n  \"version\": 1,\n  \"entries\": []\n}\n")
	writeEngineFile(t, root, ".code-polishy-reports/behavior-review/intent-journal.json", string(staleJournal), 0o600)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	return root, policyEngine, staleJournal
}

func assertBehaviorReviewNotRunPlan(t *testing.T, plan MergeGateExecutionPlan, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	status := plan.BehaviorReview.status
	if status.State != BehaviorReviewNotRun || status.FullCandidate || len(status.SelectedFeatures) != 0 {
		t.Fatalf("behavior review plan = %+v", status)
	}
}

func assertBehaviorReviewNotRunStatus(t *testing.T, status BehaviorReviewStatus, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if status.State != BehaviorReviewNotRun || len(status.Required) != 0 || len(status.Missing) != 0 {
		t.Fatalf("status = %+v", status)
	}
}

func assertBehaviorReviewOptionalMergeGate(t *testing.T, report Report, err error, commandRunner *recordingEngineRunner) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if report.BehaviorReview == nil || report.BehaviorReview.State != BehaviorReviewNotRun || len(report.Findings) != 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(commandRunner.commands) == 0 {
		t.Fatal("optional merge gate did not run its ordinary commands")
	}
}

func assertBehaviorReviewArtifactsUnchanged(t *testing.T, root string, staleJournal []byte) {
	t.Helper()
	journalPath := filepath.Join(root, ".code-polishy-reports", "behavior-review", "intent-journal.json")
	journal, readErr := os.ReadFile(journalPath)
	if readErr != nil || !slices.Equal(journal, staleJournal) {
		t.Fatalf("optional gate changed stale behavior-review artifacts: %q, %v", journal, readErr)
	}
}

func assertBehaviorReviewNotRunPlanningReport(t *testing.T, report Report, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if report.BehaviorReview == nil || report.BehaviorReview.State != BehaviorReviewNotRun {
		t.Fatalf("planning report = %+v", report)
	}
}

func TestTaskRequestedBehaviorReviewFeatureBlocksBeforeMergeCommands(t *testing.T) {
	policyEngine, _ := behaviorReviewContentCandidate(t, `{"defaultRequiredAt":"on-request","features":[{"name":"content","modules":["content"],"suites":["focused"]}]}`, []string{"content"})
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	status := plan.BehaviorReview.status
	if status.State != BehaviorReviewRequired || status.RequiredBoundary != BehaviorReviewOnRequest ||
		!slices.Equal(status.TaskRequested, []string{"content"}) || !slices.Equal(status.Required, []string{"content"}) ||
		len(status.SelectedFeatures) != 1 || status.SelectedFeatures[0].Name != "content" ||
		!slices.Equal(status.SelectedFeatures[0].Reasons, []string{behaviorreview.SelectionReasonTaskRequested}) {
		t.Fatalf("task-requested plan = %+v", status)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFinding(t, report, "missing behavior review receipt")
	if report.BehaviorReview == nil || report.BehaviorReview.State != BehaviorReviewRequired || len(commandRunner.commands) != 0 {
		t.Fatalf("report = %+v commands = %v", report, commandRunner.commands)
	}
}

func TestMergeRequiredFeatureStaysOptionalAtCheckpointAndBlocksMerge(t *testing.T) {
	policyEngine, _ := behaviorReviewContentCandidate(t, `{"defaultRequiredAt":"on-request","features":[{"name":"content","modules":["content"],"suites":["focused"],"requiredAt":"merge"}]}`, nil)
	checkpointRunner := &recordingEngineRunner{}
	policyEngine.Runner = checkpointRunner
	checkpoint, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil || checkpoint.BehaviorReview == nil || checkpoint.BehaviorReview.State != BehaviorReviewNotRun || len(checkpoint.Findings) != 0 {
		t.Fatalf("checkpoint = %+v, error = %v", checkpoint, err)
	}
	if len(checkpointRunner.commands) == 0 {
		t.Fatal("merge-only feature skipped ordinary checkpoint commands")
	}
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil || plan.BehaviorReview.status.State != BehaviorReviewRequired || plan.BehaviorReview.status.RequiredBoundary != BehaviorReviewMerge {
		t.Fatalf("merge plan = %+v, error = %v", plan.BehaviorReview.status, err)
	}
	mergeRunner := &recordingEngineRunner{}
	policyEngine.Runner = mergeRunner
	merged, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFinding(t, merged, "missing behavior review receipt")
	if merged.BehaviorReview == nil || merged.BehaviorReview.State != BehaviorReviewRequired || len(mergeRunner.commands) != 0 {
		t.Fatalf("merge report = %+v commands = %v", merged, mergeRunner.commands)
	}
}

func TestCheckpointRequiredFeatureBlocksCheckpointAndMerge(t *testing.T) {
	policyEngine, _ := behaviorReviewContentCandidate(t, `{"defaultRequiredAt":"on-request","features":[{"name":"content","modules":["content"],"suites":["focused"],"requiredAt":"checkpoint"}]}`, nil)
	checkpointRunner := &recordingEngineRunner{}
	policyEngine.Runner = checkpointRunner
	checkpoint, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertCheckpointBehaviorReviewFinding(t, checkpoint, "missing behavior review receipt")
	if checkpoint.BehaviorReview == nil || checkpoint.BehaviorReview.RequiredBoundary != BehaviorReviewCheckpoint || len(checkpointRunner.commands) != 0 {
		t.Fatalf("checkpoint report = %+v commands = %v", checkpoint, checkpointRunner.commands)
	}
	mergeRunner := &recordingEngineRunner{}
	policyEngine.Runner = mergeRunner
	merged, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFinding(t, merged, "missing behavior review receipt")
	if merged.BehaviorReview == nil || merged.BehaviorReview.RequiredBoundary != BehaviorReviewCheckpoint || len(mergeRunner.commands) != 0 {
		t.Fatalf("merge report = %+v commands = %v", merged, mergeRunner.commands)
	}
}

func TestStrictBehaviorReviewSelectsTheFullCandidate(t *testing.T) {
	policyEngine, _ := behaviorReviewContentCandidate(t, `{"defaultRequiredAt":"merge","features":[]}`, nil)
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	status := plan.BehaviorReview.status
	if status.State != BehaviorReviewRequired || status.RequiredBoundary != BehaviorReviewMerge || !status.FullCandidate ||
		len(status.SelectedFeatures) != 0 || len(status.Required) != 0 {
		t.Fatalf("strict plan = %+v", status)
	}
}

func TestBehaviorReviewUnionsBaseAndCandidateFeatureDefinitions(t *testing.T) {
	root := contentRepository(t, nil)
	basePolicy := `{"defaultRequiredAt":"on-request","features":[{"name":"content","modules":["content"],"suites":["focused"],"requiredAt":"checkpoint"}]}`
	candidatePolicy := `{"defaultRequiredAt":"on-request","features":[{"name":"content","paths":["content/data.json"],"suites":["full"]}]}`
	installBehaviorReviewPolicy(t, root, basePolicy)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "candidate")
	replaceBehaviorReviewPolicy(t, root, basePolicy, candidatePolicy)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", policy.ConfigFilename, "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "weaken candidate behavior review")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.BehaviorReview.selection.Features) != 1 {
		t.Fatalf("selection = %+v", plan.BehaviorReview.selection)
	}
	feature := plan.BehaviorReview.selection.Features[0]
	if feature.Name != "content" || feature.RequiredAt != policy.BehaviorReviewCheckpoint ||
		!slices.Equal(feature.Modules, []string{"content"}) || !slices.Equal(feature.Paths, []string{"content/data.json"}) ||
		!slices.Equal(feature.Suites, []string{"focused", "full"}) ||
		!slices.Equal(feature.Reasons, []string{behaviorreview.SelectionReasonBaseRequired}) {
		t.Fatalf("merged feature = %+v", feature)
	}
	if plan.BehaviorReview.status.RequiredBoundary != BehaviorReviewCheckpoint {
		t.Fatalf("decision = %+v", plan.BehaviorReview.status)
	}
}

func TestBehaviorReviewSelectsFeaturesFromDeletedPathsAndReverseDependentModules(t *testing.T) {
	t.Run("deleted path ownership", func(t *testing.T) {
		root := contentRepository(t, nil)
		installBehaviorReviewPolicy(t, root, `{"defaultRequiredAt":"on-request","features":[{"name":"deleted-content","paths":["content/data.json"],"suites":["focused"],"requiredAt":"merge"}]}`)
		installBehaviorReviewTestGuidance(t, root)
		initializeEngineGitRepository(t, root)
		gitBehaviorReview(t, root, "switch", "-c", "candidate")
		if err := os.Remove(filepath.Join(root, "content", "data.json")); err != nil {
			t.Fatal(err)
		}
		gitBehaviorReview(t, root, "add", "--update")
		gitBehaviorReview(t, root, "commit", "-m", "delete content data")
		policyEngine, err := Open(root, enginePolicyRoot(t), "")
		if err != nil {
			t.Fatal(err)
		}
		plan, err := policyEngine.PlanMergeGateExecution("main")
		if err != nil {
			t.Fatal(err)
		}
		if plan.BehaviorReview.status.State != BehaviorReviewRequired || !slices.Equal(plan.BehaviorReview.status.Required, []string{"deleted-content"}) {
			t.Fatalf("deleted-path behavior review = %+v", plan.BehaviorReview.status)
		}
	})

	t.Run("reverse dependent module impact", func(t *testing.T) {
		root := contentRepository(t, nil)
		configPath := filepath.Join(root, policy.ConfigFilename)
		config, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		updated := strings.Replace(string(config), `{"name":"content","paths":["content/**"]}`, `{"name":"content","paths":["content/**"]},{"name":"dependent","paths":["dependent/**"],"dependsOn":["content"]}`, 1)
		if updated == string(config) {
			t.Fatal("content module was not extended with a dependent module")
		}
		writeEngineFile(t, root, policy.ConfigFilename, updated, 0o600)
		installBehaviorReviewPolicy(t, root, `{"defaultRequiredAt":"on-request","features":[{"name":"dependent-content","modules":["dependent"],"suites":["focused"],"requiredAt":"merge"}]}`)
		installBehaviorReviewTestGuidance(t, root)
		initializeEngineGitRepository(t, root)
		gitBehaviorReview(t, root, "switch", "-c", "candidate")
		writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
		gitBehaviorReview(t, root, "add", "content/data.json")
		gitBehaviorReview(t, root, "commit", "-m", "change content data")
		policyEngine, err := Open(root, enginePolicyRoot(t), "")
		if err != nil {
			t.Fatal(err)
		}
		plan, err := policyEngine.PlanMergeGateExecution("main")
		if err != nil {
			t.Fatal(err)
		}
		if plan.BehaviorReview.status.State != BehaviorReviewRequired || !slices.Equal(plan.BehaviorReview.status.Required, []string{"dependent-content"}) {
			t.Fatalf("reverse-dependent behavior review = %+v", plan.BehaviorReview.status)
		}
	})
}

func TestBehaviorReviewRejectsCandidateSuiteThatWeakensBaseRequiredEvidence(t *testing.T) {
	root := contentRepository(t, nil)
	basePolicy := `{"defaultRequiredAt":"on-request","features":[{"name":"content","modules":["content"],"suites":["focused"],"requiredAt":"checkpoint"}]}`
	installBehaviorReviewPolicy(t, root, basePolicy)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "candidate")
	configPath := filepath.Join(root, policy.ConfigFilename)
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	weakened := strings.Replace(string(config), `"argv":["go","test","./..."]`, `"argv":["go","test","./content/..."]`, 1)
	if weakened == string(config) {
		t.Fatal("focused suite was not weakened")
	}
	writeEngineFile(t, root, policy.ConfigFilename, weakened, 0o600)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", policy.ConfigFilename, "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "weaken focused behavior evidence")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = policyEngine.PlanMergeGateExecution("main")
	if err == nil || !strings.Contains(err.Error(), `selected behavior review suite "focused" no longer matches its base-selected definition`) {
		t.Fatalf("plan error = %v", err)
	}
}

func TestBehaviorReviewRejectsCandidateSuiteThatWeakensAnySelectedBaseDefinition(t *testing.T) {
	basePolicy := `{"defaultRequiredAt":"on-request","features":[{"name":"content","modules":["content"],"suites":["focused"]}]}`
	for _, test := range []struct {
		name            string
		candidatePolicy string
		captureFeatures []string
	}{
		{name: "task requested", candidatePolicy: basePolicy, captureFeatures: []string{"content"}},
		{name: "candidate required", candidatePolicy: `{"defaultRequiredAt":"on-request","features":[{"name":"content","modules":["content"],"suites":["focused"],"requiredAt":"merge"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := contentRepository(t, nil)
			installBehaviorReviewPolicy(t, root, basePolicy)
			installBehaviorReviewTestGuidance(t, root)
			initializeEngineGitRepository(t, root)
			gitBehaviorReview(t, root, "switch", "-c", "candidate")
			policyEngine, err := Open(root, enginePolicyRoot(t), "")
			if err != nil {
				t.Fatal(err)
			}
			if test.captureFeatures != nil {
				intentPath := filepath.Join(t.TempDir(), "intent.md")
				if err := os.WriteFile(intentPath, []byte("Preserve the requested behavior.\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := policyEngine.CaptureBehaviorReviewIntent(t.Context(), intentPath, test.captureFeatures); err != nil {
					t.Fatal(err)
				}
			}
			if test.candidatePolicy != basePolicy {
				replaceBehaviorReviewPolicy(t, root, basePolicy, test.candidatePolicy)
			}
			configPath := filepath.Join(root, policy.ConfigFilename)
			config, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			weakened := strings.Replace(string(config), `"argv":["go","test","./..."]`, `"argv":["go","test","./content/..."]`, 1)
			if weakened == string(config) {
				t.Fatal("focused suite was not weakened")
			}
			writeEngineFile(t, root, policy.ConfigFilename, weakened, 0o600)
			writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
			gitBehaviorReview(t, root, "add", policy.ConfigFilename, "content/data.json")
			gitBehaviorReview(t, root, "commit", "-m", "weaken selected behavior evidence")
			policyEngine, err = Open(root, enginePolicyRoot(t), "")
			if err != nil {
				t.Fatal(err)
			}
			_, err = policyEngine.PlanMergeGateExecution("main")
			if err == nil || !strings.Contains(err.Error(), `selected behavior review suite "focused" no longer matches its base-selected definition`) {
				t.Fatalf("plan error = %v", err)
			}
		})
	}
}

func TestBehaviorReviewForcesSelectedSuitesOnce(t *testing.T) {
	policyEngine, _ := behaviorReviewContentCandidate(t, `{"defaultRequiredAt":"on-request","features":[{"name":"first","modules":["content"],"suites":["full"],"requiredAt":"merge"},{"name":"second","modules":["content"],"suites":["full"],"requiredAt":"merge"}]}`, nil)
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(testSuiteNames(plan.Tests.Suites), []string{"focused", "full"}) || mergeGateCommandCount(plan.Commands, "full") != 1 || mergeGateCommandCount(plan.Commands, "focused") != 1 {
		t.Fatalf("forced tests = %+v commands = %+v", plan.Tests.Suites, plan.Commands)
	}
}

func TestBehaviorReviewStatusDistinguishesMissingAndStaleReceiptsWithoutWriting(t *testing.T) {
	root := contentRepository(t, nil)
	installRequiredBehaviorReviewPolicy(t, root, "merge")
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "candidate")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	intentPath := filepath.Join(t.TempDir(), "intent.md")
	if err := os.WriteFile(intentPath, []byte("Preserve content behavior.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := policyEngine.CaptureBehaviorReviewIntent(t.Context(), intentPath, nil); err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "candidate")
	policyEngine, err = Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	missing, err := policyEngine.BehaviorReviewStatus(t.Context(), "main")
	if err != nil || missing.State != BehaviorReviewRequired || !missing.FullCandidate {
		t.Fatalf("missing status = %+v, error = %v", missing, err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	receiptPath := filepath.Join(root, filepath.FromSlash(behaviorReviewReceiptPath))
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	passed, err := policyEngine.BehaviorReviewStatus(t.Context(), "main")
	if err != nil || passed.State != BehaviorReviewPassed || passed.ReviewID == "" || !passed.FullCandidate {
		t.Fatalf("passed status = %+v, error = %v", passed, err)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil || !slices.Equal(before, after) {
		t.Fatalf("status changed receipt: before=%q after=%q error=%v", before, after, err)
	}
	writeEngineFile(t, root, "content/data.json", "{\"updated\":2}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "stale receipt")
	policyEngine, err = Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := policyEngine.BehaviorReviewStatus(t.Context(), "main")
	if err != nil || stale.State != BehaviorReviewFailed || stale.ReviewID != "" {
		t.Fatalf("stale status = %+v, error = %v", stale, err)
	}
}

func TestBehaviorReviewStatusReportsEvidenceBoundFinalStateFindings(t *testing.T) {
	root := contentRepository(t, nil)
	installRequiredBehaviorReviewPolicy(t, root, "merge")
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "candidate")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	intentPath := filepath.Join(t.TempDir(), "intent.md")
	if err := os.WriteFile(intentPath, []byte("Keep final artifacts free of task narration.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := policyEngine.CaptureBehaviorReviewIntent(t.Context(), intentPath, nil); err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "candidate")
	policyEngine, err = Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := policyEngine.PrepareBehaviorReview(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	packetData, err := os.ReadFile(filepath.Join(root, ".code-polishy-reports", "behavior-review", "packet.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packet struct {
		Intents []struct {
			ID string `json:"id"`
		} `json:"intents"`
		FinalState finalstate.Evidence `json:"final_state_evidence"`
	}
	if err := json.Unmarshal(packetData, &packet); err != nil {
		t.Fatal(err)
	}
	path := packet.FinalState.Paths[0]
	hunk := path.Hunks[0]
	review := behaviorreview.ReviewResult{
		Version: 4, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: prepared.Candidate, IntentSHA256: prepared.IntentSHA256,
		SelectionSHA256: prepared.SelectionSHA256, DecisionSHA256: prepared.DecisionSHA256,
		Behaviors: []behaviorreview.Behavior{{
			Before: "Content is available.", After: "Content is available.", Classification: "preserved", ProofIDs: []string{},
			Scope: behaviorreview.BehaviorScope{Features: []string{}, FullCandidate: true},
		}}, Findings: []string{}, FinalStateFindings: []finalstate.Finding{{
			Kind: finalstate.KindMetaNote, Path: path.Path, Line: hunk.Context.StartLine, PatchHunkSHA256: hunk.SHA256,
			IntentIDs: []string{packet.Intents[0].ID}, Summary: "The value narrates the current task.",
		}},
	}
	data, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, ".code-polishy-reports/behavior-review/result.json", string(data), 0o600)
	status, err := policyEngine.BehaviorReviewStatus(t.Context(), "main")
	if err != nil || status.State != BehaviorReviewFailed || status.FinalStateState != BehaviorReviewFailed || len(status.FinalStateFindings) != 1 || status.FinalStateFindings[0].Path != path.Path {
		t.Fatalf("status = %+v, error = %v", status, err)
	}
}

func TestBehaviorReviewGateStatusUsesPreparedPacketSelectionDigest(t *testing.T) {
	policyEngine, root := behaviorReviewContentCandidate(t, `{"defaultRequiredAt":"on-request","features":[{"name":"content","modules":["content"],"suites":["focused"]}]}`, []string{"content"})
	prepared, err := policyEngine.PrepareBehaviorReview(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	packetData, err := os.ReadFile(filepath.Join(root, ".code-polishy-reports", "behavior-review", "packet.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packet struct {
		SelectionSHA256 string `json:"selection_sha256"`
	}
	if err := json.Unmarshal(packetData, &packet); err != nil {
		t.Fatal(err)
	}
	if packet.SelectionSHA256 == "" || prepared.SelectionSHA256 != packet.SelectionSHA256 {
		t.Fatalf("prepared selection digest = %q, packet selection digest = %q", prepared.SelectionSHA256, packet.SelectionSHA256)
	}
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	status, err := policyEngine.BehaviorReviewStatus(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if plan.BehaviorReview.status.SelectionDigest != packet.SelectionSHA256 || status.SelectionDigest != packet.SelectionSHA256 {
		t.Fatalf("plan digest = %q, status digest = %q, packet selection digest = %q", plan.BehaviorReview.status.SelectionDigest, status.SelectionDigest, packet.SelectionSHA256)
	}
}

func TestBehaviorReviewStatusStaysRequiredForDirtyTaskSelectedCandidate(t *testing.T) {
	policyEngine, root := behaviorReviewContentCandidate(t, `{"defaultRequiredAt":"on-request","features":[{"name":"content","modules":["content"],"suites":["focused"]}]}`, []string{"content"})
	writeEngineFile(t, root, "content/data.json", "{\"updated\":2}\n", 0o600)
	status, err := policyEngine.BehaviorReviewStatus(t.Context(), "main")
	if err != nil || status.State != BehaviorReviewRequired || !slices.Equal(status.TaskRequested, []string{"content"}) ||
		!slices.Equal(status.Required, []string{"content"}) || !slices.Equal(status.Missing, []string{"content"}) {
		t.Fatalf("dirty status = %+v, error = %v", status, err)
	}
	report, err := policyEngine.TestPlan("main")
	if err != nil || report.BehaviorReview == nil || report.BehaviorReview.State != BehaviorReviewRequired ||
		!slices.Equal(report.BehaviorReview.Missing, []string{"content"}) {
		t.Fatalf("dirty planning report = %+v, error = %v", report, err)
	}
}

func TestDocumentationBehaviorReviewIsOptionalUnlessAFeatureSelectsIt(t *testing.T) {
	t.Run("optional", func(t *testing.T) {
		root := documentationRepository(t)
		installBehaviorReviewTestGuidance(t, root)
		initializeEngineGitRepository(t, root)
		writeEngineFile(t, root, "docs/index.md", "# Updated\n", 0o600)
		commitEngineCandidate(t, root, "documentation candidate")
		policyEngine, err := Open(root, enginePolicyRoot(t), "")
		if err != nil {
			t.Fatal(err)
		}
		report, err := policyEngine.MergeGate(t.Context(), "main")
		if err != nil || report.BehaviorReview == nil || report.BehaviorReview.State != BehaviorReviewNotRun || len(report.Findings) != 0 {
			t.Fatalf("report = %+v, error = %v", report, err)
		}
	})
	t.Run("selected", func(t *testing.T) {
		root := documentationRepository(t)
		installDocumentationBehaviorReviewPolicy(t, root, `{"defaultRequiredAt":"on-request","features":[{"name":"documentation","paths":["docs/index.md"],"suites":["docs-product-contract"],"requiredAt":"merge"}]}`)
		installBehaviorReviewTestGuidance(t, root)
		initializeEngineGitRepository(t, root)
		writeEngineFile(t, root, "docs/index.md", "# Updated\n", 0o600)
		commitEngineCandidate(t, root, "documentation candidate")
		policyEngine, err := Open(root, enginePolicyRoot(t), "")
		if err != nil {
			t.Fatal(err)
		}
		commandRunner := &recordingEngineRunner{}
		policyEngine.Runner = commandRunner
		report, err := policyEngine.MergeGate(t.Context(), "main")
		if err != nil {
			t.Fatal(err)
		}
		assertBehaviorReviewGateFindingAtLevel(t, report, testpolicy.MergeLevelDocumentation, "missing behavior review receipt")
		if report.BehaviorReview == nil || report.BehaviorReview.State != BehaviorReviewRequired || len(commandRunner.commands) != 0 {
			t.Fatalf("report = %+v commands = %v", report, commandRunner.commands)
		}
	})
}

func TestMergeGateReportsMissingRequiredBehaviorReviewReceiptBeforeCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFinding(t, report, "missing behavior review receipt")
	if len(commandRunner.commands) != 0 {
		t.Fatalf("missing receipt ran commands: %v", commandRunner.commands)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".code-polishy-reports", "behavior-review", "receipt.json")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("missing receipt validation created a receipt: %v", statErr)
	}
}

func TestCheckpointGateReportsMissingBehaviorReviewBeforeCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertCheckpointBehaviorReviewFinding(t, report, "missing behavior review receipt")
	if len(commandRunner.commands) != 0 {
		t.Fatalf("missing receipt ran commands: %v", commandRunner.commands)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("missing receipt recorded a checkpoint: %v", statErr)
	}
}

func TestDoctorReportsMissingReportArtifactIgnoreAtTheOwnedPath(t *testing.T) {
	root := contentRepository(t, nil)
	policyRoot := enginePolicyRoot(t)
	installBehaviorReviewTestGuidance(t, root)
	if err := os.Remove(filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	policyEngine, err := Open(root, policyRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	report, err := policyEngine.Doctor(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
		return finding.Check == "policy.reportArtifacts" && finding.Path == ".gitignore" &&
			finding.Subject == "workspace-ignore" && strings.Contains(finding.Message, "agents sync")
	}) {
		t.Fatalf("findings = %+v", report.Findings)
	}
}

func TestCheckpointGateRunsChangedScopeWithoutDeclaredSupplementalAndRecordsAcceptedCandidate(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	declareSupplementalMutation(&policyEngine.Repository.Config)
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.CheckpointPolicy == nil || report.CheckpointPolicy.Scope != behaviorreview.CheckpointScopeChanged ||
		report.CheckpointPolicy.ReceiptPath != behaviorreview.CheckpointReceiptPath || report.GateRunPolicy == nil || report.GateRunPolicy.Status != "passed" {
		t.Fatalf("report = %+v", report)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(report.GateRunPolicy.ReportPath))); err != nil {
		t.Fatalf("checkpoint gate report is unavailable: %v", err)
	}
	if !slices.Equal(commandRunner.commands, []string{"content-check", "focused"}) {
		t.Fatalf("checkpoint commands = %v", commandRunner.commands)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath)))
	if err != nil {
		t.Fatal(err)
	}
	var receipt behaviorreview.CheckpointReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Candidate != report.CheckpointPolicy.Candidate || receipt.Scope != behaviorreview.CheckpointScopeChanged ||
		receipt.BehaviorReview.State != gaterun.BehaviorReviewPassed || receipt.BehaviorReview.ReviewID == "" {
		t.Fatalf("checkpoint receipt = %+v", receipt)
	}
	accepted, err := behaviorreview.ReadCheckpoint(t.Context(), policyEngine.Repository)
	if err != nil || !reflect.DeepEqual(accepted, receipt) {
		t.Fatalf("ReadCheckpoint() receipt = %+v, error = %v, want %+v", accepted, err, receipt)
	}
}

func TestCheckpointGateReplaysRegressionProofsBeforeChangedChecks(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &behaviorReviewReplayRunner{candidateRoot: root, baselineStatus: 1}
	policyEngine.Runner = commandRunner
	prepareRequestedBehaviorReviewReceipt(t, policyEngine, root, commandRunner, nil)
	commandRunner.events = nil
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil || len(report.Findings) != 0 {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	if len(commandRunner.events) < 4 || commandRunner.events[0] != "replay:focused" || commandRunner.events[1] != "replay:focused" ||
		commandRunner.events[2] != "planned:content-check" || commandRunner.events[3] != "planned:focused" {
		t.Fatalf("checkpoint replay and verification order = %v", commandRunner.events)
	}
	assertBehaviorProofGateOutcome(t, root, report)
}

func TestCheckpointGateDoesNotRecordAfterChangedTestFailure(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	commandRunner := &failingEngineRunner{failure: "focused"}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.CheckpointPolicy == nil || report.CheckpointPolicy.ReceiptPath != "" ||
		report.GateRunPolicy == nil || report.GateRunPolicy.Status != "failed" {
		t.Fatalf("report = %+v", report)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed checkpoint recorded a receipt: %v", statErr)
	}
}

func TestCheckpointGateDoesNotRecordWhenGateArtifactFinalizationFails(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	policyEngine.Runner = &checkpointArtifactFailureRunner{root: root}
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err == nil {
		t.Fatal("checkpoint gate unexpectedly succeeded")
	}
	if report.CheckpointPolicy == nil || report.CheckpointPolicy.ReceiptPath != "" || report.GateRunPolicy == nil ||
		report.GateRunPolicy.Status != string(gaterun.RunOperational) {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	if report.GateRunPolicy.ReportPath != "" {
		t.Fatalf("failed gate retained a current report path: %+v", report.GateRunPolicy)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))); statErr != nil {
		t.Fatalf("bound receipt is unavailable: %v", statErr)
	}
	if _, readErr := behaviorreview.ReadCheckpoint(t.Context(), policyEngine.Repository); !errors.Is(readErr, behaviorreview.ErrStaleCheckpoint) {
		t.Fatalf("ReadCheckpoint() error = %v, want stale checkpoint", readErr)
	}
}

func TestCheckpointGateFinalizesOperationalWhenReceiptPublicationFails(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	path := filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	policyEngine.Runner = &recordingEngineRunner{}
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err == nil {
		t.Fatal("checkpoint gate unexpectedly succeeded")
	}
	if report.CheckpointPolicy == nil || report.CheckpointPolicy.ReceiptPath != "" || report.GateRunPolicy == nil ||
		report.GateRunPolicy.Status != string(gaterun.RunOperational) || report.GateRunPolicy.ReportPath == "" {
		t.Fatalf("report = %+v, error = %v", report, err)
	}
	if _, readErr := behaviorreview.ReadCheckpoint(t.Context(), policyEngine.Repository); !errors.Is(readErr, behaviorreview.ErrStaleCheckpoint) {
		t.Fatalf("ReadCheckpoint() error = %v, want stale checkpoint", readErr)
	}
}

func TestCheckpointGateRecordsDocumentationWithoutBehaviorReview(t *testing.T) {
	root := documentationRepository(t)
	initializeEngineGitRepository(t, root)
	writeEngineFile(t, root, "README.md", "# Updated\n", 0o600)
	commitEngineCandidate(t, root, "documentation candidate")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.CheckpointPolicy == nil || report.CheckpointPolicy.Scope != behaviorreview.CheckpointScopeDocumentation ||
		report.CheckpointPolicy.ReceiptPath != behaviorreview.CheckpointReceiptPath || len(commandRunner.commands) != 0 {
		t.Fatalf("report = %+v commands = %v", report, commandRunner.commands)
	}
}

func TestCheckpointGateTreatsUnchangedCandidateAsNoOp(t *testing.T) {
	root := contentRepository(t, nil)
	initializeEngineGitRepository(t, root)
	policyEngine, err := Open(root, root, "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.CheckpointGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.CheckpointPolicy == nil || report.CheckpointPolicy.Scope != "unchanged" ||
		report.CheckpointPolicy.ReceiptPath != "" || len(commandRunner.commands) != 0 {
		t.Fatalf("report = %+v commands = %v", report, commandRunner.commands)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(behaviorreview.CheckpointReceiptPath))); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unchanged checkpoint wrote a receipt: %v", statErr)
	}
}

func TestMergeGateReportsStaleBehaviorReviewReceiptBeforeCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	writeEngineFile(t, root, "content/data.json", "{\"updated\":2}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "change candidate after review")
	policyEngine, err = Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFinding(t, report, "stale behavior review receipt")
	if len(commandRunner.commands) != 0 {
		t.Fatalf("stale receipt ran commands: %v", commandRunner.commands)
	}
}

func TestMergeGateContinuesItsPlannedCommandsAfterAValidPreservedBehaviorReviewReceipt(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	commandRunner := &recordingEngineRunner{}
	policyEngine.Runner = commandRunner
	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 || report.MergePolicy == nil || report.MergePolicy.Level != testpolicy.MergeLevelRecommended {
		t.Fatalf("report = %+v", report)
	}
	for _, command := range []string{"content-check", "focused", "content-build", "offline-supply"} {
		if !slices.Contains(commandRunner.commands, command) {
			t.Fatalf("valid receipt skipped planned %q: %v", command, commandRunner.commands)
		}
	}
}

func TestMergeGateResumeReusesOnlyPassedOrdinaryTestsFromIdenticalFailedRun(t *testing.T) {
	root := requiredBehaviorReviewCandidateWithReuse(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	prepareValidBehaviorReviewReceipt(t, policyEngine, root)
	failedRunner := &failingEngineRunner{failure: "offline-supply"}
	policyEngine.Runner = failedRunner
	failed, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if failed.GateRunPolicy == nil || failed.GateRunPolicy.Status != "failed" || len(failed.Findings) == 0 {
		t.Fatalf("failed report = %+v", failed)
	}

	resumedRunner := &recordingEngineRunner{}
	policyEngine.Runner = resumedRunner
	resumed, err := policyEngine.MergeGateWithOptions(t.Context(), "main", MergeGateOptions{Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.Findings) != 0 || resumed.GateRunPolicy == nil || resumed.GateRunPolicy.Status != "passed" ||
		!slices.Equal(resumed.GateRunPolicy.ReusedPhases, []string{"focused"}) {
		t.Fatalf("resumed report = %+v", resumed)
	}
	if slices.Contains(resumedRunner.commands, "focused") {
		t.Fatalf("resumed ordinary test executed again: %v", resumedRunner.commands)
	}
	for _, command := range []string{"content-check", "content-build", "offline-supply"} {
		if !slices.Contains(resumedRunner.commands, command) {
			t.Fatalf("resume skipped non-reusable %q: %v", command, resumedRunner.commands)
		}
	}
}

func TestMergeGateResumeReplaysBehaviorProofInsteadOfReusingIt(t *testing.T) {
	root := requiredBehaviorReviewCandidateWithReuse(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	failedRunner := &behaviorReviewReplayRunner{candidateRoot: root, baselineStatus: 1, failure: "offline-supply"}
	policyEngine.Runner = failedRunner
	prepareRequestedBehaviorReviewReceipt(t, policyEngine, root, failedRunner, nil)
	failedRunner.events = nil
	failed, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if failed.GateRunPolicy == nil || failed.GateRunPolicy.Status != "failed" {
		t.Fatalf("failed report = %+v", failed)
	}

	resumedRunner := &behaviorReviewReplayRunner{candidateRoot: root, baselineStatus: 1}
	policyEngine.Runner = resumedRunner
	resumed, err := policyEngine.MergeGateWithOptions(t.Context(), "main", MergeGateOptions{Resume: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.Findings) != 0 || resumed.GateRunPolicy == nil || resumed.GateRunPolicy.Status != "passed" ||
		!slices.Contains(resumed.GateRunPolicy.ReusedPhases, "focused") {
		t.Fatalf("resumed report = %+v", resumed)
	}
	if len(resumedRunner.events) < 2 || resumedRunner.events[0] != "replay:focused" || resumedRunner.events[1] != "replay:focused" {
		t.Fatalf("resumed proof replay events = %v", resumedRunner.events)
	}
	assertBehaviorProofGateOutcome(t, root, resumed)
}

func TestMergeGateReplaysRequestedBehaviorReviewProofBeforePlannedCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &behaviorReviewReplayRunner{candidateRoot: root, baselineStatus: 1}
	policyEngine.Runner = commandRunner
	prepareRequestedBehaviorReviewReceipt(t, policyEngine, root, commandRunner, nil)
	commandRunner.events = nil

	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(commandRunner.events) < 3 || commandRunner.events[0] != "replay:focused" || commandRunner.events[1] != "replay:focused" {
		t.Fatalf("replay and planned command order = %v", commandRunner.events)
	}
	if !slices.ContainsFunc(commandRunner.events[2:], func(event string) bool { return strings.HasPrefix(event, "planned:") }) {
		t.Fatalf("replay did not continue to planned commands: %v", commandRunner.events)
	}
	assertBehaviorProofGateOutcome(t, root, report)
}

func assertBehaviorProofGateOutcome(t *testing.T, root string, report Report) {
	t.Helper()
	if report.GateRunPolicy == nil || report.GateRunPolicy.ReportPath == "" {
		t.Fatalf("gate run policy = %+v", report.GateRunPolicy)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(report.GateRunPolicy.ReportPath)))
	if err != nil {
		t.Fatal(err)
	}
	var artifact gaterun.Report
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(artifact.Commands, func(outcome gaterun.CommandOutcome) bool {
		return outcome.Category == gaterun.BehaviorProof && outcome.Status == gaterun.Passed && !outcome.Reused
	}) {
		t.Fatalf("behavior proof outcome is absent: %+v", artifact.Commands)
	}
}

func TestMergeGateRejectsForgedRecordedProofBeforePlannedCommands(t *testing.T) {
	root := requiredBehaviorReviewCandidate(t)
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	commandRunner := &behaviorReviewReplayRunner{candidateRoot: root, baselineStatus: 1}
	policyEngine.Runner = commandRunner
	prepareRequestedBehaviorReviewReceipt(t, policyEngine, root, commandRunner, forgeBehaviorReviewProofRecord)
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := behaviorreview.ValidateGateReceipt(t.Context(), policyEngine.Repository, behaviorreview.ValidateGateReceiptOptions{Base: "main", Selection: plan.BehaviorReview.selection}); err != nil {
		t.Fatalf("forged proof record was not statically valid: %v", err)
	}
	commandRunner.events = nil
	commandRunner.baselineStatus = 2

	report, err := policyEngine.MergeGate(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	assertBehaviorReviewGateFinding(t, report, "replay baseline suite")
	if len(commandRunner.events) != 1 || commandRunner.events[0] != "replay:focused" {
		t.Fatalf("forged proof ran unexpected commands: %v", commandRunner.events)
	}
}

func behaviorReviewContentCandidate(t *testing.T, behaviorReview string, captureFeatures []string) (*Engine, string) {
	t.Helper()
	root := contentRepository(t, nil)
	installBehaviorReviewPolicy(t, root, behaviorReview)
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "behavior-review")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	if captureFeatures != nil {
		intentPath := filepath.Join(t.TempDir(), "intent.md")
		if err := os.WriteFile(intentPath, []byte("Preserve the requested behavior.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := policyEngine.CaptureBehaviorReviewIntent(t.Context(), intentPath, captureFeatures); err != nil {
			t.Fatal(err)
		}
	}
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "candidate")
	policyEngine, err = Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	return policyEngine, root
}

func replaceBehaviorReviewPolicy(t *testing.T, root, oldPolicy, newPolicy string) {
	t.Helper()
	path := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), `"behaviorReview":`+oldPolicy, `"behaviorReview":`+newPolicy, 1)
	if updated == string(data) {
		t.Fatal("test behavior review policy was not replaced")
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installDocumentationBehaviorReviewPolicy(t *testing.T, root, behaviorReview string) {
	t.Helper()
	path := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), `"quality": {},`, `"quality": {},
  "verification": {"behaviorReview":`+behaviorReview+`},`, 1)
	if updated == string(data) {
		t.Fatal("documentation test policy lacks quality configuration")
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testSuiteNames(suites []policy.TestSuite) []string {
	names := make([]string, 0, len(suites))
	for _, suite := range suites {
		names = append(names, suite.Name)
	}
	return names
}

func mergeGateCommandCount(commands []MergeGateExecutionCommand, name string) int {
	count := 0
	for _, command := range commands {
		if command.Command.Name == name {
			count++
		}
	}
	return count
}

func requiredBehaviorReviewCandidate(t *testing.T) string {
	return requiredBehaviorReviewCandidateConfigured(t, false)
}

func requiredBehaviorReviewCandidateWithReuse(t *testing.T) string {
	return requiredBehaviorReviewCandidateConfigured(t, true)
}

func requiredBehaviorReviewCandidateConfigured(t *testing.T, reusable bool) string {
	t.Helper()
	root := contentRepository(t, nil)
	if reusable {
		enableReusableContentSuites(t, root)
	}
	installRequiredBehaviorReviewPolicy(t, root, "checkpoint")
	installBehaviorReviewTestGuidance(t, root)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "behavior-review")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	intentPath := filepath.Join(t.TempDir(), "intent.md")
	if err := os.WriteFile(intentPath, []byte("Preserve the requested behavior.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := policyEngine.CaptureBehaviorReviewIntent(t.Context(), intentPath, nil); err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, "content/data.json", "{\"updated\":true}\n", 0o600)
	gitBehaviorReview(t, root, "add", "content/data.json")
	gitBehaviorReview(t, root, "commit", "-m", "candidate")
	return root
}

func installRequiredBehaviorReviewPolicy(t *testing.T, root, requiredAt string) {
	installBehaviorReviewPolicy(t, root, `{"defaultRequiredAt":"`+requiredAt+`","features":[]}`)
}

func installBehaviorReviewPolicy(t *testing.T, root, behaviorReview string) {
	t.Helper()
	path := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), `"verification": {
    "mergeGate"`, `"verification": {
    "behaviorReview":`+behaviorReview+`,
    "mergeGate"`, 1)
	if updated == string(data) {
		t.Fatal("content test policy lacks verification.mergeGate")
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installBehaviorReviewTestGuidance(t *testing.T, root string) {
	t.Helper()
	if _, err := agentpolicy.Install(root, enginePolicyRoot(t)); err != nil {
		t.Fatal(err)
	}
}

func prepareValidBehaviorReviewReceipt(t *testing.T, policyEngine *Engine, root string) {
	t.Helper()
	prepared, err := policyEngine.PrepareBehaviorReview(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	review := behaviorreview.ReviewResult{
		Version: 4, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: prepared.Candidate, IntentSHA256: prepared.IntentSHA256,
		SelectionSHA256: prepared.SelectionSHA256, DecisionSHA256: prepared.DecisionSHA256,
		Behaviors: []behaviorreview.Behavior{{
			Before: "The baseline content is available.", After: "The candidate content is available.", Classification: "preserved", ProofIDs: []string{},
			Scope: behaviorreview.BehaviorScope{Features: []string{}, FullCandidate: true},
		}}, Findings: []string{}, FinalStateFindings: []finalstate.Finding{},
	}
	data, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(root, ".code-polishy-reports", "behavior-review", "result.json")
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := policyEngine.FinalizeBehaviorReview(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
}

func prepareRequestedBehaviorReviewReceipt(t *testing.T, policyEngine *Engine, root string, commandRunner *behaviorReviewReplayRunner, forge func(*testing.T, string, string)) {
	t.Helper()
	intentPath := filepath.Join(t.TempDir(), "intent.md")
	if err := os.WriteFile(intentPath, []byte("Change the content behavior.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := policyEngine.CaptureBehaviorReviewIntent(t.Context(), intentPath, nil); err != nil {
		t.Fatal(err)
	}
	evidence := "content/behavior.test.txt"
	writeEngineFile(t, root, evidence, "behavior evidence\n", 0o600)
	gitBehaviorReview(t, root, "add", evidence)
	gitBehaviorReview(t, root, "commit", "-m", "add behavior evidence")
	prepared, err := policyEngine.PrepareBehaviorReview(t.Context(), "main")
	if err != nil {
		t.Fatal(err)
	}
	proof, err := policyEngine.ProveRegression(t.Context(), "main", "focused", []string{evidence}, "content-change", 1)
	if err != nil {
		t.Fatal(err)
	}
	if forge != nil {
		forge(t, root, proof.ID)
	}
	review := behaviorreview.ReviewResult{
		Version: 4, ReviewID: prepared.ReviewID, Base: prepared.Base, Candidate: prepared.Candidate, IntentSHA256: prepared.IntentSHA256,
		SelectionSHA256: prepared.SelectionSHA256, DecisionSHA256: prepared.DecisionSHA256,
		Behaviors: []behaviorreview.Behavior{{
			Before: "The baseline content is available.", After: "The candidate content is available.", Classification: "requested", ProofIDs: []string{proof.ID},
			Scope: behaviorreview.BehaviorScope{Features: []string{}, FullCandidate: true},
		}}, Findings: []string{}, FinalStateFindings: []finalstate.Finding{},
	}
	data, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(root, ".code-polishy-reports", "behavior-review", "result.json")
	if err := os.WriteFile(resultPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := policyEngine.FinalizeBehaviorReview(t.Context(), "main"); err != nil {
		t.Fatal(err)
	}
	if len(commandRunner.events) != 2 || commandRunner.events[0] != "replay:focused" || commandRunner.events[1] != "replay:focused" {
		t.Fatalf("proof recording events = %v", commandRunner.events)
	}
}

func forgeBehaviorReviewProofRecord(t *testing.T, root, id string) {
	t.Helper()
	proofDirectory := filepath.Join(root, ".code-polishy-reports", "behavior-review", "proofs")
	baselineLog := []byte("recorded baseline failure\n")
	candidateLog := []byte("recorded candidate success\n")
	for path, data := range map[string][]byte{
		filepath.Join(proofDirectory, id+".baseline.log"):  baselineLog,
		filepath.Join(proofDirectory, id+".candidate.log"): candidateLog,
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(proofDirectory, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var proof map[string]any
	if err := json.Unmarshal(data, &proof); err != nil {
		t.Fatal(err)
	}
	baseline, ok := proof["baseline"].(map[string]any)
	if !ok {
		t.Fatalf("baseline = %#v", proof["baseline"])
	}
	candidate, ok := proof["candidate_execution"].(map[string]any)
	if !ok {
		t.Fatalf("candidate execution = %#v", proof["candidate_execution"])
	}
	baseline["exit_status"] = 1
	baseline["log_sha256"] = behaviorReviewProofDigest(baselineLog)
	candidate["exit_status"] = 0
	candidate["log_sha256"] = behaviorReviewProofDigest(candidateLog)
	updated, err := json.Marshal(proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
}

func behaviorReviewProofDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

type behaviorReviewReplayRunner struct {
	candidateRoot  string
	baselineStatus int
	failure        string
	events         []string
}

type checkpointArtifactFailureRunner struct {
	root      string
	sabotaged bool
}

func (runner *checkpointArtifactFailureRunner) Run(_ context.Context, _ string, _ policy.Command) error {
	if runner.sabotaged {
		return nil
	}
	paths, err := filepath.Glob(filepath.Join(runner.root, ".code-polishy-reports", string(gaterun.CheckpointGate), "*"))
	if err != nil {
		return err
	}
	if len(paths) != 1 {
		return errors.New("checkpoint gate artifact directory is unavailable")
	}
	if err := os.Mkdir(filepath.Join(paths[0], "latest.json"), 0o700); err != nil {
		return err
	}
	runner.sabotaged = true
	return nil
}

func (commandRunner *behaviorReviewReplayRunner) Run(_ context.Context, _ string, command policy.Command) error {
	commandRunner.events = append(commandRunner.events, "planned:"+command.Name)
	if command.Name == commandRunner.failure {
		return errors.New("configured failure")
	}
	return nil
}

func (commandRunner *behaviorReviewReplayRunner) RunWithOutput(_ context.Context, root string, command policy.Command) (runner.Result, runner.Output, error) {
	commandRunner.events = append(commandRunner.events, "replay:"+command.Name)
	data, err := os.ReadFile(filepath.Join(root, "content", "data.json"))
	if err != nil {
		return runner.Result{ExitStatus: -1}, runner.Output{}, err
	}
	if strings.Contains(string(data), "updated") {
		return runner.Result{ExitStatus: 0}, runner.Output{Stdout: []byte("candidate passed\n")}, nil
	}
	return runner.Result{ExitStatus: commandRunner.baselineStatus}, runner.Output{Stdout: []byte("baseline failed\n")}, errors.New("recorded failure")
}

func assertBehaviorReviewGateFinding(t *testing.T, report Report, message string) {
	t.Helper()
	assertBehaviorReviewGateFindingAtLevel(t, report, testpolicy.MergeLevelRecommended, message)
}

func assertBehaviorReviewGateFindingAtLevel(t *testing.T, report Report, level, message string) {
	t.Helper()
	if report.MergePolicy == nil || report.MergePolicy.Level != level || len(report.Findings) != 1 {
		t.Fatalf("merge policy = %+v findings = %+v", report.MergePolicy, report.Findings)
	}
	finding := report.Findings[0]
	if finding.Check != "policy.behaviorReview" || finding.Path != ".code-polishy-reports/behavior-review/receipt.json" ||
		finding.Subject != "gate-receipt" || !strings.Contains(finding.Message, message) {
		t.Fatalf("finding = %+v", finding)
	}
}

func assertCheckpointBehaviorReviewFinding(t *testing.T, report Report, message string) {
	t.Helper()
	if report.CheckpointPolicy == nil || report.CheckpointPolicy.Scope != behaviorreview.CheckpointScopeChanged || len(report.Findings) != 1 {
		t.Fatalf("checkpoint policy = %+v findings = %+v", report.CheckpointPolicy, report.Findings)
	}
	finding := report.Findings[0]
	if finding.Check != "policy.behaviorReview" || finding.Path != ".code-polishy-reports/behavior-review/receipt.json" ||
		finding.Subject != "gate-receipt" || !strings.Contains(finding.Message, message) {
		t.Fatalf("finding = %+v", finding)
	}
}

func enginePolicyRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err == nil {
		current := workingDirectory
		for {
			version, versionErr := os.Stat(filepath.Join(current, "VERSION"))
			schema, schemaErr := os.Stat(filepath.Join(current, "schema", "code-polishy.schema.json"))
			if versionErr == nil && schemaErr == nil && version.Mode().IsRegular() && schema.Mode().IsRegular() {
				return current
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	goExecutable, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("resolve governed Go executable: %v", err)
	}
	current := filepath.Dir(goExecutable)
	for {
		version, versionErr := os.Stat(filepath.Join(current, "VERSION"))
		schema, schemaErr := os.Stat(filepath.Join(current, "schema", "code-polishy.schema.json"))
		if versionErr == nil && schemaErr == nil && version.Mode().IsRegular() && schema.Mode().IsRegular() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatalf("governed Go executable is outside a Code Polishy policy root: %s", goExecutable)
		}
		current = parent
	}
}

func gitBehaviorReview(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
