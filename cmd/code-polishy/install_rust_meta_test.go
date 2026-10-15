package main

import (
	"strings"
	"testing"
)

func TestRustInstallerRequiresInstalledReleaseAuthority(t *testing.T) {
	root := t.TempDir()
	status, stdout, stderr := captureRunOutput(t, []string{
		"--repo-root", root, "--policy-root", root, "install-rust",
	})
	if status != 2 || stdout != "" || !strings.Contains(stderr, "has no release-manifest.json") {
		t.Fatalf("Rust setup did not enforce installed authority: status=%d stdout=%q stderr=%q", status, stdout, stderr)
	}
}
