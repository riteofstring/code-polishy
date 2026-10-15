package testing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestNativeRustRejectsExternalWorkspaceBeforeCompilation(t *testing.T) {
	t.Parallel()
	for _, doctests := range []bool{false, true} {
		repo, suite := rustSuiteRepository(t, "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n")
		parent := t.TempDir()
		root := filepath.Join(parent, "member")
		if err := os.Rename(repo.Root, root); err != nil {
			t.Fatal(err)
		}
		repo.Root = root
		suite.Rust.Doctests = doctests
		writeRustSuiteFile(t, root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n")
		writeRustSuiteFile(t, parent, "Cargo.toml", "[workspace]\nmembers=['member']\nresolver='3'\n")
		writeRustSuiteFile(t, parent, "Cargo.lock", "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n")
		writeRustSuiteFile(t, root, "build.rs", "fn main() { std::fs::write(\"build-started\", b\"ran\").unwrap(); }\n")
		for attempt := 1; attempt <= 2; attempt++ {
			execution := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, attempt)
			if !execution.Failed() || !strings.Contains(execution.FailureMessage, "workspace root") {
				t.Fatalf("external workspace accepted for doctests=%v attempt=%d: %+v", doctests, attempt, execution)
			}
			if _, err := os.Stat(filepath.Join(root, "build-started")); !os.IsNotExist(err) {
				t.Fatalf("build script executed before containment validation: %v", err)
			}
		}
	}
}

func TestNativeRustRejectsLinkedWorkspaceLockBeforeCompilation(t *testing.T) {
	t.Parallel()
	for _, doctests := range []bool{false, true} {
		repo, suite := rustSuiteRepository(t, "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n")
		suite.Rust.Doctests = doctests
		lock := filepath.Join(repo.Root, "Cargo.lock")
		outside := filepath.Join(t.TempDir(), "Cargo.lock")
		if err := os.Rename(lock, outside); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, lock); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		writeRustSuiteFile(t, repo.Root, "build.rs", "fn main() { std::fs::write(\"build-started\", b\"ran\").unwrap(); }\n")
		execution := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 2)
		if !execution.Failed() || !strings.Contains(execution.FailureMessage, "contained Cargo.lock") {
			t.Fatalf("linked lock accepted for doctests=%v: %+v", doctests, execution)
		}
		if _, err := os.Stat(filepath.Join(repo.Root, "build-started")); !os.IsNotExist(err) {
			t.Fatalf("build script executed with a linked lock: %v", err)
		}
	}
}

func TestNativeRustUsesActualExplicitWorkspaceLock(t *testing.T) {
	t.Parallel()
	repo, suite := rustSuiteRepository(t, "pub fn unrelated() {}\n")
	suite.Rust.Manifest = "crates/app/Cargo.toml"
	writeRustSuiteFile(t, repo.Root, suite.Rust.Manifest, "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\nworkspace='../../workspace'\n")
	writeRustSuiteFile(t, repo.Root, "workspace/Cargo.toml", "[workspace]\nmembers=['../crates/app']\nresolver='3'\n")
	writeRustSuiteFile(t, repo.Root, "crates/app/src/lib.rs", "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n")
	lock := "version=4\n[[package]]\nname='app'\nversion='1.0.0'\n"
	writeRustSuiteFile(t, repo.Root, "crates/app/Cargo.lock", lock)
	missing := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 1)
	if !missing.Failed() || !strings.Contains(missing.FailureMessage, "contained Cargo.lock") {
		t.Fatalf("unrelated root or member lock replaced the workspace lock: %+v", missing)
	}
	writeRustSuiteFile(t, repo.Root, "workspace/Cargo.lock", lock)
	if recovered := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 2); recovered.Failed() {
		t.Fatalf("valid explicit workspace failed: %+v", recovered)
	}
}
