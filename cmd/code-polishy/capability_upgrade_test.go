package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/release"
)

func TestLockPublishesTheDeltaReturnedByLaterCapabilityInspection(t *testing.T) {
	repo := newCapabilityCLIRepository(t)
	prefix := t.TempDir()
	oldRoot := installedRelease(t, strings.Repeat("a", 40))
	nextRoot := installedRelease(t, strings.Repeat("b", 40))
	changeCapabilityCLIRelease(t, oldRoot, "9.9.8", "Check selected source.")
	changeCapabilityCLIRelease(t, nextRoot, "9.9.9", "Check source and dependencies.")
	oldRoot, old := relocateCapabilityCLIRelease(t, prefix, oldRoot)
	nextRoot, next := relocateCapabilityCLIRelease(t, prefix, nextRoot)
	if err := release.WriteLock(repo, old); err != nil {
		t.Fatal(err)
	}
	arguments := []string{"--repo-root", repo, "--policy-root", nextRoot}
	status, stdout, stderr := captureRunOutput(t, append(slices.Clone(arguments), "lock"))
	for _, text := range []string{"PASS .code-polishy.lock.json requires Code Polishy 9.9.9", "CAPABILITY DELTA: available (9.9.8 -> 9.9.9)", "CHANGED check:", "docs/agent-workflows.md", "UPGRADE RECORD:"} {
		if status != 0 || stderr != "" || !strings.Contains(stdout, text) {
			t.Fatalf("lock output omitted %q: status=%d stdout=%q stderr=%q", text, status, stdout, stderr)
		}
	}
	delta := release.ReadCapabilityUpgrade(repo, next)
	if delta.Availability != "available" || delta.Outgoing == nil || delta.Outgoing.ReleaseDigest != old.ReleaseDigest {
		t.Fatalf("wrong persisted release delta: %+v", delta)
	}
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(delta.ArtifactPath)))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Protocol string                  `json:"protocol"`
		Delta    release.CapabilityDelta `json:"delta"`
	}
	if err := json.Unmarshal(data, &record); err != nil || record.Protocol != "capability-upgrade-record/v1" {
		t.Fatalf("upgrade machine record: %+v, error = %v", record, err)
	}
	if err := os.RemoveAll(oldRoot); err != nil {
		t.Fatal(err)
	}
	status, stdout, stderr = captureRunOutput(t, append(slices.Clone(arguments), "capabilities", "--format", "json"))
	inventory := decodeCapabilityCLIInventory(t, status, stdout, stderr)
	want, _ := json.Marshal(record.Delta)
	actual, _ := json.Marshal(inventory.UpgradeDelta)
	if string(actual) != string(want) {
		t.Fatalf("capability inspection diverged from lock output: got %s, want %s", actual, want)
	}
	status, stdout, stderr = captureRunOutput(t, append(slices.Clone(arguments), "capabilities", "--query", "check"))
	if status != 0 || stderr != "" || !strings.Contains(stdout, capabilityDeltaHuman(delta)) {
		t.Fatalf("human inspection lost the upgrade delta: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	assertCapabilityCLIDidNotExecute(t, repo)
}

func TestLockReportsUnavailableDeltaOnFirstAdoption(t *testing.T) {
	repo := t.TempDir()
	policyRoot := installedRelease(t, strings.Repeat("c", 40))
	status, stdout, stderr := captureRunOutput(t, []string{"--repo-root", repo, "--policy-root", policyRoot, "lock"})
	if status != 0 || stderr != "" || !strings.Contains(stdout, "CAPABILITY DELTA: unavailable (none -> 9.9.9)") || !strings.Contains(stdout, "There was no outgoing release lock") {
		t.Fatalf("first adoption delta: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
	if strings.Contains(stdout, "ADDED ") || strings.Contains(stdout, "CHANGED ") {
		t.Fatal("first adoption invented a comparison with an absent release")
	}
}

func TestCapabilityDeltaHumanBoundsChangesAndDocumentation(t *testing.T) {
	delta := release.CapabilityDelta{
		Availability: "available", Outgoing: &release.Lock{CodePolishyVersion: "9.9.8"}, Incoming: release.Lock{CodePolishyVersion: "9.9.9"},
	}
	for index := range 20 {
		delta.Added = append(delta.Added, release.CapabilityDefinition{
			Name: fmt.Sprintf("command-%02d", index), Workflows: []string{"docs/" + strings.Repeat("long-path-", 100) + ".md", "docs/second.md", "docs/third.md"},
		})
	}
	output := capabilityDeltaHuman(delta)
	if strings.Count(output, "  ADDED ") != maximumCapabilityDeltaLines || !strings.Contains(output, "12 more changes") || !strings.Contains(output, "1 more documents") || len(output) > 4096 {
		t.Fatalf("unbounded or incomplete human delta: %q", output)
	}
}

func relocateCapabilityCLIRelease(t testing.TB, prefix, source string) (string, release.Lock) {
	t.Helper()
	manifest, present, err := release.ReadManifest(source)
	if err != nil || !present {
		t.Fatalf("release fixture: present=%v error=%v", present, err)
	}
	locked := release.LockFor(manifest)
	destination := release.Directory(prefix, locked)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, destination); err != nil {
		t.Fatal(err)
	}
	return destination, locked
}
