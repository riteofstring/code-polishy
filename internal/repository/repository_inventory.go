package repository

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	untracked, err := repo.gitIncludedUntrackedFiles()
	if err != nil {
		return nil, err
	}
	return repo.existingIncluded(append(tracked, untracked...))
}

func (repo Repository) gitIncludedUntrackedFiles() ([]string, error) {
	excludes := append(append([]string{}, policy.DefaultExcludes...), repo.Config.Scope.Exclude...)
	return gitUntrackedFiles(repo.Root, excludes, repo.DynamicControlInputs)
}

func gitUntrackedFiles(root string, excludes, controlInputs []string) ([]string, error) {
	arguments := []string{"ls-files", "-z", "--others", "--exclude-standard", "--"}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." || !excludedGitInventoryDirectory(relative, excludes, controlInputs) {
			return nil
		}
		arguments = append(arguments, ":(top,literal,exclude)"+relative)
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	return gitLinesAt(root, arguments...)
}

func excludedGitInventoryDirectory(path string, excludes, controlInputs []string) bool {
	prefix := path + "/"
	for _, input := range controlInputs {
		if strings.HasPrefix(input, prefix) {
			return false
		}
	}
	for _, pattern := range excludes {
		if strings.HasSuffix(pattern, "**") && policy.Match(prefix, pattern) {
			return true
		}
	}
	return false
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
		if (entry.Type().IsRegular() || entry.Type()&os.ModeSymlink != 0) && !repo.IsExcluded(normalized) {
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
	if repo.dataExecutableClassification(path) {
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

func (repo Repository) dataExecutableClassification(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	if extension == ".js" || extension == ".mjs" {
		return false
	}
	return repo.builtInLanguage(path) != "" || extension == ".ps1" || extension == ".psm1" || len(repo.computeDeclaredLanguages(path)) > 0
}
