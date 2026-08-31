package javascript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJavaScriptExecutableUsesNativeHostContract(t *testing.T) {
	if got := javascriptExecutable("node", "windows"); got != "node.exe" {
		t.Fatalf("windows node=%q", got)
	}
	if got := javascriptExecutable("node", "linux"); got != "node" {
		t.Fatalf("linux node=%q", got)
	}
	if !executableModeSupported("windows", 0o600) {
		t.Fatal("Windows regular executable required Unix mode bits")
	}
	if executableModeSupported("linux", os.FileMode(0o600)) {
		t.Fatal("Unix accepted a non-executable mode")
	}
}

func TestJavaScriptBundleContainsNativeTargetPaths(t *testing.T) {
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	for name, contents := range map[string]string{
		"AGENTS.md": "# Agent guidance\n",
		"CLAUDE.md": "Read `AGENTS.md`.\n",
	} {
		if err := os.WriteFile(filepath.Join(target, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := (Bundle{PolicyRoot: policyRoot}).Format(t.Context(), target, []string{"AGENTS.md", "CLAUDE.md"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Unsupported) != 0 {
		t.Fatalf("native target files escaped containment: %+v", result.Unsupported)
	}
}
