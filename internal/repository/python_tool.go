package repository

import (
	"path/filepath"
	"runtime"
)

func (repo Repository) PythonTool() string {
	return filepath.Join(repo.PolicyRoot, ".tools", "python", runtime.GOOS+"-"+releaseArchitecture(runtime.GOARCH), executableName("python"))
}

func (repo Repository) PythonCommand(source string, arguments ...string) []string {
	command := []string{repo.PythonTool(), "-I", "-B", "-c", source}
	return append(command, arguments...)
}
