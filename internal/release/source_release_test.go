package release

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallSourceReleaseBindsACleanCheckoutToALegacyLock(t *testing.T) {
	source, revision := sourceCheckoutFixture(t)
	releaseRoot, manifest := installedRelease(t, map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher"}, nil)
	manifest.SourceRevision = revision
	manifest.ReleaseDigest = manifest.Identity()
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseRoot, ManifestFilename), append(manifestData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	prefix := filepath.Join(t.TempDir(), "prefix")
	result, err := installSourceRelease(context.Background(), source, prefix, func(_ context.Context, _ string, gotPrefix string) error {
		target := Directory(gotPrefix, LockFor(manifest))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.Rename(releaseRoot, target)
	})
	if err != nil {
		t.Fatal(err)
	}
	canonicalPrefix, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		t.Fatal(err)
	}
	if result.Lock.LockVersion != LegacyLockVersion || result.Lock.Publication != nil || result.Manifest.SourceRevision != revision || result.Root != Directory(canonicalPrefix, result.Lock) {
		t.Fatalf("source release = %+v", result)
	}
}

func TestInstallSourceReleaseRejectsADirtyCheckoutBeforeInstallation(t *testing.T) {
	source, _ := sourceCheckoutFixture(t)
	if err := os.WriteFile(filepath.Join(source, "VERSION"), []byte("9.9.10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := installSourceRelease(context.Background(), source, t.TempDir(), func(context.Context, string, string) error {
		called = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "must be clean") || called {
		t.Fatalf("dirty source error=%v installer-called=%v", err, called)
	}
}

func sourceCheckoutFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "VERSION"), []byte("9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{
		{"init", "--quiet"},
		{"config", "user.name", "Code Polishy Test"},
		{"config", "user.email", "test@example.invalid"},
		{"add", "VERSION"},
		{"commit", "--quiet", "-m", "fixture"},
	} {
		command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, output)
		}
	}
	command := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return root, strings.TrimSpace(string(output))
}
