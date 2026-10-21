package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/riteofstring/code-polishy/internal/durablefile"
)

const (
	RepositoryRegistryFilename = "repositories.json"

	repositoryRegistryLockFilename = ".repositories.lock"
	cleanupLockFilename            = ".cleanup.lock"

	repositoryRegistryProtocol     = "code-polishy-repositories/v1"
	maximumRepositoryRegistryBytes = 4 << 20
)

type repositoryRegistry struct {
	Protocol     string   `json:"protocol"`
	Repositories []string `json:"repositories"`
}

func RecordRepository(repoRoot, policyRoot string) error {
	root, err := canonicalDirectory(repoRoot)
	if err != nil {
		return nil
	}
	lock, present, err := ReadLock(root)
	if err != nil || !present {
		return nil
	}
	prefix, installed := installedReleasePrefix(policyRoot, lock)
	if !installed {
		return nil
	}
	if repositories, err := readRepositoryRegistry(prefix); err != nil || slices.Contains(repositories, root) {
		return err
	}
	unlock, err := lockPrefixFile(prefix, repositoryRegistryLockFilename)
	if err != nil {
		return err
	}
	defer unlock()
	repositories, err := readRepositoryRegistry(prefix)
	if err != nil || slices.Contains(repositories, root) {
		return err
	}
	retained := slices.DeleteFunc(repositories, func(repository string) bool {
		_, err := os.Lstat(filepath.Join(repository, LockFilename))
		return errors.Is(err, os.ErrNotExist)
	})
	return writeRepositoryRegistry(prefix, append(retained, root))
}

func lockPrefixFile(prefix, name string) (func(), error) {
	path := filepath.Join(prefix, name)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := waitForFileLock(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() { _ = file.Close() }, nil
}

func installedReleasePrefix(policyRoot string, lock Lock) (string, bool) {
	root, err := canonicalDirectory(policyRoot)
	if err != nil {
		return "", false
	}
	prefix := filepath.Dir(filepath.Dir(root))
	return prefix, Directory(prefix, lock) == root
}

func canonicalDirectory(directory string) (string, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}

func repositoryRegistryPath(prefix string) string {
	return filepath.Join(prefix, RepositoryRegistryFilename)
}

func readRepositoryRegistry(prefix string) ([]string, error) {
	source := repositoryRegistryPath(prefix)
	file, err := os.Open(source)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maximumRepositoryRegistryBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, fmt.Errorf("read %s: %w", source, err)
	}
	if len(data) > maximumRepositoryRegistryBytes {
		return nil, fmt.Errorf("%s exceeds its byte bound", source)
	}
	var registry repositoryRegistry
	if err := decodeExactly(data, source, &registry); err != nil {
		return nil, err
	}
	if registry.Protocol != repositoryRegistryProtocol {
		return nil, fmt.Errorf("%s records the unsupported protocol %q", source, registry.Protocol)
	}
	return registry.Repositories, validateRegisteredRepositories(registry.Repositories, source)
}

func validateRegisteredRepositories(repositories []string, source string) error {
	for index, repository := range repositories {
		if !filepath.IsAbs(repository) || filepath.Clean(repository) != repository {
			return fmt.Errorf("%s records the unusable repository path %q", source, repository)
		}
		if index > 0 && repositories[index-1] >= repository {
			return fmt.Errorf("%s does not record its repositories once each in order, at %q", source, repository)
		}
	}
	return nil
}

func writeRepositoryRegistry(prefix string, repositories []string) error {
	repositories = slices.Compact(slices.Sorted(slices.Values(repositories)))
	var rendered bytes.Buffer
	encoder := json.NewEncoder(&rendered)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(repositoryRegistry{Protocol: repositoryRegistryProtocol, Repositories: repositories}); err != nil {
		return err
	}
	if rendered.Len() > maximumRepositoryRegistryBytes {
		return fmt.Errorf("%s would exceed its byte bound", repositoryRegistryPath(prefix))
	}
	if err := durablefile.Write(repositoryRegistryPath(prefix), rendered.Bytes(), 0o644); err != nil {
		return fmt.Errorf("replace %s: %w", repositoryRegistryPath(prefix), err)
	}
	return nil
}
