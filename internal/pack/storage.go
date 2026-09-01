package pack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func UserDataRoot() (string, error) {
	return userDataRoot(runtime.GOOS, os.Getenv, os.UserHomeDir)
}

func userDataRoot(goos string, getenv func(string) string, home func() (string, error)) (string, error) {
	if goos == "windows" {
		root := getenv("LOCALAPPDATA")
		if root == "" {
			return "", errors.New("LOCALAPPDATA is unavailable")
		}
		return filepath.Join(root, "CodePolishy", "packs"), nil
	}
	if root := getenv("XDG_DATA_HOME"); root != "" {
		if !filepath.IsAbs(root) {
			return "", errors.New("XDG_DATA_HOME must be absolute")
		}
		return filepath.Join(root, "code-polishy", "packs"), nil
	}
	root, err := home()
	if err != nil || root == "" {
		return "", fmt.Errorf("resolve user data home: %w", err)
	}
	return filepath.Join(root, ".local", "share", "code-polishy", "packs"), nil
}

func InstalledRoot(dataRoot, name, version, digest string) string {
	return filepath.Join(dataRoot, name, version, digest)
}
