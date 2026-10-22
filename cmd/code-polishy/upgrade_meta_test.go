package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/release"
)

func TestUpgradeApplicationAuthenticatesHistoricalOutgoingRelease(t *testing.T) {
	repo, prefix := t.TempDir(), t.TempDir()
	oldRoot := installedRelease(t, strings.Repeat("a", 40))
	changeCapabilityCLIRelease(t, oldRoot, "9.9.8", "Check selected source.")
	old := historicalUpgradeCLIRelease(t, oldRoot)
	relocated := release.Directory(prefix, old)
	if err := os.MkdirAll(filepath.Dir(relocated), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldRoot, relocated); err != nil {
		t.Fatal(err)
	}
	oldRoot = relocated
	nextRoot, next := relocateCapabilityCLIRelease(t, prefix, installedRelease(t, strings.Repeat("b", 40)))
	if err := release.WriteLock(repo, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(repo, release.LockFilename))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyUpgradeRelease(oldRoot, old, true); err != nil {
		t.Fatalf("planning rejected authentic historical release: %v", err)
	}
	if err := verifyUpgradeRelease(oldRoot, old, false); err == nil {
		t.Fatal("historical schema was accepted as an incoming release")
	}
	capability, err := release.PrepareCapabilityUpgrade(repo, nextRoot, next)
	if err != nil || capability.Delta.Availability != "available" {
		t.Fatalf("historical capability comparison: %+v, error = %v", capability.Delta, err)
	}
	plan := release.UpgradePlan{
		Protocol: release.UpgradePlanProtocol, Outgoing: old, Incoming: next,
		OutgoingLockSHA256: release.LockBytesSHA256(before), InstallPrefix: prefix, CapabilityDelta: capability.Delta,
		DiagnosticDelta:           compareUpgradeDiagnostics(nil, nil),
		OutgoingDiagnosticsSHA256: strings.Repeat("c", 64), IncomingDiagnosticsSHA256: strings.Repeat("d", 64),
	}
	planPath, err := release.WriteUpgradePlan(repo, plan)
	if err != nil {
		t.Fatal(err)
	}
	invocation := invocation{repoRoot: repo, policyRoot: nextRoot}
	prepared, current, incomingRoot, err := prepareUpgradeApplication(invocation, planPath)
	if err != nil || !bytes.Equal(current, before) || incomingRoot != nextRoot || prepared.Outgoing.ReleaseDigest != old.ReleaseDigest {
		t.Fatalf("historical upgrade application failed: %+v, error = %v", prepared, err)
	}
	if err := os.WriteFile(filepath.Join(oldRoot, filepath.FromSlash(release.BinaryPath)), []byte("altered engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := prepareUpgradeApplication(invocation, planPath); err == nil || !strings.Contains(err.Error(), "outgoing release") {
		t.Fatalf("application accepted tampered outgoing engine: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(repo, release.LockFilename))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("upgrade preparation changed repository authority: %v", err)
	}
}

func historicalUpgradeCLIRelease(t *testing.T, root string) release.Lock {
	t.Helper()
	manifest, _, err := release.ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ManifestVersion = 6
	manifest.Tools.Rust, manifest.Tools.Nextest, manifest.Tools.Deadcode, manifest.Tools.Trivy = "", "", "", ""
	manifest.ReleaseDigest = manifest.Identity()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var tools map[string]json.RawMessage
	if err := json.Unmarshal(document["tools"], &tools); err != nil {
		t.Fatal(err)
	}
	delete(tools, "rust")
	delete(tools, "cargo-nextest")
	document["tools"], err = json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, release.ManifestFilename), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return publishedTestLock(manifest)
}

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
		{"plan", "--source", "/tmp/source"},
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

func TestUpgradePlanOptionsRequirePublishedCandidate(t *testing.T) {
	digest := strings.Repeat("a", 64)
	indexed, err := parseUpgradePlanOptions([]string{"--index", "https://example.invalid/index.json", "--sha256", digest, "--prefix", "/tmp/prefix"})
	if err != nil || indexed.indexURL == "" || indexed.indexSHA256 != digest || indexed.prefix != "/tmp/prefix" {
		t.Fatalf("indexed options=%+v error=%v", indexed, err)
	}
	for _, arguments := range [][]string{
		{},
		{"--source", "../code-polishy"},
		{"--index", "https://example.invalid/index.json"},
		{"--sha256", digest},
		{"--source", "../code-polishy", "--index", "https://example.invalid/index.json", "--sha256", digest},
	} {
		if _, err := parseUpgradePlanOptions(arguments); err == nil {
			t.Fatalf("arguments %v were accepted", arguments)
		}
	}
}
