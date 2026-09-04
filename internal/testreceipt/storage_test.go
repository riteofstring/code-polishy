package testreceipt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

func TestPassedReceiptRoundTripBindsExactIdentityAndArtifacts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identity := fixtureIdentity()
	created := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	artifact := testartifact.Record{Suite: "unit", Path: "unit/junit.xml", Type: "junit", Required: true, Size: 12, SHA256: strings.Repeat("d", 64)}
	recorded, err := recordPassedAt(root, identity, 1500*time.Millisecond, []testartifact.Record{artifact}, Source{Kind: "local"}, created)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := loadAt(root, identity, created.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !sameReceipt(recorded.Receipt, loaded.Receipt) || loaded.Path != recorded.Path || loaded.Receipt.DurationMillis != 1500 {
		t.Fatalf("loaded receipt = %+v, recorded = %+v", loaded, recorded)
	}
}

func TestReceiptReuseRejectsChangedExpiredAndCorruptEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	identity := fixtureIdentity()
	created := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	recorded, err := recordPassedAt(root, identity, time.Second, []testartifact.Record{}, Source{Kind: "local"}, created)
	if err != nil {
		t.Fatal(err)
	}
	changed := identity
	changed.Platform.OS = "windows"
	if _, err := loadAt(root, changed, created.Add(time.Hour)); !errors.Is(err, ErrMissing) {
		t.Fatalf("changed identity error = %v", err)
	}
	if _, err := loadAt(root, identity, created.Add(Retention+time.Second)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired receipt error = %v", err)
	}
	path := filepath.Join(root, filepath.FromSlash(recorded.Path))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 1
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAt(root, identity, created.Add(time.Hour)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("corrupt receipt error = %v", err)
	}
}

func TestReceiptStoreRejectsLinkedControlDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, rootDirectory)); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordPassed(root, fixtureIdentity(), time.Second, []testartifact.Record{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("linked store error = %v", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v, %v", entries, err)
	}
}

func fixtureIdentity() Identity {
	return Identity{
		Version: IdentityVersion, Release: Release{Version: "0.22.0", Digest: strings.Repeat("a", 64)}, PolicySchema: 3,
		ConfigurationSHA256: strings.Repeat("b", 64), Platform: Platform{OS: "linux", Arch: "amd64"},
		Suite: policy.TestSuite{
			Name: "unit", Kind: "unit", Scope: "module", Cost: "quick", Modules: []string{"domain"},
			Argv: []string{"go", "test", "./domain/..."}, Cwd: ".", Paths: []string{}, ExtraInputs: []string{},
			Artifacts: []policy.TestArtifact{}, RunOn: []string{"focused", "recommended", "full"}, Environment: []string{},
			ExclusiveResources: []string{}, TimeoutSeconds: 300,
		},
		Environment: []Environment{}, Tools: []Tool{{Name: "go", Version: "1.26.6"}},
		Inputs: []Input{{Path: "domain/model.go", Mode: 0o644, SHA256: strings.Repeat("c", 64)}},
	}
}
