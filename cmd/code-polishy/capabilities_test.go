package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/release"
)

func TestCapabilitiesCLIUsesExactLockedCatalogAndRepositoryDeclarations(t *testing.T) {
	root := newCapabilityCLIRepository(t)
	first := installedRelease(t, strings.Repeat("a", 40))
	second := installedRelease(t, strings.Repeat("b", 40))
	changeCapabilityCLIRelease(t, second, "9.9.8", "Validate repository source.")
	for _, policyRoot := range []string{first, second} {
		manifest, present, err := release.ReadManifest(policyRoot)
		if err != nil || !present {
			t.Fatalf("release fixture: %v", err)
		}
		if err := release.WriteLock(root, release.LockFor(manifest)); err != nil {
			t.Fatal(err)
		}
		arguments := []string{"--repo-root", root, "--policy-root", policyRoot, "capabilities"}
		status, stdout, stderr := captureRunOutput(t, append(slices.Clone(arguments), "--format", "json"))
		inventory := decodeCapabilityCLIInventory(t, status, stdout, stderr)
		if inventory.LockedRelease == nil || inventory.LockedRelease.CodePolishyVersion != manifest.CodePolishyVersion || inventory.ReleaseCatalog.SHA256 != manifest.CapabilityCatalogSHA256 {
			t.Fatalf("wrong locked release: %+v", inventory)
		}
		for _, id := range []string{"release:check", "project:cli", "module:application:payments", "check:must-not-run", "behavior-feature:checkout"} {
			if entry := capabilityCLIEntry(t, inventory, id); entry.Availability != "available" {
				t.Fatalf("%s unavailable: %+v", id, entry)
			}
		}
		if entry := capabilityCLIEntry(t, inventory, "check:absent-scope"); entry.Availability != "inapplicable" {
			t.Fatalf("unmatched check scope = %+v", entry)
		}
		if entry := capabilityCLIEntry(t, inventory, "release:check"); entry.Source.Version != manifest.CodePolishyVersion || entry.Description == "" {
			t.Fatalf("release command = %+v", entry)
		}
		status, stdout, stderr = captureRunOutput(t, arguments)
		if status != 0 || stderr != "" || !strings.Contains(stdout, "LOCKED RELEASE: "+manifest.CodePolishyVersion) || !strings.Contains(stdout, "BEHAVIOR-FEATURE checkout (available)") {
			t.Fatalf("human inventory: status=%d stdout=%q stderr=%q", status, stdout, stderr)
		}
	}
	assertCapabilityCLIDidNotExecute(t, root)
}

func TestCapabilitiesCLIQueriesRemainDeterministicAndCannotActivateReview(t *testing.T) {
	root := newCapabilityCLIRepository(t)
	policyRoot := installedRelease(t, strings.Repeat("c", 40))
	manifest, _, err := release.ReadManifest(policyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := release.WriteLock(root, release.LockFor(manifest)); err != nil {
		t.Fatal(err)
	}
	arguments := []string{"--repo-root", root, "--policy-root", policyRoot, "capabilities", "--query", "ＰＵＲＣＨＡＳＥ\u00a0COMPLETION", "--format=json"}
	status, first, stderr := captureRunOutput(t, arguments)
	inventory := decodeCapabilityCLIInventory(t, status, first, stderr)
	if inventory.Activation != "explicit-only" || len(inventory.Capabilities) != 1 || inventory.Capabilities[0].Name != "checkout" || inventory.Capabilities[0].Enforcement[0] != "on-request" {
		t.Fatalf("query selected incorrect candidates: %+v", inventory)
	}
	status, second, stderr := captureRunOutput(t, arguments)
	if status != 0 || stderr != "" || second != first {
		t.Fatalf("query changed its output: status=%d stderr=%q", status, stderr)
	}
	assertCapabilityCLIDidNotExecute(t, root)
}

func TestCapabilitiesCLIReportsUnavailableCatalogWithoutAmbientFallback(t *testing.T) {
	root := newCapabilityCLIRepository(t)
	policyRoot := installedRelease(t, strings.Repeat("d", 40))
	manifest, _, err := release.ReadManifest(policyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := release.WriteLock(root, release.LockFor(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(policyRoot, release.CapabilityCatalogPath)); err != nil {
		t.Fatal(err)
	}
	status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", root, "--policy-root", policyRoot, "capabilities", "--format", "json"})
	inventory := decodeCapabilityCLIInventory(t, status, stdout, stderr)
	if inventory.ReleaseCatalog.Availability != "unavailable" || inventory.ReleaseCatalog.Reason == "" || inventory.LockedRelease.ReleaseDigest != manifest.ReleaseDigest {
		t.Fatalf("missing catalog = %+v", inventory)
	}
	if slices.ContainsFunc(inventory.Capabilities, func(entry engine.CapabilityEntry) bool { return entry.Source.Kind == "locked-release" }) {
		t.Fatal("invented release capabilities without authenticated evidence")
	}
	if capabilityCLIEntry(t, inventory, "behavior-feature:checkout").Name != "checkout" {
		t.Fatal("unavailable release catalog discarded repository declarations")
	}
}

func newCapabilityCLIRepository(t *testing.T) string {
	t.Helper()
	root, _ := newBehaviorReviewCLIBaseRepositoryWithReviewPolicy(t, `{"features":[{"name":"checkout","description":"Complete purchases and confirm payment.","aliases":["purchase completion"],"modules":["application"],"suites":["regression"]}]}`)
	data, err := os.ReadFile(filepath.Join(root, ".code-polishy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	config["project"].(map[string]any)["capabilities"] = []string{"cli"}
	config["modules"].([]any)[0].(map[string]any)["capabilities"] = []string{"payments"}
	config["checks"] = append(config["checks"].([]any),
		map[string]any{"name": "must-not-run", "provides": []string{"lint"}, "paths": []string{"value.go"}, "argv": []string{"sh", "guard.sh"}, "runOn": []string{"check"}},
		map[string]any{"name": "absent-scope", "provides": []string{"lint"}, "paths": []string{"absent/**"}, "argv": []string{"sh", "guard.sh"}, "runOn": []string{"check"}},
	)
	data, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeBehaviorReviewCLIFile(t, root, ".code-polishy.json", string(data))
	writeBehaviorReviewCLIFile(t, root, "guard.sh", "#!/bin/sh\ntouch executed-check\n")
	gitBehaviorReviewCLI(t, root, "add", ".code-polishy.json", "guard.sh")
	gitBehaviorReviewCLI(t, root, "commit", "-m", "Declare discoverable capabilities")
	return root
}

func decodeCapabilityCLIInventory(t testing.TB, status int, stdout, stderr string) engine.CapabilityInventory {
	t.Helper()
	var inventory engine.CapabilityInventory
	if err := json.Unmarshal([]byte(stdout), &inventory); err != nil || status != 0 || stderr != "" || inventory.Protocol != "capabilities/v1" {
		t.Fatalf("capabilities: status=%d error=%v stdout=%q stderr=%q", status, err, stdout, stderr)
	}
	return inventory
}

func capabilityCLIEntry(t testing.TB, inventory engine.CapabilityInventory, id string) engine.CapabilityEntry {
	t.Helper()
	for _, entry := range inventory.Capabilities {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("inventory omitted %s", id)
	return engine.CapabilityEntry{}
}

func assertCapabilityCLIDidNotExecute(t testing.TB, root string) {
	t.Helper()
	for _, path := range []string{"executed-check", ".code-polishy-reports/behavior-review/intent-journal.json", ".code-polishy-reports/behavior-review/packet.json"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("discovery produced %s: %v", path, err)
		}
	}
}

func changeCapabilityCLIRelease(t testing.TB, root, version, description string) {
	t.Helper()
	manifest, _, err := release.ReadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, release.CapabilityCatalogPath))
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "Check selected source.", description, 1))
	digest := sha256.Sum256(data)
	manifest.CapabilityCatalogSHA256 = hex.EncodeToString(digest[:])
	manifest.CodePolishyVersion = version
	for index := range manifest.Entries {
		if manifest.Entries[index].Path == release.CapabilityCatalogPath {
			manifest.Entries[index].SHA256 = manifest.CapabilityCatalogSHA256
		}
	}
	manifest.ContentDigest, manifest.ReleaseDigest = release.EntriesDigest(manifest.Entries), manifest.Identity()
	if err := os.WriteFile(filepath.Join(root, release.CapabilityCatalogPath), data, 0o644); err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, release.ManifestFilename), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
