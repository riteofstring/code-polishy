package repository

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func (repo Repository) CommandDiscoveryNote() string {
	if !isRegularFile(filepath.Join(repo.PolicyRoot, "release-manifest.json")) {
		return ""
	}
	prefix := filepath.Dir(filepath.Dir(repo.PolicyRoot))
	expected := filepath.Join(prefix, "bin", executableName("code-polishy"))
	found := executableOnPath("code-polishy", os.Getenv("PATH"))
	if sameExecutable(found, expected) {
		return fmt.Sprintf("stable command discovery: code-polishy resolves to %s", expected)
	}
	action := commandPathAction(filepath.Dir(expected), os.Getenv("SHELL"))
	if found == "" {
		return fmt.Sprintf("stable command discovery: %s is not on PATH; run %s", expected, action)
	}
	return fmt.Sprintf("stable command discovery: PATH selects %s instead of %s; run %s", found, expected, action)
}

func executableOnPath(name, pathValue string) string {
	name = executableName(name)
	for _, directory := range filepath.SplitList(pathValue) {
		if directory == "" {
			continue
		}
		candidate := filepath.Join(directory, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0) {
			return candidate
		}
	}
	return ""
}

func sameExecutable(first, second string) bool {
	if first == "" || second == "" {
		return false
	}
	firstResolved, firstErr := filepath.EvalSymlinks(first)
	secondResolved, secondErr := filepath.EvalSymlinks(second)
	return firstErr == nil && secondErr == nil && firstResolved == secondResolved
}

func commandPathAction(directory, shell string) string {
	quoted := shellSingleQuote(directory)
	if strings.EqualFold(filepath.Base(shell), "fish") {
		return "fish_add_path " + quoted
	}
	return "export PATH=" + quoted + ":\"$PATH\""
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
