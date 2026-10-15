package release

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRustBundleCancellationPreservesInstalledTools(t *testing.T) {
	root, manifest := rustBundleFixture(t)
	companion := filepath.Join(t.TempDir(), "rust.zip")
	if _, err := WriteRustArchive(root, companion); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if err := WriteLock(repo, indexedManifestLock(manifest)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := InstallRustBundle(ctx, repo, root, companion); err == nil {
		t.Fatal("canceled installation succeeded")
	}
	if err := VerifyRustTools(root); err != nil {
		t.Fatalf("cancellation damaged installed tools: %v", err)
	}
}

func TestRustBundlePublicationRollsBackEarlierTools(t *testing.T) {
	root, manifest := rustBundleFixture(t)
	stage, err := os.MkdirTemp(filepath.Dir(root), "rust-rollback-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(stage) })
	sdk := filepath.Join(".tools", "rust", manifest.Host)
	write(t, stage, filepath.Join("payload", sdk, "replacement"), "replacement SDK")
	if err := publishRustTools(root, stage, manifest); err == nil {
		t.Fatal("publication succeeded with a missing staged runner")
	}
	if err := VerifyRustTools(root); err != nil {
		t.Fatalf("partial publication damaged the original tools: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stage, "payload", sdk, "replacement")); err != nil {
		t.Fatalf("staged tools were lost during recovery: %v", err)
	}
}

func TestRustBundleNormalizesArchivePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX execution and shared-write permission bits")
	}
	root, manifest := rustBundleFixture(t)
	paths := map[string]os.FileMode{
		rustNextestPath(manifest.Host):                           0o755,
		".tools/rust/" + manifest.Host + "/lib/compiler-library": 0o644,
	}
	for path := range paths {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(path)), 0o777); err != nil {
			t.Fatal(err)
		}
	}
	archive, repo := filepath.Join(t.TempDir(), "rust.zip"), t.TempDir()
	if _, err := WriteRustArchive(root, archive); err != nil {
		t.Fatal(err)
	}
	if err := WriteLock(repo, indexedManifestLock(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := InstallRustBundle(t.Context(), repo, root, archive); err != nil {
		t.Fatal(err)
	}
	for path, expected := range paths {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil || info.Mode().Perm() != expected {
			t.Fatalf("unsafe permissions on %s: %v %v", path, info, err)
		}
	}
}

func TestOptionalRustInstallationPreservesRequiredToolIntegrity(t *testing.T) {
	root, manifest := rustBundleFixture(t)
	core, companion := filepath.Join(t.TempDir(), "core.zip"), filepath.Join(t.TempDir(), "rust.zip")
	if _, err := WriteArchive(root, core); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteRustArchive(root, companion); err != nil {
		t.Fatal(err)
	}
	installed, repo := t.TempDir(), t.TempDir()
	if err := extractReleaseZip(core, installed); err != nil {
		t.Fatal(err)
	}
	if err := WriteLock(repo, indexedManifestLock(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Verify(installed); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRustTools(installed); err == nil {
		t.Fatal("a core-only installation supplied required Rust coverage")
	}
	if _, err := os.Stat(filepath.Join(installed, ".tools", "rust")); !os.IsNotExist(err) {
		t.Fatalf("default archive carried the Rust SDK: %v", err)
	}
	if err := InstallRustBundle(t.Context(), repo, installed, companion); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRustTools(installed); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(installed, filepath.FromSlash(rustNextestPath(manifest.Host)))
	if err := os.WriteFile(tool, []byte("replacement"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRustTools(installed); err == nil {
		t.Fatal("a modified native test runner was accepted")
	}
	if err := InstallRustBundle(t.Context(), repo, installed, companion); err != nil {
		t.Fatalf("repair of modified optional tools failed: %v", err)
	}
	if err := VerifyRustTools(installed); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tool); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRustTools(installed); err == nil {
		t.Fatal("removing the optional test runner bypassed required integrity")
	}
}

func TestRustBundleCannotSupplyItsOwnAuthorityOrCoreFiles(t *testing.T) {
	root, manifest := rustBundleFixture(t)
	repo := t.TempDir()
	if err := WriteLock(repo, indexedManifestLock(manifest)); err != nil {
		t.Fatal(err)
	}
	foreign, _ := rustBundleFixture(t)
	tool := filepath.Join(foreign, filepath.FromSlash(rustNextestPath(manifest.Host)))
	if err := os.WriteFile(tool, []byte("foreign release"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(foreign, ManifestFilename)); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteManifest(foreign, strings.Repeat("a", 40)); err != nil {
		t.Fatal(err)
	}
	companion := filepath.Join(t.TempDir(), "foreign.zip")
	if _, err := WriteRustArchive(foreign, companion); err != nil {
		t.Fatal(err)
	}
	if err := InstallRustBundle(t.Context(), repo, root, companion); err == nil {
		t.Fatal("foreign archive replaced the selected release's tool authority")
	}
	if err := VerifyRustTools(root); err != nil {
		t.Fatalf("failed installation damaged working tools: %v", err)
	}
	core := filepath.Join(t.TempDir(), "core.zip")
	if _, err := WriteArchive(root, core); err != nil {
		t.Fatal(err)
	}
	if err := InstallRustBundle(t.Context(), repo, root, core); err == nil {
		t.Fatal("Rust setup admitted core files")
	}
}

func TestRustIntegrityRejectsAdditionalFilesAndLinkedDirectories(t *testing.T) {
	root, manifest := rustBundleFixture(t)
	directory := filepath.Join(root, ".tools", "rust", manifest.Host)
	extra := filepath.Join(directory, "lib", "injected-library")
	if err := os.WriteFile(extra, []byte("injected"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRustTools(root); err == nil {
		t.Fatal("unrecorded native library was accepted")
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "sdk")
	if err := os.Rename(directory, other); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, directory); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := VerifyRustTools(root); err == nil {
		t.Fatal("linked SDK directory escaped the installed boundary")
	}
}

func rustBundleFixture(t *testing.T) (string, Manifest) {
	t.Helper()
	root := writableReleaseStage(t)
	host, err := Host()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher", rustNextestPath(host): "nextest"}
	for _, name := range []string{"rustc", "rustdoc", "cargo", "rustfmt", "cargo-fmt", "clippy-driver", "cargo-clippy"} {
		suffix := ""
		if host == "windows-x64" {
			suffix = ".exe"
		}
		files[".tools/rust/"+host+"/bin/"+name+suffix] = name
	}
	files[".tools/rust/"+host+"/lib/compiler-library"] = "compiler library"
	for path, value := range files {
		write(t, root, path, value)
	}
	manifest, err := WriteManifest(root, exampleRevision)
	if err != nil {
		t.Fatal(err)
	}
	return root, manifest
}
