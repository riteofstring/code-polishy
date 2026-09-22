package pack

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestToolIdentityRequiresRecordedContainedUnchangedBytes(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "runtime/node", "recorded runtime\n", 0o755)
	requested := policy.PackTool{ID: "node", Name: "node", Version: "24.18.0", Launcher: true}
	manifest := release.Manifest{Entries: []release.Entry{{Path: "runtime/node", SHA256: inputDigest([]byte("recorded runtime\n"))}}}
	executable := filepath.Join(root, "runtime/node")
	identity, err := verifyToolFile(root, manifest, executable, requested)
	if err != nil || identity.SHA256 != manifest.Entries[0].SHA256 {
		t.Fatalf("recorded runtime was rejected: %+v %v", identity, err)
	}
	if _, err := verifyToolFile(root, release.Manifest{}, executable, requested); err == nil {
		t.Fatal("unrecorded runtime was accepted")
	}
	writeTestFile(t, root, "runtime/node", "changed runtime\n", 0o755)
	if _, err := verifyToolFile(root, manifest, executable, requested); err == nil {
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
	if _, err := verifyToolFile(root, manifest, outside, requested); err == nil {
		t.Fatal("escaping runtime was accepted")
	}
}

func TestToolIdentityFollowsOnlyAnExactRecordedSymlinkChain(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "runtime/bin/python3.12", "recorded runtime\n", 0o755)
	if err := os.Symlink("bin/python", filepath.Join(root, "runtime", "python")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("python3.12", filepath.Join(root, "runtime", "bin", "python")); err != nil {
		t.Fatal(err)
	}
	manifest := release.Manifest{Entries: []release.Entry{
		{Path: "runtime/python", Symlink: "bin/python"},
		{Path: "runtime/bin/python", Symlink: "python3.12"},
		{Path: "runtime/bin/python3.12", SHA256: inputDigest([]byte("recorded runtime\n"))},
	}}
	requested := policy.PackTool{ID: "python", Name: "python", Version: "3.12.13+20260728"}
	identity, err := verifyToolFile(root, manifest, filepath.Join(root, "runtime", "python"), requested)
	if err != nil || identity.SHA256 != manifest.Entries[2].SHA256 {
		t.Fatalf("recorded symlink tool was rejected: %+v %v", identity, err)
	}
	if err := os.Remove(filepath.Join(root, "runtime", "python")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("bin/python3.12", filepath.Join(root, "runtime", "python")); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyToolFile(root, manifest, filepath.Join(root, "runtime", "python"), requested); err == nil {
		t.Fatal("changed tool symlink was accepted")
	}
	outside := filepath.Join(t.TempDir(), "python")
	writeTestFile(t, filepath.Dir(outside), filepath.Base(outside), "recorded runtime\n", 0o755)
	if err := os.Remove(filepath.Join(root, "runtime", "python")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "runtime", "python")); err != nil {
		t.Fatal(err)
	}
	manifest.Entries[0].Symlink = outside
	if _, err := verifyToolFile(root, manifest, filepath.Join(root, "runtime", "python"), requested); err == nil {
		t.Fatal("escaping tool symlink was accepted")
	}
}

func TestToolchainBindsEveryExactToolAndOnlyOneLauncher(t *testing.T) {
	root := t.TempDir()
	packRoot := filepath.Join(root, "pack")
	writeTestFile(t, root, "tools/python", "python\n", 0o755)
	writeTestFile(t, root, "tools/shellcheck", "shellcheck\n", 0o755)
	writeTestFile(t, packRoot, "adapter.py", "adapter\n", 0o644)
	manifest := release.Manifest{Entries: []release.Entry{
		{Path: "tools/python", SHA256: inputDigest([]byte("python\n"))},
		{Path: "tools/shellcheck", SHA256: inputDigest([]byte("shellcheck\n"))},
	}}
	available := []repository.GovernedTool{
		{Name: "python", Path: filepath.Join(root, "tools", "python"), Version: "3.13.11"},
		{Name: "shellcheck", Path: filepath.Join(root, "tools", "shellcheck"), Version: "0.11.0"},
	}
	requested := []policy.PackTool{
		{ID: "python", Name: "python", Version: "3.13.11", Launcher: true},
		{ID: "shellcheck", Name: "shellcheck", Version: "0.11.0"},
	}
	command := policy.Command{Argv: []string{"adapter.py", "--check"}, Adapter: &policy.PackAdapter{PackRoot: packRoot}}
	bound, identities, err := bindToolchain(root, manifest, available, command, requested)
	if err != nil {
		t.Fatal(err)
	}
	wantArgv := []string{available[0].Path, filepath.Join(packRoot, "adapter.py"), "--check"}
	if !slices.Equal(bound.Argv, wantArgv) {
		t.Fatalf("argv = %v, want %v", bound.Argv, wantArgv)
	}
	wantEnvironment := []string{"CODE_POLISHY_TOOL_PYTHON=" + available[0].Path, "CODE_POLISHY_TOOL_SHELLCHECK=" + available[1].Path}
	if !slices.Equal(bound.EnvironmentOverrides, wantEnvironment) || len(identities) != 2 || identities[0].ID != "python" || identities[1].ID != "shellcheck" {
		t.Fatalf("toolchain was not bound exactly: environment=%v identities=%+v", bound.EnvironmentOverrides, identities)
	}
	requested[1].Launcher = true
	if _, _, err := toolchainCommand(repository.Repository{}, command, requested); err == nil || !strings.Contains(err.Error(), "at most one launcher") {
		t.Fatalf("multiple launchers passed: %v", err)
	}
}

func TestToolchainSelectionCannotUseAnAmbientToolWithoutAnInstalledRelease(t *testing.T) {
	root := t.TempDir()
	repo := repository.Repository{Root: root, PolicyRoot: root}
	command := policy.Command{Argv: []string{"adapter.mjs"}, Adapter: &policy.PackAdapter{PackRoot: root}}
	_, _, err := toolchainCommand(repo, command, []policy.PackTool{{ID: "node", Name: "node", Version: "24.18.0", Launcher: true}})
	if err == nil || !strings.Contains(err.Error(), "verified installed") {
		t.Fatalf("ambient fallback was not rejected: %v", err)
	}
}
