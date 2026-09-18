package main

import (
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/release"
)

func TestUpgradeDiagnosticDeltaSeparatesPolicyEffects(t *testing.T) {
	before := []release.UpgradeDiagnostic{
		{Fingerprint: "changed", RuleID: "policy.changed", Severity: "warning", Path: "changed.go", Message: "before"},
		{Fingerprint: "removed", RuleID: "policy.removed", Severity: "error", Path: "removed.go", Message: "removed"},
	}
	after := []release.UpgradeDiagnostic{
		{Fingerprint: "added", RuleID: "policy.added", Severity: "error", Path: "added.go", Message: "added"},
		{Fingerprint: "changed", RuleID: "policy.changed", Severity: "error", Path: "changed.go", Message: "after"},
	}
	delta := compareUpgradeDiagnostics(before, after)
	if len(delta.Added) != 1 || delta.Added[0].Fingerprint != "added" ||
		len(delta.Removed) != 1 || delta.Removed[0].Fingerprint != "removed" ||
		len(delta.Changed) != 1 || delta.Changed[0].After.Message != "after" || !upgradeDeltaHasNewErrors(delta) {
		t.Fatalf("diagnostic delta = %+v", delta)
	}
	if !sameUpgradeDiagnosticDelta(delta, compareUpgradeDiagnostics(before, after)) {
		t.Fatal("equivalent diagnostic deltas did not compare equal")
	}
}

func TestUpgradeMetaRejectsIncompleteCommandsWithoutOpeningTheRepository(t *testing.T) {
	tests := [][]string{
		nil,
		{"plan"},
		{"plan", "--index", "https://example.invalid/index.json"},
		{"plan", "--source", "/tmp/source", "--index", "https://example.invalid/index.json", "--sha256", strings.Repeat("a", 64)},
		{"apply"},
		{"apply", "--accept-new-findings"},
		{"unknown"},
	}
	for _, arguments := range tests {
		if status := handleUpgradeMeta(invocation{arguments: arguments}); status != 2 {
			t.Fatalf("arguments=%v status=%d", arguments, status)
		}
	}
}

func TestUpgradePlanOptionsSelectOneCandidateSource(t *testing.T) {
	digest := strings.Repeat("a", 64)
	indexed, err := parseUpgradePlanOptions([]string{"--index", "https://example.invalid/index.json", "--sha256", digest, "--prefix", "/tmp/prefix"})
	if err != nil || indexed.indexURL == "" || indexed.indexSHA256 != digest || indexed.source != "" || indexed.prefix != "/tmp/prefix" {
		t.Fatalf("indexed options=%+v error=%v", indexed, err)
	}
	source, err := parseUpgradePlanOptions([]string{"--source", "../code-polishy"})
	if err != nil || source.source != "../code-polishy" || source.indexURL != "" || source.indexSHA256 != "" {
		t.Fatalf("source options=%+v error=%v", source, err)
	}
	for _, arguments := range [][]string{
		{},
		{"--source", "../code-polishy", "--sha256", digest},
		{"--index", "https://example.invalid/index.json"},
		{"--sha256", digest},
		{"--source", "../code-polishy", "--index", "https://example.invalid/index.json", "--sha256", digest},
	} {
		if _, err := parseUpgradePlanOptions(arguments); err == nil {
			t.Fatalf("arguments %v were accepted", arguments)
		}
	}
}
