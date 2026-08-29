package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func (manifest Manifest) Verify(releaseDir string) error {
	pending := make(map[string]Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		pending[entry.Path] = entry
	}
	walk := func(installed string, found fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read the installed Code Polishy release: %w", err)
		}
		if found.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(releaseDir, installed)
		if err != nil {
			return fmt.Errorf("read the installed Code Polishy release: %w", err)
		}
		relative = filepath.ToSlash(relative)

		if relative == ManifestFilename {
			return nil
		}
		recorded, isRecorded := pending[relative]
		if !isRecorded {
			return fmt.Errorf("%s is installed under a Code Polishy release that does not record it", installed)
		}
		delete(pending, relative)
		return verifyEntry(installed, found, recorded)
	}
	if err := filepath.WalkDir(releaseDir, walk); err != nil {
		return err
	}

	for _, entry := range manifest.Entries {
		if _, missing := pending[entry.Path]; missing {
			return fmt.Errorf(
				"the installed Code Polishy release is missing %s",
				filepath.Join(releaseDir, filepath.FromSlash(entry.Path)),
			)
		}
	}
	return nil
}

func verifyEntry(installed string, found fs.DirEntry, recorded Entry) error {
	if found.Type()&fs.ModeSymlink != 0 {
		if recorded.Symlink == "" {
			return fmt.Errorf("%s is a link, and the installed Code Polishy release records a file", installed)
		}
		target, err := os.Readlink(installed)
		if err != nil {
			return fmt.Errorf("read %s: %w", installed, err)
		}
		if target != recorded.Symlink {
			return fmt.Errorf("%s is not the link the installed Code Polishy release recorded", installed)
		}
		return nil
	}
	if !found.Type().IsRegular() {
		return fmt.Errorf("%s is neither a file nor a link the installed Code Polishy release can verify", installed)
	}
	if recorded.SHA256 == "" {
		return fmt.Errorf("%s is a file, and the installed Code Polishy release records a link", installed)
	}
	file, err := os.Open(installed)
	if err != nil {
		return fmt.Errorf("read %s: %w", installed, err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("read %s: %w", installed, err)
	}
	if hex.EncodeToString(digest.Sum(nil)) != recorded.SHA256 {
		return fmt.Errorf("%s is not the file the installed Code Polishy release recorded", installed)
	}
	return nil
}
