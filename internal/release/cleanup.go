package release

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const removalPrefix = ".code-polishy-removed-"

type KeptRelease struct {
	Name         string
	Repositories []string
}

type RemovedRelease struct {
	Name  string
	Bytes int64
}

type CleanupResult struct {
	Prefix                string
	Kept                  []KeptRelease
	Removed               []RemovedRelease
	ForgottenRepositories []string
}

func (result CleanupResult) ReclaimedBytes() int64 {
	total := int64(0)
	for _, removed := range result.Removed {
		total += removed.Bytes
	}
	return total
}

type stagedRemoval struct {
	name, path string
}

func CleanReleases(repoRoot, policyRoot string, dryRun bool) (CleanupResult, error) {
	root, prefix, err := cleanupScope(repoRoot, policyRoot)
	if err != nil {
		return CleanupResult{}, err
	}
	unlock, err := lockPrefixFile(prefix, cleanupLockFilename)
	if err != nil {
		return CleanupResult{}, err
	}
	defer unlock()
	result, staged, stageErr := stageCleanup(root, prefix, dryRun)
	removed, removeErr := removeStaged(staged, dryRun)
	result.Removed = removed
	return result, errors.Join(stageErr, removeErr)
}

func stageCleanup(root, prefix string, dryRun bool) (CleanupResult, []stagedRemoval, error) {
	unlock, err := lockPrefixFile(prefix, repositoryRegistryLockFilename)
	if err != nil {
		return CleanupResult{}, nil, err
	}
	defer unlock()
	repositories, err := readRepositoryRegistry(prefix)
	if err != nil {
		return CleanupResult{}, nil, fmt.Errorf("%w; repair or remove it before cleanup", err)
	}
	keep, forgotten, err := lockedReleases(append(slices.Clone(repositories), root))
	if err != nil {
		return CleanupResult{}, nil, err
	}
	candidates, err := removableReleases(prefix, keep)
	if err != nil {
		return CleanupResult{}, nil, err
	}
	result := CleanupResult{Prefix: prefix, ForgottenRepositories: forgotten}
	for _, name := range slices.Sorted(maps.Keys(keep)) {
		result.Kept = append(result.Kept, KeptRelease{Name: name, Repositories: keep[name]})
	}
	staged, err := stageRemovals(prefix, candidates, dryRun)
	if err != nil || dryRun || len(forgotten) == 0 {
		return result, staged, err
	}
	return result, staged, writeRepositoryRegistry(prefix, slices.DeleteFunc(repositories, func(repository string) bool {
		return slices.Contains(forgotten, repository)
	}))
}

func cleanupScope(repoRoot, policyRoot string) (string, string, error) {
	root, err := canonicalDirectory(repoRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve the repository root: %w", err)
	}
	lock, present, err := ReadLock(root)
	if err != nil {
		return "", "", err
	}
	if !present {
		return "", "", fmt.Errorf("%s has no %s", root, LockFilename)
	}
	prefix, installed := installedReleasePrefix(policyRoot, lock)
	if !installed {
		return "", "", errors.New("cleanup runs from the installed release this repository locks; run ./code-polishyw cleanup")
	}
	return root, prefix, nil
}

func stageRemovals(prefix string, candidates []string, dryRun bool) ([]stagedRemoval, error) {
	staged := []stagedRemoval{}
	for _, candidate := range candidates {
		name := filepath.Base(candidate)
		if dryRun || strings.HasPrefix(name, removalPrefix) {
			staged = append(staged, stagedRemoval{name: name, path: candidate})
			continue
		}
		staging, err := os.MkdirTemp(prefix, removalPrefix)
		if err != nil {
			return staged, err
		}
		if err := os.Rename(candidate, filepath.Join(staging, name)); err != nil {
			return staged, errors.Join(fmt.Errorf("remove %s: %w", candidate, err), os.Remove(staging))
		}
		staged = append(staged, stagedRemoval{name: name, path: staging})
	}
	return staged, nil
}

func removeStaged(staged []stagedRemoval, dryRun bool) ([]RemovedRelease, error) {
	removed := []RemovedRelease{}
	for _, removal := range staged {
		bytes, err := treeBytes(removal.path)
		if err != nil {
			return removed, err
		}
		if !dryRun {
			if err := os.RemoveAll(removal.path); err != nil {
				return removed, err
			}
		}
		removed = append(removed, RemovedRelease{Name: removal.name, Bytes: bytes})
	}
	return removed, nil
}

func lockedReleases(repositories []string) (map[string][]string, []string, error) {
	keep := map[string][]string{}
	forgotten := []string{}
	for _, repository := range slices.Compact(slices.Sorted(slices.Values(repositories))) {
		lock, present, err := ReadLock(repository)
		if err != nil {
			return nil, nil, fmt.Errorf("%w; cleanup cannot tell which release %s requires, so it removed nothing", err, repository)
		}
		if !present {
			forgotten = append(forgotten, repository)
			continue
		}
		name := filepath.Base(Directory("", lock))
		keep[name] = append(keep[name], repository)
	}
	return keep, forgotten, nil
}

func removableReleases(prefix string, keep map[string][]string) ([]string, error) {
	candidates := []string{}
	leftovers, err := os.ReadDir(prefix)
	if err != nil {
		return nil, err
	}
	for _, entry := range leftovers {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), removalPrefix) {
			candidates = append(candidates, filepath.Join(prefix, entry.Name()))
		}
	}
	releases, err := os.ReadDir(filepath.Join(prefix, releasesDirectory))
	if errors.Is(err, os.ErrNotExist) {
		return candidates, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range releases {
		if entry.IsDir() && isReleaseDirectoryName(entry.Name()) && keep[entry.Name()] == nil {
			candidates = append(candidates, filepath.Join(prefix, releasesDirectory, entry.Name()))
		}
	}
	return candidates, nil
}

func isReleaseDirectoryName(name string) bool {
	separator := len(name) - 65
	return separator > 0 && name[separator] == '-' &&
		versionPattern.MatchString(name[:separator]) && digestPattern.MatchString(name[separator+1:])
}

func treeBytes(root string) (int64, error) {
	total := int64(0)
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
