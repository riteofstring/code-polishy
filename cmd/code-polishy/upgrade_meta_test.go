package main

import (
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
