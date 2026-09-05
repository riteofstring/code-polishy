package release

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCapabilityUpgradePersistsExactChangesAndPreservesRepeatedLock(t *testing.T) {
	t.Parallel()
	repo, oldRoot, nextRoot, old, next := capabilityUpgradeFixture(t)
	result, err := WriteReleaseLock(repo, nextRoot)
	if err != nil || !result.Changed || !sameCapabilityLock(result.Lock, next) {
		t.Fatalf("upgrade = %+v, error = %v", result, err)
	}
	delta := result.Delta
	assertCapabilityUpgradeChanges(t, delta, old)
	assertCapabilityUpgradeLock(t, repo, next)
	first := readCapabilityUpgradeArtifact(t, repo, delta)
	if err := os.RemoveAll(oldRoot); err != nil {
		t.Fatal(err)
	}
	if inspected := ReadCapabilityUpgrade(repo, next); !bytes.Equal(render(t, inspected), render(t, delta)) {
		t.Fatalf("inspection after removal of old installation changed the delta: %+v", inspected)
	}
	repeated, err := WriteReleaseLock(repo, nextRoot)
	if err != nil || repeated.Changed || !bytes.Equal(render(t, repeated.Delta), render(t, delta)) {
		t.Fatalf("repeated lock lost original upgrade: %+v, error = %v", repeated, err)
	}
	if !bytes.Equal(first, readCapabilityUpgradeArtifact(t, repo, delta)) {
		t.Fatal("repeated lock changed the stored upgrade evidence")
	}
}

func assertCapabilityUpgradeChanges(t *testing.T, delta CapabilityDelta, old Lock) {
	t.Helper()
	if delta.Availability != "available" || delta.Outgoing == nil || !sameCapabilityLock(*delta.Outgoing, old) || delta.Incoming.CodePolishyVersion != "9.9.9" {
		t.Fatalf("upgrade release identities = %+v", delta)
	}
	if len(delta.Added) != 1 || delta.Added[0].Name != "task-start" || len(delta.Removed) != 1 || delta.Removed[0].Name != "legacy-command" || len(delta.Changed) != 1 {
		t.Fatalf("upgrade changes = %+v", delta)
	}
	change := delta.Changed[0]
	if change.Before.Name != "check" || change.Before.Description != "Check selected source." || change.After.Description != "Check source and dependencies." || !slices.Equal(change.After.Workflows, []string{"docs/agent-workflows.md"}) {
		t.Fatalf("changed capability omitted exact before/after metadata: %+v", change)
	}
}

func TestCapabilityUpgradeDoesNotInferMissingReleaseMetadata(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"outgoing lock", "outgoing catalog", "incoming catalog", "outgoing installation", "older manifest"} {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			repo, oldRoot, nextRoot, _, next := capabilityUpgradeFixture(t)
			switch missing {
			case "outgoing lock":
				if err := os.Remove(filepath.Join(repo, LockFilename)); err != nil {
					t.Fatal(err)
				}
			case "outgoing catalog":
				removeCapabilityUpgradeInput(t, oldRoot, CapabilityCatalogPath)
			case "incoming catalog":
				removeCapabilityUpgradeInput(t, nextRoot, CapabilityCatalogPath)
			case "outgoing installation":
				if err := os.RemoveAll(oldRoot); err != nil {
					t.Fatal(err)
				}
			case "older manifest":
				manifest, _, err := ReadManifest(oldRoot)
				if err != nil {
					t.Fatal(err)
				}
				manifest.ManifestVersion, manifest.CapabilityCatalogSHA256 = 5, ""
				manifest.ReleaseDigest = manifest.Identity()
				old := LockFor(manifest)
				writeCapabilityManifest(t, oldRoot, manifest)
				if err := os.Rename(oldRoot, Directory(filepath.Dir(filepath.Dir(oldRoot)), old)); err != nil {
					t.Fatal(err)
				}
				if err := WriteLock(repo, old); err != nil {
					t.Fatal(err)
				}
			}
			result, err := WriteReleaseLock(repo, nextRoot)
			if err != nil || !result.Changed || result.Delta.Availability != "unavailable" || result.Delta.Reason == "" {
				t.Fatalf("missing %s: %+v, error = %v", missing, result, err)
			}
			if len(result.Delta.Added)+len(result.Delta.Removed)+len(result.Delta.Changed) != 0 {
				t.Fatalf("invented changes without both catalogs: %+v", result.Delta)
			}
			if inspected := ReadCapabilityUpgrade(repo, next); !bytes.Equal(render(t, inspected), render(t, result.Delta)) {
				t.Fatalf("missing evidence changed on inspection: %+v", inspected)
			}
			assertCapabilityUpgradeLock(t, repo, next)
		})
	}
}

func TestCapabilityUpgradeRejectsAlteredEvidence(t *testing.T) {
	t.Parallel()
	repo, _, nextRoot, _, next := capabilityUpgradeFixture(t)
	result, err := WriteReleaseLock(repo, nextRoot)
	if err != nil {
		t.Fatal(err)
	}
	original := readCapabilityUpgradeArtifact(t, repo, result.Delta)
	cases := map[string]func(*capabilityUpgradeRecord){
		"changed description":  func(record *capabilityUpgradeRecord) { record.Delta.Added[0].Description = "Invented capability." },
		"discarded removal":    func(record *capabilityUpgradeRecord) { record.Delta.Removed = []CapabilityDefinition{} },
		"wrong lock":           func(record *capabilityUpgradeRecord) { record.Delta.Incoming.ReleaseDigest = otherDigest },
		"wrong catalog digest": func(record *capabilityUpgradeRecord) { record.Delta.IncomingCatalogSHA256 = otherDigest },
		"rehashed catalog": func(record *capabilityUpgradeRecord) {
			record.Incoming.Catalog = bytes.ReplaceAll(record.Incoming.Catalog, []byte("dependencies"), []byte("anything"))
			record.Incoming.Manifest.CapabilityCatalogSHA256 = capabilityContentSHA256(record.Incoming.Catalog)
			record.Delta.IncomingCatalogSHA256 = record.Incoming.Manifest.CapabilityCatalogSHA256
		},
		"omitted proof": func(record *capabilityUpgradeRecord) { record.Incoming = nil },
		"null changes":  func(record *capabilityUpgradeRecord) { record.Delta.Added = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var record capabilityUpgradeRecord
			if err := json.Unmarshal(original, &record); err != nil {
				t.Fatal(err)
			}
			mutate(&record)
			data, err := renderCapabilityUpgradeRecord(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseCapabilityUpgradeRecord(data, next); err == nil {
				t.Fatal("accepted altered upgrade evidence")
			}
		})
	}
	for _, data := range [][]byte{
		bytes.Replace(original, []byte(`"protocol":`), []byte(`"unknown": true, "protocol":`), 1),
		bytes.Replace(original, []byte(`"protocol":`), []byte(`"protocol": "ignored", "protocol":`), 1),
		bytes.Replace(original, []byte(`"protocol":`), []byte(`"Protocol":`), 1),
		bytes.Repeat([]byte(" "), MaximumCapabilityUpgradeBytes+1),
	} {
		if _, err := parseCapabilityUpgradeRecord(data, next); err == nil {
			t.Fatal("accepted a malformed upgrade document")
		}
	}
	path := filepath.Join(repo, filepath.FromSlash(result.Delta.ArtifactPath))
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if inspected := ReadCapabilityUpgrade(repo, next); inspected.Availability != "unavailable" || !strings.Contains(inspected.Reason, "invalid") {
		t.Fatalf("corrupt record was not visibly unavailable: %+v", inspected)
	}
	assertCapabilityUpgradeLock(t, repo, next)
}

func TestCapabilityUpgradePreservesLockWhenRecordCannotBePublished(t *testing.T) {
	t.Parallel()
	for _, obstacle := range []string{"file parent", "symlink parent", "directory record", "symlink record", "symlink write lock"} {
		t.Run(obstacle, func(t *testing.T) {
			t.Parallel()
			repo, _, nextRoot, old, next := capabilityUpgradeFixture(t)
			switch obstacle {
			case "file parent":
				if err := os.WriteFile(filepath.Join(repo, ".code-polishy-reports"), []byte("occupied"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink parent":
				if err := os.Symlink(t.TempDir(), filepath.Join(repo, ".code-polishy-reports")); err != nil {
					t.Fatal(err)
				}
			default:
				path := filepath.Join(repo, filepath.FromSlash(capabilityUpgradePath(next)))
				if obstacle == "symlink write lock" {
					path = filepath.Join(repo, filepath.FromSlash(CapabilityUpgradeDirectory), ".write-lock")
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if obstacle == "directory record" {
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(filepath.Join(repo, LockFilename), path); err != nil {
					t.Fatal(err)
				}
			}
			if result, err := WriteReleaseLock(repo, nextRoot); err == nil || result.Changed {
				t.Fatalf("unpublishable upgrade succeeded: %+v, error = %v", result, err)
			}
			assertCapabilityUpgradeLock(t, repo, old)
		})
	}
}

func TestCapabilityUpgradeSerializesWritersWithoutAStaleDirectoryLock(t *testing.T) {
	t.Parallel()
	repo, _, nextRoot, old, next := capabilityUpgradeFixture(t)
	root, err := openCapabilityUpgradeRoot(repo, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	unlock, err := acquireCapabilityUpgradeLock(root)
	if err != nil {
		t.Fatal(err)
	}
	result, upgradeErr := WriteReleaseLock(repo, nextRoot)
	unlock()
	if upgradeErr == nil || result.Changed || !strings.Contains(upgradeErr.Error(), "busy") {
		t.Fatalf("concurrent writer acquired occupied lock: %+v, error = %v", result, upgradeErr)
	}
	assertCapabilityUpgradeLock(t, repo, old)
	if _, err := WriteReleaseLock(repo, nextRoot); err != nil {
		t.Fatalf("closed writer left a stale lock: %v", err)
	}
	assertCapabilityUpgradeLock(t, repo, next)
}

func capabilityUpgradeFixture(t *testing.T) (string, string, string, Lock, Lock) {
	t.Helper()
	catalog, err := ParseCapabilityCatalog([]byte(fixtureCapabilityCatalog))
	if err != nil {
		t.Fatal(err)
	}
	check := catalog.Capabilities[0]
	stable := check
	stable.Name, stable.Aliases, stable.Enforcement = "stable", []string{"stable beta", "stable alpha"}, []string{"explicit", "gate"}
	removed := check
	removed.Name, removed.Aliases = "legacy-command", []string{}
	catalog.Capabilities = []CapabilityDefinition{stable, removed, check}
	oldRoot, old := installedRelease(t, map[string]string{BinaryPath: "old engine", CapabilityCatalogPath: string(render(t, catalog))}, nil)
	old.CodePolishyVersion, old.ReleaseDigest = "9.9.8", ""
	old.ReleaseDigest = old.Identity()
	writeCapabilityManifest(t, oldRoot, old)
	check.Description = "Check source and dependencies."
	stable.Aliases, stable.Enforcement = []string{"stable alpha", "stable beta"}, []string{"gate", "explicit"}
	added := removed
	added.Name = "task-start"
	catalog.Capabilities = []CapabilityDefinition{check, added, stable}
	nextRoot, next := installedRelease(t, map[string]string{BinaryPath: "new engine", CapabilityCatalogPath: string(render(t, catalog))}, nil)
	prefix := t.TempDir()
	oldRoot = relocateCapabilityRelease(t, prefix, oldRoot, LockFor(old))
	nextRoot = relocateCapabilityRelease(t, prefix, nextRoot, LockFor(next))
	repo := t.TempDir()
	if err := WriteLock(repo, LockFor(old)); err != nil {
		t.Fatal(err)
	}
	return repo, oldRoot, nextRoot, LockFor(old), LockFor(next)
}

func relocateCapabilityRelease(t testing.TB, prefix, source string, locked Lock) string {
	t.Helper()
	destination := Directory(prefix, locked)
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(source, destination); err != nil {
		t.Fatal(err)
	}
	return destination
}

func assertCapabilityUpgradeLock(t testing.TB, repo string, expected Lock) {
	t.Helper()
	actual, present, err := ReadLock(repo)
	if err != nil || !present || !sameCapabilityLock(actual, expected) {
		t.Fatalf("lock = %+v, present = %v, error = %v", actual, present, err)
	}
}

func readCapabilityUpgradeArtifact(t testing.TB, repo string, delta CapabilityDelta) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(delta.ArtifactPath)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func removeCapabilityUpgradeInput(t testing.TB, root, relative string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
		t.Fatal(err)
	}
}
