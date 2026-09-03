package release

import (
	"fmt"
	"os"
	"path/filepath"
)

func WriteLock(repoRoot string, lock Lock) error {
	data := RenderLock(lock)
	if _, err := parseLock(data, LockFilename); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(repoRoot, ".code-polishy-lock-*.tmp")
	if err != nil {
		return fmt.Errorf("stage %s: %w", LockFilename, err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("stage %s: %w", LockFilename, err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("stage %s: %w", LockFilename, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("stage %s: %w", LockFilename, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("stage %s: %w", LockFilename, err)
	}
	target := filepath.Join(repoRoot, LockFilename)
	if err := replaceLockFile(temporaryPath, target); err != nil {
		return fmt.Errorf("replace %s: %w", LockFilename, err)
	}
	remove = false
	return nil
}
