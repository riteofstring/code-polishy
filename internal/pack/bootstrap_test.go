package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestRuntimeIdentityRequiresRecordedContainedUnchangedBytes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "runtime/node", "recorded runtime\n", 0o755)
	requested := &policy.PackRuntime{Name: "node", Version: "24.18.0"}
	manifest := release.Manifest{Entries: []release.Entry{{Path: "runtime/node", SHA256: inputDigest([]byte("recorded runtime\n"))}}}
	executable := filepath.Join(root, "runtime/node")
	identity, err := verifyRuntimeFile(root, manifest, executable, requested)
	if err != nil || identity.SHA256 != manifest.Entries[0].SHA256 {
		t.Fatalf("recorded runtime was rejected: %+v %v", identity, err)
	}
	if _, err := verifyRuntimeFile(root, release.Manifest{}, executable, requested); err == nil {
		t.Fatal("unrecorded runtime was accepted")
	}
	writeTestFile(t, root, "runtime/node", "changed runtime\n", 0o755)
	if _, err := verifyRuntimeFile(root, manifest, executable, requested); err == nil {
		t.Fatal("changed runtime was accepted")
	}
	outside := filepath.Join(t.TempDir(), "node")
	if err := os.WriteFile(outside, []byte("recorded runtime\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, outside)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Entries[0].Path = filepath.ToSlash(relative)
	if _, err := verifyRuntimeFile(root, manifest, outside, requested); err == nil {
		t.Fatal("escaping runtime was accepted")
	}
}

func TestRuntimeSelectionCannotUseAnAmbientToolWithoutAnInstalledRelease(t *testing.T) {
	root := t.TempDir()
	repo := repository.Repository{Root: root, PolicyRoot: root}
	command := policy.Command{Argv: []string{"adapter.mjs"}, Adapter: &policy.PackAdapter{PackRoot: root}}
	_, _, err := runtimeCommand(repo, command, &policy.PackRuntime{Name: "node", Version: "24.18.0"})
	if err == nil || !strings.Contains(err.Error(), "verified installed") {
		t.Fatalf("ambient fallback was not rejected: %v", err)
	}
}
