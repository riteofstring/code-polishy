package repository

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func (repo Repository) AllFiles() ([]string, error) {
	files, err := repo.includedFiles()
	if err != nil {
		return nil, err
	}
	return files, repo.validateDataFiles(files)
}

func (repo Repository) includedFiles() ([]string, error) {
	if repo.hasGit() {
		return repo.gitIncludedFiles()
	}
	return repo.walkIncludedFiles()
}

func (repo Repository) gitIncludedFiles() ([]string, error) {
	tracked, err := repo.gitLines("ls-files", "-z")
	if err != nil {
		return nil, err
	}
	untracked, err := repo.gitLines("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return repo.existingIncluded(append(tracked, untracked...)), nil
}

func (repo Repository) walkIncludedFiles() ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(repo.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(repo.Root, path)
		if err != nil {
			return err
		}
		normalized := filepath.ToSlash(relative)
		if entry.IsDir() {
			if normalized != "." && repo.IsExcluded(normalized+"/placeholder") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && !repo.IsExcluded(normalized) {
			files = append(files, normalized)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return uniqueSorted(files), nil
}

func (repo Repository) validateDataFiles(files []string) error {
	if err := repo.validateDynamicControlInputs(); err != nil {
		return err
	}
	for _, path := range files {
		if err := repo.validateDataFile(path); err != nil {
			return err
		}
	}
	return nil
}

func (repo Repository) validateDataFile(path string) error {
	if !policy.MatchesAny(path, repo.Config.Scope.Data) {
		return nil
	}
	if err := repo.validateDataClassification(path); err != nil {
		return err
	}
	raw := filepath.Join(repo.Root, filepath.FromSlash(path))
	info, err := os.Lstat(raw)
	if err != nil {
		return fmt.Errorf("validate scope.data file %s: %w", path, err)
	}
	resolved, err := repo.validateDataFileInfo(path, info)
	if err != nil {
		return err
	}
	return repo.validateDataFilePrefix(path, resolved)
}

func (repo Repository) validateDataClassification(path string) error {
	if repo.IsControlInput(path) {
		return fmt.Errorf("scope.data must not classify policy-sensitive control input %s", path)
	}
	if repo.IsExecutableSource(path) {
		return fmt.Errorf("scope.data must not classify executable source %s", path)
	}
	return nil
}

func (repo Repository) validateDataFileInfo(path string, info os.FileInfo) (string, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("scope.data must not classify symbolic link %s", path)
	}
	resolved, err := repo.Resolve(path)
	if err != nil {
		return "", fmt.Errorf("validate scope.data file %s: %w", path, err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		return "", fmt.Errorf("scope.data must not classify executable file %s", path)
	}
	return resolved, nil
}

func (repo Repository) validateDataFilePrefix(path, resolved string) error {
	file, err := os.Open(resolved)
	if err != nil {
		return fmt.Errorf("validate scope.data file %s: %w", path, err)
	}
	prefix := make([]byte, 2)
	count, readErr := file.Read(prefix)
	closeErr := file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("validate scope.data file %s: %w", path, readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("validate scope.data file %s: %w", path, closeErr)
	}
	if count == len(prefix) && bytes.Equal(prefix, []byte("#!")) {
		return fmt.Errorf("scope.data must not classify shebang source %s", path)
	}
	return nil
}
