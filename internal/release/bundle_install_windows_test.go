package release

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseHostMatrixAcceptsAbsoluteWindowsBundlePath(t *testing.T) {
	releaseRoot, manifest := installedRelease(t, map[string]string{
		BinaryPath: "engine", LauncherBinaryPath: "launcher",
	}, nil)
	archive := zipDirectory(t, releaseRoot)
	if !filepath.IsAbs(archive) {
		t.Fatalf("test archive is not absolute: %s", archive)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	prefix := filepath.Join(t.TempDir(), "installed")
	installed, err := InstallLocalBundle(archive, hex.EncodeToString(digest[:]), prefix)
	if err != nil {
		t.Fatal(err)
	}
	if installed.ReleaseDigest != manifest.ReleaseDigest {
		t.Fatalf("release=%s want=%s", installed.ReleaseDigest, manifest.ReleaseDigest)
	}
}
