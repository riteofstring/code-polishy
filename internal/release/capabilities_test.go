package release

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapabilityCatalogBindsExactReleaseAndContent(t *testing.T) {
	t.Parallel()
	firstRoot, first := installedRelease(t, map[string]string{BinaryPath: "engine"}, nil)
	changed := strings.Replace(fixtureCapabilityCatalog, "Check selected source.", "Check selected source and dependencies.", 1)
	secondRoot, second := installedRelease(t, map[string]string{BinaryPath: "engine", CapabilityCatalogPath: changed}, nil)
	if first.ReleaseDigest == second.ReleaseDigest {
		t.Fatal("changed capability metadata retained the same release identity")
	}
	for _, selected := range []struct {
		root        string
		manifest    Manifest
		description string
	}{
		{firstRoot, first, "Check selected source."},
		{secondRoot, second, "Check selected source and dependencies."},
	} {
		catalog, err := ReadCapabilityCatalog(selected.root, LockFor(selected.manifest))
		if err != nil || catalog.SHA256 != selected.manifest.CapabilityCatalogSHA256 || catalog.Release.ReleaseDigest != selected.manifest.ReleaseDigest {
			t.Fatalf("catalog = %+v, error = %v", catalog, err)
		}
		if len(catalog.Catalog.Capabilities) != 1 || catalog.Catalog.Capabilities[0].Description != selected.description {
			t.Fatalf("wrong release catalog: %+v", catalog.Catalog)
		}
	}
	if _, err := ReadCapabilityCatalog(firstRoot, LockFor(second)); err == nil {
		t.Fatal("accepted a catalog from another exact release")
	}
	if _, err := ReadCapabilityCatalog(t.TempDir(), LockFor(first)); err == nil {
		t.Fatal("missing selected release fell back to an ambient catalog")
	}
}

func TestCapabilityCatalogRejectsRehashedContentUnderAnUnchangedLock(t *testing.T) {
	t.Parallel()
	root, manifest := installedRelease(t, map[string]string{BinaryPath: "engine"}, nil)
	locked := LockFor(manifest)
	changed := []byte(strings.Replace(fixtureCapabilityCatalog, "Check selected source.", "Execute unapproved commands.", 1))
	if err := os.WriteFile(filepath.Join(root, CapabilityCatalogPath), changed, 0o644); err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Entries {
		if manifest.Entries[index].Path == CapabilityCatalogPath {
			manifest.Entries[index].SHA256 = capabilityContentSHA256(changed)
		}
	}
	manifest.ContentDigest = EntriesDigest(manifest.Entries)
	writeCapabilityManifest(t, root, manifest)
	if _, err := ReadCapabilityCatalog(root, locked); err == nil {
		t.Fatal("accepted a changed catalog after only the entry hashes were recomputed")
	}
	manifest.CapabilityCatalogSHA256 = capabilityContentSHA256(changed)
	manifest.ReleaseDigest = manifest.Identity()
	writeCapabilityManifest(t, root, manifest)
	if _, err := ReadCapabilityCatalog(root, locked); err == nil {
		t.Fatal("accepted a changed catalog after its release identity diverged from the lock")
	}
}

func TestCapabilityCatalogRejectsUnsafeAndUnavailableInputs(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing catalog", "symlink catalog", "oversized catalog", "missing workflow", "symlink parent", "changed catalog"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, manifest := installedRelease(t, map[string]string{BinaryPath: "engine"}, nil)
			catalogPath := filepath.Join(root, CapabilityCatalogPath)
			switch name {
			case "missing catalog":
				if err := os.Remove(catalogPath); err != nil {
					t.Fatal(err)
				}
			case "symlink catalog":
				target := filepath.Join(t.TempDir(), "catalog.json")
				if err := os.Rename(catalogPath, target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, catalogPath); err != nil {
					t.Fatal(err)
				}
			case "oversized catalog":
				if err := os.Truncate(catalogPath, MaximumCapabilityCatalogBytes+1); err != nil {
					t.Fatal(err)
				}
			case "missing workflow":
				if err := os.Remove(filepath.Join(root, "docs/agent-workflows.md")); err != nil {
					t.Fatal(err)
				}
			case "symlink parent":
				docs := filepath.Join(root, "docs")
				moved := filepath.Join(root, "original-docs")
				if err := os.Rename(docs, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, docs); err != nil {
					t.Fatal(err)
				}
			case "changed catalog":
				if err := os.WriteFile(catalogPath, []byte(strings.Replace(fixtureCapabilityCatalog, "Check", "Alter", 1)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ReadCapabilityCatalog(root, LockFor(manifest)); err == nil {
				t.Fatal("accepted unsafe or unavailable catalog input")
			}
		})
	}
}

func TestCapabilityCatalogRequiresBoundedUnambiguousMetadata(t *testing.T) {
	t.Parallel()
	cases := []string{
		strings.Replace(fixtureCapabilityCatalog, "release-capabilities/v1", "release-capabilities/v2", 1),
		strings.Replace(fixtureCapabilityCatalog, `"kind":"command"`, `"kind":"guessed"`, 1),
		strings.Replace(fixtureCapabilityCatalog, `"aliases":["source validation"]`, `"aliases":["ＣＨＥＣＫ"]`, 1),
		strings.Replace(fixtureCapabilityCatalog, `"aliases":["source validation"]`, `"aliases":null`, 1),
		strings.Replace(fixtureCapabilityCatalog, `"description":"Check selected source."`, `"description":" "`, 1),
		strings.Replace(fixtureCapabilityCatalog, `"paths":[]`, `"unknown":true,"paths":[]`, 1),
		strings.Replace(fixtureCapabilityCatalog, "docs/agent-workflows.md", "docs/../outside.md", 1),
		fixtureCapabilityCatalog + `{}`,
		strings.Repeat(" ", MaximumCapabilityCatalogBytes+1),
	}
	for _, candidate := range cases {
		if _, err := ParseCapabilityCatalog([]byte(candidate)); err == nil {
			t.Fatalf("accepted invalid catalog %s", candidate[:min(len(candidate), 512)])
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "..", CapabilityCatalogPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCapabilityCatalog(data); err != nil {
		t.Fatalf("shipped capability catalog: %v", err)
	}
}

func writeCapabilityManifest(t testing.TB, root string, manifest Manifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ManifestFilename), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
