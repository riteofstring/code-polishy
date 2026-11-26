package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestSizeCommandProducesAgentReadableComparison(t *testing.T) {
	root, _ := newBehaviorReviewCLIRepository(t)
	writeBehaviorReviewCLIFile(t, root, "assets/large.bin", strings.Repeat("x", 4096))
	status, stdout, stderr := captureRunOutput(t, []string{
		"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t),
		"size", "--base", "main", "--format", "json",
	})
	report := engine.Report{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 {
		t.Fatalf("status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	if report.Command != "size" || report.RepositorySize == nil || report.RepositorySize.Comparison == nil ||
		report.RepositorySize.Comparison.RequestedBase != "main" || report.RepositorySize.Workspace.Bytes < 4096 ||
		!strings.HasPrefix(report.ReportPath, ".code-polishy-reports/size/") {
		t.Fatalf("size report = %+v", report.RepositorySize)
	}
}

func TestSizeShowsMeasuredBudgetGrowthWithoutChangingPolicy(t *testing.T) {
	root, _ := newBehaviorReviewCLIRepository(t)
	policyPath := filepath.Join(root, policy.ConfigFilename)
	data, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	config["quality"].(map[string]any)["lineBudget"] = map[string]any{"production": 200}
	data, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorReviewCLIFile(t, root, policy.ConfigFilename, string(data))
	before, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorReviewCLIFile(t, root, "growth.go", "package application\n\nconst Feature = 42\n")
	status, stdout, stderr := captureRunOutput(t, []string{
		"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t), "size", "--base", "main",
		"--format", "json",
	})
	var report engine.Report
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 || stderr != "" || report.RepositorySize == nil {
		t.Fatalf("size status=%d error=%v stdout=%s stderr=%s", status, err, stdout, stderr)
	}
	scopes := report.RepositorySize.SourceLines.Budget
	if len(scopes) != 1 || scopes[0].Scope != "production" || scopes[0].BaseLines == nil || scopes[0].Lines-*scopes[0].BaseLines != 2 || scopes[0].Budget != 200 {
		t.Fatalf("size lacks measured growth evidence: %+v", scopes)
	}
	after, err := os.ReadFile(policyPath)
	if err != nil || string(before) != string(after) {
		t.Fatalf("size changed policy: %v", err)
	}
}

func TestSizeCommandRejectsEvaluationSelectors(t *testing.T) {
	_, err := handleSize(context.Background(), &engine.Engine{}, []string{"--all"})
	if err == nil || !isCommandInputError(err) {
		t.Fatalf("error = %v", err)
	}
	for _, arguments := range [][]string{{"--base="}, {"--base", "main", "--base=other"}} {
		if _, err := handleSize(context.Background(), &engine.Engine{}, arguments); err == nil || !isCommandInputError(err) {
			t.Fatalf("arguments %v error = %v", arguments, err)
		}
	}
}
