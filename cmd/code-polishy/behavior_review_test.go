package main

import (
	"slices"
	"strings"
	"testing"
)

func TestParseBehaviorReviewOptionsAcceptsStrictPrepareAndFinalizeRequests(t *testing.T) {
	t.Parallel()
	prepare, err := parseBehaviorReviewOptions([]string{"prepare", "--intent-file=intent.md", "--base", "origin/main"})
	if err != nil || prepare.action != "prepare" || prepare.base != "origin/main" || prepare.intentFile != "intent.md" {
		t.Fatalf("prepare=%+v err=%v", prepare, err)
	}
	finalize, err := parseBehaviorReviewOptions([]string{"finalize", "--base=origin/main"})
	if err != nil || finalize.action != "finalize" || finalize.base != "origin/main" || finalize.intentFile != "" {
		t.Fatalf("finalize=%+v err=%v", finalize, err)
	}
}

func TestParseBehaviorReviewOptionsRejectsIncompleteDuplicateAndUnknownRequests(t *testing.T) {
	t.Parallel()
	for _, arguments := range [][]string{
		nil,
		{"unknown"},
		{"prepare", "--base", "origin/main"},
		{"prepare", "--intent-file", "intent.md"},
		{"prepare", "--base", "origin/main", "--base", "HEAD", "--intent-file", "intent.md"},
		{"prepare", "--base", "origin/main", "--intent-file", "intent.md", "extra"},
		{"finalize"},
		{"finalize", "--base", "origin/main", "--intent-file", "intent.md"},
		{"finalize", "--base="},
	} {
		if _, err := parseBehaviorReviewOptions(arguments); err == nil {
			t.Fatalf("accepted invalid behavior-review request %v", arguments)
		}
	}
}

func TestParseRegressionProofOptionsAcceptsRepeatedEvidenceAndRedExit(t *testing.T) {
	t.Parallel()
	options, err := parseRegressionProofOptions([]string{
		"--id", "login-fix", "--evidence=internal/login/login_test.go", "--base", "abc123", "--suite", "login-unit",
		"--evidence", "web/login.spec.ts", "--red-exit", "3",
	})
	if err != nil || options.base != "abc123" || options.suite != "login-unit" || options.id != "login-fix" || options.redExit != 3 ||
		!slices.Equal(options.evidence, []string{"internal/login/login_test.go", "web/login.spec.ts"}) {
		t.Fatalf("options=%+v err=%v", options, err)
	}
}

func TestParseRegressionProofOptionsDefaultsRedExitAndRejectsInvalidRequests(t *testing.T) {
	t.Parallel()
	options, err := parseRegressionProofOptions([]string{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix"})
	if err != nil || options.redExit != 1 {
		t.Fatalf("options=%+v err=%v", options, err)
	}
	for _, arguments := range [][]string{
		nil,
		{"--base", "abc123", "--suite", "unit", "--id", "fix"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--suite", "other"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--evidence", "test.go", "--id", "fix"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--red-exit", "0"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--red-exit", "one"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--red-exit", "1", "--red-exit", "1"},
		{"--base", "abc123", "--suite", "unit", "--evidence", "test.go", "--id", "fix", "--unknown"},
	} {
		if _, err := parseRegressionProofOptions(arguments); err == nil {
			t.Fatalf("accepted invalid regression-proof request %v", arguments)
		}
	}
}

func TestBehaviorReviewSuccessMessagesStayConciseAndAlwaysNameTheArtifact(t *testing.T) {
	t.Parallel()
	for name, message := range map[string]string{
		"prepare":  behaviorReviewPreparedMessage(".code-polishy-reports/behavior-review/packet.json", "review-123"),
		"finalize": behaviorReviewFinalizedMessage(".code-polishy-reports/behavior-review/receipt.json", "review-123"),
		"proof":    regressionProofMessage(".code-polishy-reports/behavior-review/proofs/login.json", "login"),
	} {
		if !strings.Contains(message, ".code-polishy-reports/behavior-review/") || strings.Contains(message, "\n") {
			t.Fatalf("%s message = %q", name, message)
		}
	}
}
