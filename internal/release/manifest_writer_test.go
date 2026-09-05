package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	return root
}

func TestShellManifestBindsTheNativeCatalogIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Unix installer uses the shell writer")
	}
	root := writableReleaseStage(t)
	native, err := WriteManifest(root, exampleRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ManifestFilename)); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../scripts/release-manifest.sh")
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.CommandContext(t.Context(), "bash", script, "write", root, exampleRevision).CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != native.ReleaseDigest {
		t.Fatalf("shell release identity differs from native: output=%s err=%v", output, err)
	}
	locked := Lock{LockVersion: LockVersion, CodePolishyVersion: native.CodePolishyVersion, ReleaseDigest: native.ReleaseDigest, Features: native.Features}
	catalog, err := ReadCapabilityCatalog(root, locked)
	if err != nil || catalog.SHA256 != native.CapabilityCatalogSHA256 {
		t.Fatalf("shell release catalog: %+v err=%v", catalog, err)
	}
	if output, err := exec.CommandContext(t.Context(), "bash", script, "verify", root).CombinedOutput(); err != nil {
		t.Fatalf("verify shell release: %s: %v", output, err)
	}
	if err := os.WriteFile(filepath.Join(root, CapabilityCatalogPath), []byte(strings.ReplaceAll(fixtureCapabilityCatalog, "Check selected source.", "Changed release capability.")), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.CommandContext(t.Context(), "bash", script, "verify", root).CombinedOutput(); err == nil {
		t.Fatalf("verified changed catalog: %s", output)
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
