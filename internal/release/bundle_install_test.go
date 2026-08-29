package release

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallLocalBundlePublishesVerifiedReleaseAndLauncher(t *testing.T) {
	releaseRoot, manifest := installedRelease(t, map[string]string{BinaryPath: "engine", LauncherBinaryPath: "launcher"}, nil)
	archive := zipDirectory(t, releaseRoot)
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
	target := filepath.Join(prefix, "releases", manifest.CodePolishyVersion+"-"+manifest.ReleaseDigest)
	if err := manifest.Verify(target); err != nil {
		t.Fatal(err)
	}
	launcher, err := os.ReadFile(filepath.Join(prefix, "bin", filepath.Base(filepath.FromSlash(BinaryPath))))
	if err != nil {
		t.Fatal(err)
	}
	if string(launcher) != "launcher" {
		t.Fatalf("launcher=%q", launcher)
	}
}

func TestInstallLocalBundleRejectsChecksumAndTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.zip")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create("../escape")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(entry, "bad"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if _, err := InstallLocalBundle(archive, hex.EncodeToString(digest[:]), t.TempDir()); err == nil {
		t.Fatal("traversal bundle was accepted")
	}
	if _, err := InstallLocalBundle(archive, string(make([]byte, 64)), t.TempDir()); err == nil {
		t.Fatal("invalid checksum was accepted")
	}
}

func TestInstallLocalBundleRejectsRemoteAndRelativeSources(t *testing.T) {
	validDigest := hex.EncodeToString(make([]byte, sha256.Size))
	for _, source := range []string{"release.zip", "https://example.invalid/release.zip"} {
		if _, err := InstallLocalBundle(source, validDigest, t.TempDir()); err == nil {
			t.Fatalf("non-local release bundle source %q was accepted", source)
		}
	}
}

func zipDirectory(t *testing.T, root string) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "release.zip")
	output, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(output)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		zipped, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(zipped, input)
		closeErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}
