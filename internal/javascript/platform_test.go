package javascript

import (
	"os"
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
