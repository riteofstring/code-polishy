package behaviorreview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanupRemovesCompleteBehaviorReviewEvidenceAndIsIdempotent(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	root := filepath.Join(repo.Root, filepath.FromSlash(artifactDirectory))
	writeBehaviorFile(t, root, packetFilename, "sensitive packet\n")
	result, err := Cleanup(t.Context(), repo)
	if err != nil || !result.Removed || result.Path != artifactDirectory {
		t.Fatalf("Cleanup() = %+v, %v", result, err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("behavior review artifacts remain: %v", err)
	}
	result, err = Cleanup(t.Context(), repo)
	if err != nil || result.Removed {
		t.Fatalf("idempotent Cleanup() = %+v, %v", result, err)
	}
}

func TestCleanupRejectsEscapingArtifactLinks(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	root := filepath.Join(repo.Root, filepath.FromSlash(artifactDirectory))
	outside := t.TempDir()
	secret := writeBehaviorFile(t, outside, "intent.txt", "retain\n")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, root); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Cleanup(context.Background(), repo); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Cleanup() error = %v, want invalid input", err)
	}
	data, err := os.ReadFile(secret)
	if err != nil || string(data) != "retain\n" {
		t.Fatalf("cleanup followed escaping link: %v %q", err, data)
	}
}

func TestCaptureIntentAcceptsStandardInputAndRejectsInvalidSources(t *testing.T) {
	repo, _, _ := newBehaviorRepository(t)
	result, err := CaptureIntent(t.Context(), repo, CaptureIntentOptions{Intent: strings.NewReader("Exact standard input.\n")})
	if err != nil || result.IntentSHA256 == "" {
		t.Fatalf("stdin capture = %+v, %v", result, err)
	}
	path := writeBehaviorFile(t, t.TempDir(), "intent.txt", "file input\n")
	for name, options := range map[string]CaptureIntentOptions{
		"missing":   {},
		"ambiguous": {IntentPath: path, Intent: strings.NewReader("stdin\n")},
		"empty":     {Intent: strings.NewReader(" \n")},
		"invalid":   {Intent: strings.NewReader(string([]byte{0xff}))},
		"oversized": {Intent: strings.NewReader(strings.Repeat("x", maximumIntentBytes+1))},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := CaptureIntent(t.Context(), repo, options); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("CaptureIntent() error = %v, want invalid input", err)
			}
		})
	}
}
