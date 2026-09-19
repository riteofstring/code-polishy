package release

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradePlanRoundTripsThroughManagedStorage(t *testing.T) {
	repoRoot := t.TempDir()
	plan := upgradePlanFixture(repoRoot)
	path, err := WriteUpgradePlan(repoRoot, plan)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := CapabilityUpgradeDirectory + "/" + plan.Incoming.ReleaseDigest + "/plan.json"
	if path != wantPath {
		t.Fatalf("plan path = %q, want %q", path, wantPath)
	}
	read, err := ReadUpgradePlan(repoRoot, path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := renderUpgradePlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderUpgradePlan(read)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("round trip error=%v\ngot=%s\nwant=%s", err, got, want)
	}
	if _, err := ReadUpgradePlan(repoRoot, filepath.Join(repoRoot, path)); err == nil {
		t.Fatal("absolute upgrade plan path was accepted")
	}
}

func TestUpgradePlanAcceptsPairedLegacyArtifactPaths(t *testing.T) {
	repoRoot := t.TempDir()
	plan := upgradePlanFixture(repoRoot)
	legacyIdentity := strings.Repeat("d", 64)
	plan.CapabilityDelta.ArtifactPath = CapabilityUpgradeDirectory + "/" + legacyIdentity + "/delta.json"
	data, err := renderUpgradePlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	legacyDirectory := filepath.Join(repoRoot, filepath.FromSlash(CapabilityUpgradeDirectory), legacyIdentity)
	if err := os.MkdirAll(legacyDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDirectory, "plan.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	legacyPath := CapabilityUpgradeDirectory + "/" + legacyIdentity + "/plan.json"
	if _, err := ReadUpgradePlan(repoRoot, legacyPath); err != nil {
		t.Fatalf("paired legacy plan was rejected: %v", err)
	}
	mismatchedIdentity := strings.Repeat("e", 64)
	mismatchedDirectory := filepath.Join(repoRoot, filepath.FromSlash(CapabilityUpgradeDirectory), mismatchedIdentity)
	if err := os.MkdirAll(mismatchedDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mismatchedDirectory, "plan.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	mismatchedPath := CapabilityUpgradeDirectory + "/" + mismatchedIdentity + "/plan.json"
	if _, err := ReadUpgradePlan(repoRoot, mismatchedPath); err == nil {
		t.Fatal("plan separated from its capability record was accepted")
	}
}

func upgradePlanFixture(repoRoot string) UpgradePlan {
	outgoing := indexedLockFixture("9.9.8", strings.Repeat("8", 64))
	incoming := indexedLockFixture("9.9.9", strings.Repeat("9", 64))
	delta := newCapabilityDelta(&outgoing, incoming)
	delta.Reason = "The outgoing release capability catalog could not be authenticated; no changes were inferred."
	return UpgradePlan{
		Protocol: UpgradePlanProtocol, Outgoing: outgoing, OutgoingLockSHA256: strings.Repeat("a", 64),
		Incoming: incoming, InstallPrefix: filepath.Join(repoRoot, "prefix"), CapabilityDelta: delta,
		DiagnosticDelta: UpgradeDiagnosticDelta{
			Added:   []UpgradeDiagnostic{{Fingerprint: "added", RuleID: "policy.added", Severity: "error", Path: "app.go", Message: "new"}},
			Removed: []UpgradeDiagnostic{}, Changed: []UpgradeDiagnosticChange{},
		},
		OutgoingDiagnosticsSHA256: strings.Repeat("b", 64), IncomingDiagnosticsSHA256: strings.Repeat("c", 64),
	}
}

func indexedLockFixture(version, digest string) Lock {
	archives := make([]LockedArchive, 0, len(supportedReleaseHosts))
	for _, host := range supportedReleaseHosts {
		archives = append(archives, LockedArchive{
			Host: host, URL: "https://example.invalid/code-polishy-" + version + "-" + host + ".zip",
			SHA256: exampleDigest, Size: 1,
		})
	}
	return Lock{
		LockVersion: LockVersion, CodePolishyVersion: version, ReleaseDigest: digest,
		Features: []string{"javascript-bundle"}, Publication: &LockPublication{
			IndexURL: "https://example.invalid/release-index.json", IndexSHA256: otherDigest, Archives: archives,
		},
	}
}
