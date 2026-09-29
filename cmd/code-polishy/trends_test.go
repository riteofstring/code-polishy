package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func TestTrendsCommandProducesAgentReadableHistory(t *testing.T) {
	root, _ := newBehaviorReviewCLIRepository(t)
	status, stdout, stderr := captureRunOutput(t, []string{
		"--repo-root", root, "--policy-root", behaviorReviewCLIPolicyRoot(t),
		"trends", "--branch", "main", "--rewrite-days", "30", "--format", "json",
	})
	report := engine.Report{}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil || status != 0 {
		t.Fatalf("status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	if report.Command != "trends" || report.RepositoryTrends == nil || report.RepositoryTrends.History.Branch != "main" ||
		report.RepositoryTrends.History.RewriteDays != 30 ||
		report.RepositoryTrends.History.Changes == 0 || !strings.HasPrefix(report.ReportPath, ".code-polishy-reports/trends/") {
		t.Fatalf("trends report = %+v", report.RepositoryTrends)
	}
}

func TestTrendsCommandRejectsInvalidOptions(t *testing.T) {
	invalid := [][]string{
		{"--all"},
		{"--since=2026-13-01"},
		{"--since", "last week"},
		{"--branch="},
		{"--branch", "main", "--branch=other"},
		{"--since", "2026-01-05", "--since=2026-02-02"},
		{"--rewrite-days", "0"},
		{"--rewrite-days=two"},
		{"--rewrite-days", "7", "--rewrite-days=9"},
		{"main"},
	}
	for _, arguments := range invalid {
		if _, err := handleTrends(context.Background(), &engine.Engine{}, arguments); err == nil || !isCommandInputError(err) {
			t.Errorf("arguments %v error = %v", arguments, err)
		}
	}
}
