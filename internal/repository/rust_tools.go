package repository

import (
	"path/filepath"
	"runtime"
)

func (repo Repository) RustTool(name string) string {
	return filepath.Join(repo.PolicyRoot, ".tools", "rust", runtime.GOOS+"-"+releaseArchitecture(runtime.GOARCH), "bin", executableName(name))
}
