package repository

import (
	"path/filepath"
	"runtime"
)

func (repo Repository) PythonTool() string {
	return filepath.Join(repo.PolicyRoot, ".tools", "python", runtime.GOOS+"-"+releaseArchitecture(runtime.GOARCH), executableName("python"))
}
