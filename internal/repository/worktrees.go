package repository

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (repo Repository) WorktreeRoots() ([]string, error) {
	root, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	roots := []string{root}
	if !repo.hasGit() {
		return roots, nil
	}
	entries, err := repo.gitLines("worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		path, found := strings.CutPrefix(entry, "worktree ")
		if !found {
			continue
		}
		resolved, err := filepath.EvalSymlinks(filepath.FromSlash(path))
		if err == nil && isDirectory(resolved) {
			roots = append(roots, resolved)
		}
	}
	return uniqueSorted(roots), nil
}
