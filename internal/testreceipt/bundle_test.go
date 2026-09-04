package testreceipt

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBundleRoundTripAuthenticatesAndImportsReceiptsAtomically(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	sourceRoot := t.TempDir()
	identity := fixtureIdentity()
	if _, err := recordPassedAt(sourceRoot, identity, 2*time.Second, nil, Source{Kind: "local"}, created); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "receipts.json")
	bundleSHA256, count, err := exportBundleAt(sourceRoot, bundlePath, created.Add(time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("export count = %d, digest = %q, error = %v", count, bundleSHA256, err)
	}
	targetRoot := t.TempDir()
	if imported, importErr := importBundleAt(targetRoot, bundlePath, bundleSHA256, created.Add(2*time.Hour)); importErr != nil || imported != 1 {
		t.Fatalf("import count = %d, error = %v", imported, importErr)
	}
	loaded, err := loadAt(targetRoot, identity, created.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Path != receiptDisplayBundlePath() || loaded.Receipt.Source.Kind != "ci" ||
		loaded.Receipt.Source.BundleSHA256 != bundleSHA256 || loaded.Receipt.DurationMillis != 2000 {
		t.Fatalf("loaded receipt = %+v", loaded)
	}
}

func TestBundleImportRejectsWrongDigestWithoutReplacingTrustedBundle(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	sourceRoot := t.TempDir()
	identity := fixtureIdentity()
	if _, err := recordPassedAt(sourceRoot, identity, time.Second, nil, Source{Kind: "local"}, created); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "receipts.json")
	bundleSHA256, _, err := exportBundleAt(sourceRoot, bundlePath, created.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := t.TempDir()
	if _, err := importBundleAt(targetRoot, bundlePath, bundleSHA256, created.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(targetRoot, filepath.FromSlash(receiptDisplayBundlePath()))
	before, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importBundleAt(targetRoot, bundlePath, fixtureIdentity().ConfigurationSHA256, created.Add(3*time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong digest error = %v", err)
	}
	after, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected import changed the trusted bundle")
	}
}

func TestBundleLoadRejectsTamperingAndExpiredEvidence(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	sourceRoot := t.TempDir()
	identity := fixtureIdentity()
	if _, err := recordPassedAt(sourceRoot, identity, time.Second, nil, Source{Kind: "local"}, created); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(t.TempDir(), "receipts.json")
	bundleSHA256, _, err := exportBundleAt(sourceRoot, bundlePath, created.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	targetRoot := t.TempDir()
	if _, err := importBundleAt(targetRoot, bundlePath, bundleSHA256, created.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAt(targetRoot, identity, created.Add(Retention+time.Second)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired bundle error = %v", err)
	}
	stored := filepath.Join(targetRoot, filepath.FromSlash(receiptDisplayBundlePath()))
	data, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 1
	if err := os.WriteFile(stored, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAt(targetRoot, identity, created.Add(3*time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered bundle error = %v", err)
	}
}
