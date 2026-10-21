package release

import (
	"fmt"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/durablefile"
)

func WriteLock(repoRoot string, lock Lock) error {
	data := RenderLock(lock)
	if _, err := parseLock(data, LockFilename); err != nil {
		return err
	}
	if err := durablefile.Write(filepath.Join(repoRoot, LockFilename), data, 0o644); err != nil {
		return fmt.Errorf("replace %s: %w", LockFilename, err)
	}
	return nil
}
