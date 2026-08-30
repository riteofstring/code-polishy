//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package behaviorreview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func openArtifactRoot(path string, components []string, create bool) (*artifactHandle, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	roots := []*os.Root{root}
	resolved := path
	for _, component := range components {
		next, err := openArtifactRootChild(root, component, create)
		if err != nil {
			closeArtifactRoots(roots)
			if errors.Is(err, os.ErrNotExist) && !create {
				return nil, fmt.Errorf("%w: %w", errMissingArtifactRoot, os.ErrNotExist)
			}
			return nil, err
		}
		root = next
		roots = append(roots, root)
		resolved = filepath.Join(resolved, component)
	}
	return &artifactHandle{path: resolved, roots: roots}, nil
}

func openArtifactRootChild(root *os.Root, name string, create bool) (*os.Root, error) {
	if _, err := artifactNameComponents(name); err != nil {
		return nil, err
	}
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		info, err = root.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errUnsafeArtifactPath
	}
	return root.OpenRoot(name)
}

func openArtifactFile(directory *artifactHandle, name string) (*os.File, error) {
	root := artifactRoot(directory)
	if root == nil {
		return nil, errUnsafeArtifactPath
	}
	if _, err := artifactNameComponents(name); err != nil {
		return nil, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errUnsafeArtifactPath
	}
	return root.Open(name)
}

func writeArtifactAtomicAt(directory *artifactHandle, name string, data []byte) error {
	root := artifactRoot(directory)
	if root == nil {
		return fmt.Errorf("%w: behavior review artifact target is not a regular file", ErrInvalidInput)
	}
	if _, err := artifactNameComponents(name); err != nil {
		return fmt.Errorf("%w: behavior review artifact target is not a regular file", ErrInvalidInput)
	}
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("%w: behavior review artifact target is not a regular file", ErrInvalidInput)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return operational("inspect behavior review artifact", err)
	}
	temporaryName, err := temporaryArtifactName()
	if err != nil {
		return operational("create behavior review artifact", err)
	}
	temporary, err := root.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return operational("create behavior review artifact", err)
	}
	defer root.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return operational("set behavior review artifact permissions", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return operational("write behavior review artifact", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return operational("sync behavior review artifact", err)
	}
	if err := temporary.Close(); err != nil {
		return operational("close behavior review artifact", err)
	}
	if err := root.Rename(temporaryName, name); err != nil {
		return operational("replace behavior review artifact", err)
	}
	return nil
}

func ensureArtifactDirectoryAt(directory *artifactHandle, name, label string) error {
	root := artifactRoot(directory)
	if root == nil {
		return fmt.Errorf("%w: %s artifact directory is not a contained directory", ErrInvalidInput, label)
	}
	if _, err := artifactNameComponents(name); err != nil {
		return fmt.Errorf("%w: %s artifact directory is not a contained directory", ErrInvalidInput, label)
	}
	if err := root.MkdirAll(name, 0o700); err != nil {
		return operational("create "+label+" artifact directory", err)
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return operational("open "+label+" artifact directory", err)
	}
	return child.Close()
}

func reserveArtifactDirectoryAt(directory *artifactHandle, parentName, prefix string) (string, error) {
	root := artifactRoot(directory)
	if root == nil || strings.ContainsAny(prefix, "/\\") || prefix == "" {
		return "", fmt.Errorf("%w: regression proof worktree prefix is invalid", ErrInvalidInput)
	}
	if err := ensureArtifactDirectoryAt(directory, parentName, "regression proof worktree"); err != nil {
		return "", err
	}
	parent, err := root.OpenRoot(parentName)
	if err != nil {
		return "", operational("open regression proof worktree directory", err)
	}
	defer parent.Close()
	for range 16 {
		identifier, err := temporaryArtifactName()
		if err != nil {
			return "", operational("reserve regression proof worktree", err)
		}
		name := prefix + strings.TrimPrefix(identifier, ".behavior-review-")
		if err := parent.Mkdir(name, 0o700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", operational("reserve regression proof worktree", err)
		}
		if err := parent.Remove(name); err != nil {
			return "", operational("prepare regression proof worktree", err)
		}
		return artifactPath(artifactPath(directory.path, parentName), name), nil
	}
	return "", operational("reserve regression proof worktree", errors.New("could not allocate a worktree path"))
}

func artifactRoot(directory *artifactHandle) *os.Root {
	if directory == nil || len(directory.roots) == 0 {
		return nil
	}
	return directory.roots[len(directory.roots)-1]
}

func closeArtifactRoots(roots []*os.Root) {
	for index := len(roots) - 1; index >= 0; index-- {
		_ = roots[index].Close()
	}
}
