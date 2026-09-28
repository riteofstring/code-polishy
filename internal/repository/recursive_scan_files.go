package repository

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func (repo Repository) RecursiveScanFiles() ([]string, error) {
	excludes := []string{".git/**", "**/.git/**", ".code-polishy-reports/**", "**/.code-polishy-reports/**"}
	if repo.hasGit() {
		return repo.recursiveGitFiles(repo.Root, "", excludes)
	}
	return repo.rawWalkFiles(excludes)
}

func (repo Repository) recursiveGitFiles(root, prefix string, excludes []string) ([]string, error) {
	entries, err := recursiveGitEntries(root)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, local := range entries {
		files, entryErr := repo.recursiveGitEntryFiles(root, prefix, local, excludes)
		if entryErr != nil {
			return nil, entryErr
		}
		result = append(result, files...)
	}
	return uniqueSorted(result), nil
}

func recursiveGitEntries(root string) ([]string, error) {
	tracked, err := gitLinesAt(root, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	untracked, err := gitLinesAt(root, "ls-files", "-z", "--others", "--exclude-per-directory=.gitignore")
	if err != nil {
		return nil, err
	}
	return uniqueSorted(append(tracked, untracked...)), nil
}

func (repo Repository) recursiveGitEntryFiles(root, prefix, local string, excludes []string) ([]string, error) {
	local = strings.TrimSuffix(local, "/")
	if local == "" {
		return nil, nil
	}
	relative := filepath.ToSlash(filepath.Join(filepath.FromSlash(prefix), filepath.FromSlash(local)))
	absolute := filepath.Join(root, filepath.FromSlash(local))
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return repo.recursiveGitDirectoryFiles(absolute, relative, excludes)
	}
	if isRegularOrSymlink(absolute) && !policy.MatchesAny(relative, excludes) {
		return []string{relative}, nil
	}
	return nil, nil
}

func (repo Repository) recursiveGitDirectoryFiles(absolute, relative string, excludes []string) ([]string, error) {
	if policy.MatchesAny(relative+"/placeholder", excludes) {
		return nil, nil
	}
	_, err := os.Lstat(filepath.Join(absolute, ".git"))
	if err == nil {
		return repo.recursiveGitFiles(absolute, relative, excludes)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return walkFiles(absolute, relative, excludes)
}
