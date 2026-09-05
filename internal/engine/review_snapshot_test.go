package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestUpgradeReviewRetainsBaseFeatureForHistoricallyOwnedTest(t *testing.T) {
	t.Parallel()
	root := contentRepository(t, nil)
	installBehaviorReviewTestGuidance(t, root)
	configPath := filepath.Join(root, policy.ConfigFilename)
	candidate, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var historical map[string]any
	if err := json.Unmarshal(candidate, &historical); err != nil {
		t.Fatal(err)
	}
	historical["version"] = 3
	delete(historical["tests"].(map[string]any), "ownership")
	historical["verification"].(map[string]any)["behaviorReview"] = map[string]any{
		"features": []any{map[string]any{"name": "content", "modules": []string{"content"}, "suites": []string{"focused"}, "requiredAt": "merge"}},
	}
	data, err := json.Marshal(historical)
	if err != nil {
		t.Fatal(err)
	}
	writeEngineFile(t, root, policy.ConfigFilename, string(data), 0o600)
	writeEngineFile(t, root, "content/probe_test.go", "package content\n", 0o600)
	initializeEngineGitRepository(t, root)
	gitBehaviorReview(t, root, "switch", "-c", "candidate")
	writeEngineFile(t, root, policy.ConfigFilename, string(candidate), 0o600)
	writeEngineFile(t, root, "content/probe_test.go", "package content\nvar Changed = true\n", 0o600)
	gitBehaviorReview(t, root, "add", "--all")
	gitBehaviorReview(t, root, "commit", "-m", "upgrade candidate policy and change test")
	policyEngine, err := Open(root, enginePolicyRoot(t), "")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := policyEngine.PlanMergeGateExecution("main")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(plan.BehaviorReview.status.Required, "content") ||
		len(plan.BehaviorReview.baseSelectedSuites) != 1 || plan.BehaviorReview.baseSelectedSuites[0].Name != "focused" {
		t.Fatalf("upgrade lost historical test ownership or required evidence: %+v", plan.BehaviorReview)
	}
}
