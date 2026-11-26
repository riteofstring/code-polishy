package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestDesignContextPreservesDirtyFilesAndPlannedPaths(t *testing.T) {
	root, policyRoot := newDesignContextCLIRepository(t)
	writeBehaviorReviewCLIFile(t, root, "value.go", "package application\nconst Staged = 1\n")
	gitBehaviorReviewCLI(t, root, "add", "value.go")
	writeBehaviorReviewCLIFile(t, root, "value.go", "package application\nconst Pending = 2\n")
	staged := contextGitOutput(t, root, "diff", "--cached")
	unstaged := contextGitOutput(t, root, "diff")
	before := contextGitOutput(t, root, "status", "--porcelain")
	arguments := []string{"--repo-root", root, "--policy-root", policyRoot, "design-context", "--files", "value.go", "--new-path", "retrospective/README.md", "--format", "json"}
	for _, features := range [][]string{nil, {"--feature", "checkout"}} {
		status, stdout, stderr := captureRunOutput(t, append(slices.Clone(arguments), features...))
		var report engine.Report
		if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 || stderr != "" || report.RepositoryContext == nil || report.RepositoryContext.Review.Required != (len(features) > 0) {
			t.Fatalf("dirty context: status=%d report=%+v error=%v stderr=%q", status, report, err, stderr)
		}
		if report.RequestedSelection == nil || !slices.Equal(report.RequestedSelection.Planned, []string{"retrospective/README.md"}) || !slices.Equal(report.RequestedSelection.Expanded, []string{"value.go"}) || before != contextGitOutput(t, root, "status", "--porcelain") {
			t.Fatalf("planned paths or dirty files changed: %+v", report.RequestedSelection)
		}
	}
	if staged != contextGitOutput(t, root, "diff", "--cached") || unstaged != contextGitOutput(t, root, "diff") {
		t.Fatal("context altered staged or unstaged contents")
	}
	for _, path := range []string{"retrospective", ".code-polishy-reports/behavior-review", "procedure-must-not-run", "executed-check"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("context created %s: %v", path, err)
		}
	}
}

func contextGitOutput(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func TestDesignContextAcceptsOnlyPlannedPaths(t *testing.T) {
	root, policyRoot := newDesignContextCLIRepository(t)
	status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", root, "--policy-root", policyRoot, "design-context", "--new-path", "retrospective/README.md", "--format", "json"})
	if status != 0 || stderr != "" || !strings.Contains(stdout, `"planned"`) || !strings.Contains(stdout, "retrospective/README.md") {
		t.Fatalf("context: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	status, stdout, stderr = captureRunOutput(t, []string{"--repo-root", root, "--policy-root", policyRoot, "design-context", "--new-path", "planned.go", "--feature", "checkout", "--format", "json"})
	var report engine.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 || stderr != "" || report.RepositoryContext == nil || !report.RepositoryContext.Review.Required || !slices.Equal(report.RepositoryContext.Review.SelectedFeatures, []string{"checkout"}) {
		t.Fatalf("review discovery: status=%d report=%+v error=%v stderr=%q", status, report, err, stderr)
	}
}

func TestDesignContextDiscoversPolicyReviewBeforeDirectCapture(t *testing.T) {
	for _, requiredAt := range []string{"merge", "checkpoint"} {
		t.Run(requiredAt, func(t *testing.T) {
			root, policyRoot := newDesignContextCLIRepository(t)
			path := filepath.Join(root, ".code-polishy.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			config["verification"].(map[string]any)["behaviorReview"].(map[string]any)["defaultRequiredAt"] = requiredAt
			data, err = json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			writeBehaviorReviewCLIFile(t, root, ".code-polishy.json", string(data))
			common := []string{"--repo-root", root, "--policy-root", policyRoot}
			status, stdout, stderr := captureRunOutput(t, append(slices.Clone(common), "design-context", "--new-path", "planned.go", "--format", "json"))
			var report engine.Report
			if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 || stderr != "" || report.RepositoryContext == nil || !report.RepositoryContext.Review.Required || !slices.Equal(report.RepositoryContext.Review.SelectedFeatures, []string{"checkout"}) {
				t.Fatalf("policy review: status=%d report=%+v error=%v stderr=%q", status, report, err, stderr)
			}
			if _, err := os.Stat(filepath.Join(root, ".code-polishy-reports", "behavior-review")); !os.IsNotExist(err) {
				t.Fatalf("discovery captured intent: %v", err)
			}
			gitBehaviorReviewCLI(t, root, "add", ".code-polishy.json")
			gitBehaviorReviewCLI(t, root, "commit", "-m", "Require review")
			status, stdout, stderr = captureRunOutputWithStdin(t, append(common, "behavior-review", "capture-intent", "--intent-file", "-"), "Add the planned behavior.\n")
			assertBehaviorReviewCLISuccess(t, status, stdout, stderr)
			journal := readBehaviorReviewCLIIntentJournal(t, root)
			if !strings.Contains(stdout, "features none") || len(journal.Requirements) != 0 {
				t.Fatalf("policy discovery became a caller feature request: %+v", journal)
			}
		})
	}
}

func TestDesignContextDiscoversEmptyModuleReviewBeforeDirectCapture(t *testing.T) {
	root, policyRoot := newDesignContextCLIRepository(t)
	data, err := os.ReadFile(filepath.Join(root, policy.ConfigFilename))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	config["modules"] = append(config["modules"].([]any),
		policy.Module{Name: "payments", Paths: []string{"payments/**"}},
		policy.Module{Name: "checkout", Paths: []string{"checkout/**"}, DependsOn: []string{"payments"}},
		policy.Module{Name: "storefront", Paths: []string{"storefront/**"}, DependsOn: []string{"checkout"}},
		policy.Module{Name: "catalog", Paths: []string{"catalog/**"}},
	)
	config["verification"].(map[string]any)["behaviorReview"] = &policy.BehaviorReviewPolicy{
		DefaultRequiredAt: "on-request",
		Features: []policy.BehaviorReviewFeature{
			{Name: "payment-review", Description: "Payment behavior.", Modules: []string{"payments"}, Suites: []string{"regression"}, RequiredAt: "checkpoint"},
			{Name: "checkout-review", Description: "Checkout behavior.", Modules: []string{"checkout"}, Suites: []string{"regression"}, RequiredAt: "merge"},
			{Name: "storefront-review", Description: "Storefront behavior.", Modules: []string{"storefront"}, Suites: []string{"regression"}, RequiredAt: "merge"},
			{Name: "unrelated-review", Description: "Application behavior.", Modules: []string{"application"}, Suites: []string{"regression"}, RequiredAt: "merge"},
			{Name: "optional-review", Description: "Optional payment behavior.", Modules: []string{"payments"}, Suites: []string{"regression"}},
		},
	}
	data, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorReviewCLIFile(t, root, policy.ConfigFilename, string(data))
	gitBehaviorReviewCLI(t, root, "add", policy.ConfigFilename)
	gitBehaviorReviewCLI(t, root, "commit", "-m", "Declare empty module review requirements")
	common := []string{"--repo-root", root, "--policy-root", policyRoot}
	for _, testCase := range []struct {
		module   string
		features []string
	}{
		{module: "payments", features: []string{"checkout-review", "payment-review", "storefront-review"}},
		{module: "checkout", features: []string{"checkout-review", "storefront-review"}},
		{module: "storefront", features: []string{"storefront-review"}},
		{module: "catalog", features: []string{}},
	} {
		t.Run(testCase.module, func(t *testing.T) {
			status, stdout, stderr := captureRunOutput(t, append(slices.Clone(common), "design-context", "--module", testCase.module, "--format", "json"))
			var report engine.Report
			if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 || stderr != "" || report.RepositoryContext == nil || report.RequestedSelection == nil {
				t.Fatalf("module context: status=%d report=%+v error=%v stderr=%q", status, report, err, stderr)
			}
			review := report.RepositoryContext.Review
			if review.Required != (len(testCase.features) > 0) || !slices.Equal(review.SelectedFeatures, testCase.features) || len(report.RequestedSelection.Expanded) != 0 {
				t.Fatalf("empty module review = %+v, selection = %+v", review, report.RequestedSelection)
			}
			assertCapabilityCLIDidNotExecute(t, root)
			if status := contextGitOutput(t, root, "status", "--porcelain"); status != "" {
				t.Fatalf("context dirtied checkout: %s", status)
			}
		})
	}
	status, stdout, stderr := captureRunOutputWithStdin(t, append(common, "behavior-review", "capture-intent", "--intent-file", "-"), "Implement the initial payment behavior.\n")
	assertBehaviorReviewCLISuccess(t, status, stdout, stderr)
}
