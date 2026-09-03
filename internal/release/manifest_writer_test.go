package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteManifestPublishesAVerifiableImmutableRelease(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"VERSION": "9.9.9\n", "scripts/go_version.txt": "1.26.6\n",
		"tools/govulncheck-version.txt": "v1.3.0\n", "tools/node-version.txt": "24.18.0\n",
		"tools/osv-scanner-version.txt": "v2.4.0\n", "tools/pnpm-version.txt": "11.13.0\n",
		"tools/python-version.txt": "3.12.13+20260728\n",
		"tools/ruff-version.txt":   "0.16.0\n", "tools/shellcheck-version.txt": "0.11.0\n",
		"tools/staticcheck-version.txt": "v0.7.0\n", "tools/ty-version.txt": "0.0.65\n", "tools/vulture-version.txt": "2.16\n",
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
	manifest, err := WriteManifest(root, exampleRevision)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Tools.Python != "3.12.13+20260728" || manifest.Tools.Vulture != "2.16" {
		t.Fatalf("tools = %+v", manifest.Tools)
	}
	read, present, err := ReadManifest(root)
	if err != nil || !present {
		t.Fatalf("read present=%v err=%v", present, err)
	}
	if err := read.Verify(root); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteManifest(root, exampleRevision); err == nil {
		t.Fatal("overwrote an existing manifest")
	}
}

func TestManifestRejectsContentDigestDetachedFromEntries(t *testing.T) {
	_, manifest := exampleRelease(t)
	manifest.ContentDigest = otherDigest
	if _, err := parseManifest(render(t, manifest), "fixture"); err == nil {
		t.Fatal("accepted a detached content digest")
	}
}
