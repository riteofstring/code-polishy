package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteManifestPublishesAVerifiableImmutableRelease(t *testing.T) {
	root := writableReleaseStage(t)
	manifest, err := WriteManifest(root, exampleRevision)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Tools.Packaging != "26.3" || manifest.Tools.Python != "3.12.13+20260728" || manifest.Tools.Vulture != "2.16" {
		t.Fatalf("tools = %+v", manifest.Tools)
	}
	if manifest.Tools.Rust != "1.97.1" || manifest.Tools.Nextest != "0.9.138" {
		t.Fatalf("Rust tools = %+v", manifest.Tools)
	}
	read, present, err := ReadManifest(root)
	if err != nil || !present {
		t.Fatalf("read present=%v err=%v", present, err)
	}
	if err := read.Verify(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, ManifestFilename))
	if err != nil || info.Mode().Perm()&0o044 != 0o044 {
		t.Fatalf("an installed release run as another user cannot read its manifest: info=%v err=%v", info, err)
	}
	if _, err := WriteManifest(root, exampleRevision); err == nil {
		t.Fatal("overwrote an existing manifest")
	}
}

func writableReleaseStage(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		CapabilityCatalogPath:     fixtureCapabilityCatalog,
		"docs/agent-workflows.md": "# Agent workflows\n",
		"VERSION":                 "9.9.9\n", "scripts/go_version.txt": "1.26.6\n",
		"tools/govulncheck-version.txt": "v1.3.0\n", "tools/node-version.txt": "24.18.0\n",
		"tools/osv-scanner-version.txt": "v2.4.0\n", "tools/pnpm-version.txt": "11.13.0\n",
		"tools/packaging-version.txt": "26.3\n",
		"tools/python-version.txt":    "3.12.13+20260728\n",
		"tools/ruff-version.txt":      "0.16.0\n", "tools/shellcheck-version.txt": "0.11.0\n",
		"tools/staticcheck-version.txt": "v0.7.0\n", "tools/deadcode-version.txt": "v0.50.0\n", "tools/ty-version.txt": "0.0.65\n", "tools/vulture-version.txt": "2.16\n",
		"tools/rust-version.txt": "1.97.1\n", "tools/cargo-nextest-version.txt": "0.9.138\n", "tools/trivy-version.txt": "0.74.0\n",
		BinaryPath: "engine", LauncherBinaryPath: "launcher",
	}
	for relative, content := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestManifestBindsTheCatalogIdentity(t *testing.T) {
	root := writableReleaseStage(t)
	native, err := WriteManifest(root, exampleRevision)
	if err != nil {
		t.Fatal(err)
	}
	locked := indexedManifestLock(native)
	catalog, err := ReadCapabilityCatalog(root, locked)
	if err != nil || catalog.SHA256 != native.CapabilityCatalogSHA256 {
		t.Fatalf("release catalog: %+v err=%v", catalog, err)
	}
	if err := native.Verify(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, CapabilityCatalogPath), []byte(strings.ReplaceAll(fixtureCapabilityCatalog, "Check selected source.", "Changed release capability.")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := native.Verify(root); err == nil {
		t.Fatal("verified changed catalog")
	}
	if _, err := ReadCapabilityCatalog(root, locked); err == nil {
		t.Fatal("read changed catalog using unchanged release identity")
	}
}

func TestManifestRejectsContentDigestDetachedFromEntries(t *testing.T) {
	_, manifest := exampleRelease(t)
	manifest.ContentDigest = otherDigest
	if _, err := parseManifest(render(t, manifest), "fixture"); err == nil {
		t.Fatal("accepted a detached content digest")
	}
}
