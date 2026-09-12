package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
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

func TestSizeCommandRejectsEvaluationSelectors(t *testing.T) {
	_, err := handleSize(context.Background(), &engine.Engine{}, []string{"--all"})
	if err == nil || !isCommandInputError(err) || !strings.Contains(err.Error(), "flag provided but not defined: -all") {
		t.Fatalf("error = %v", err)
	}
	for _, arguments := range [][]string{{"--base="}, {"--base", "main", "--base=other"}} {
		if _, err := handleSize(context.Background(), &engine.Engine{}, arguments); err == nil || !isCommandInputError(err) {
			t.Fatalf("arguments %v error = %v", arguments, err)
		}
	}
}
