package repository

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestPythonToolResolvesOnlyThePolicyRuntimeThePolicyRootCarries(t *testing.T) {
	t.Parallel()
	policyRoot := t.TempDir()
	repo := Repository{PolicyRoot: policyRoot}
	want := filepath.Join(policyRoot, ".tools", "python", runtime.GOOS+"-"+releaseArchitecture(runtime.GOARCH), executableName("python"))
	if got := repo.PythonTool(); got != want {
		t.Fatalf("PythonTool() = %q, want %q", got, want)
	}
}
