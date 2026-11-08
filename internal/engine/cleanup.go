package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/riteofstring/code-polishy/internal/ownedoutput"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

type CleanupOptions struct {
	Reports    bool
	Artifacts  bool
	BuildCache bool
	DryRun     bool
}

type CleanupEntry struct {
	Path  string
	Bytes int64
}

type CleanupKept struct {
	Path   string
	Reason string
}

type RepositoryCleanupResult struct {
	Removed     []CleanupEntry
	Kept        []CleanupKept
	BuildCaches []ownedoutput.Entry
}

func (result RepositoryCleanupResult) ReclaimedBytes() int64 {
	var total int64
	for _, entry := range result.Removed {
		total += entry.Bytes
	}
	for _, entry := range result.BuildCaches {
		if entry.Action == "removed" {
			total += entry.Bytes
		}
	}
	return total
}

func CleanupRepository(repositoryRoot string, options CleanupOptions) (RepositoryCleanupResult, error) {
	result := RepositoryCleanupResult{}
	root, err := os.OpenRoot(repositoryRoot)
	if err != nil {
		return result, err
	}
	defer root.Close()
	candidates := []string{}
	var problems error
	if options.Reports {
		paths, kept, err := reportCleanupCandidates(root)
		if err != nil {
			problems = errors.Join(problems, err)
		}
		candidates = append(candidates, paths...)
		result.Kept = append(result.Kept, kept...)
	}
	if options.Artifacts {
		paths, kept, err := testartifact.CleanupCandidates(repositoryRoot)
		if err != nil {
			problems = errors.Join(problems, err)
		}
		candidates = append(candidates, paths...)
		for _, path := range kept {
			result.Kept = append(result.Kept, CleanupKept{Path: path, Reason: "active, unowned, or unsafe test artifacts"})
		}
	}
	plan, kept := measureCleanupCandidates(root, candidates)
	result.Kept = append(result.Kept, kept...)
	for _, entry := range plan {
		if !options.DryRun {
			if err := root.RemoveAll(entry.Path); err != nil {
				problems = errors.Join(problems, fmt.Errorf("remove %s: %w", entry.Path, err))
				continue
			}
		}
		result.Removed = append(result.Removed, entry)
	}
	if options.BuildCache {
		entries, err := cleanupBuildCaches(context.Background(), repositoryRoot, options.DryRun)
		result.BuildCaches = entries
		problems = errors.Join(problems, err)
	}
	return result, problems
}

func measureCleanupCandidates(root *os.Root, candidates []string) ([]CleanupEntry, []CleanupKept) {
	plan := []CleanupEntry{}
	kept := []CleanupKept{}
	for _, path := range candidates {
		bytes, err := cleanupTreeBytes(root, path)
		if err != nil {
			kept = append(kept, CleanupKept{Path: path, Reason: err.Error()})
			continue
		}
		plan = append(plan, CleanupEntry{Path: path, Bytes: bytes})
	}
	return plan, kept
}

func cleanupTreeBytes(root *os.Root, path string) (int64, error) {
	var bytes int64
	err := fs.WalkDir(root.FS(), path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("contains a linked or special entry")
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		return nil
	})
	return bytes, err
}

func openOutputCleanupRoot(root *os.Root, path string) (*os.Root, error) {
	info, err := root.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is not a real directory", path)
	}
	return root.OpenRoot(path)
}
