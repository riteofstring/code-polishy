package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestContentDigestBindsExactBytesAndRejectsUnboundedOrLinkedFiles(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir()}
	original := []byte{0, 1, 0xff, '\n'}
	path := filepath.Join(repo.Root, "output.bin")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(original)
	actual, err := repo.ContentDigest("output.bin")
	if err != nil || actual != hex.EncodeToString(expected[:]) {
		t.Fatalf("exact content identity = %q, %v", actual, err)
	}
	if err := os.Truncate(path, MaximumContentDigestBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ContentDigest("output.bin"); err == nil {
		t.Fatal("oversized identity was accepted")
	}
	if err := os.Symlink(path, filepath.Join(repo.Root, "linked.bin")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	for _, input := range []string{"linked.bin", "missing.bin", "../outside.bin"} {
		if _, err := repo.ContentDigest(input); err == nil {
			t.Fatalf("invalid digest input accepted: %s", input)
		}
	}
}
