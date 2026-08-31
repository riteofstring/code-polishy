//go:build windows

package gaterun

import (
	"errors"
	"fmt"
	"io"
	"os"
)

type artifactRootHandle struct{}

func makeArtifactRoot(path string) (artifactRootHandle, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return artifactRootHandle{}, err
	}
	defer root.Close()
	info, err := root.Stat(".")
	if err != nil {
		return artifactRootHandle{}, err
	}
	if !info.IsDir() {
		return artifactRootHandle{}, errUnsafeArtifact
	}
	return artifactRootHandle{}, nil
}

func platformEnsureArtifactDirectory(directory artifactDirectory, create bool) error {
	_, closeDirectory, err := openRootArtifactDirectory(directory, create)
	if err != nil {
		return err
	}
	return closeDirectory()
}

func platformCreateArtifactDirectory(directory artifactDirectory) error {
	if len(directory.components) == 0 {
		return errUnsafeArtifact
	}
	parent := artifactDirectory{root: directory.root, components: append([]string{}, directory.components[:len(directory.components)-1]...)}
	root, closeDirectory, err := openRootArtifactDirectory(parent, false)
	if err != nil {
		return err
	}
	defer closeDirectory()
	name := directory.components[len(directory.components)-1]
	if err := root.Mkdir(name, 0o700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%w: gate execution directory exists", os.ErrExist)
		}
		return err
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return err
	}
	return child.Close()
}

func platformWriteArtifactAtomic(artifact artifactFile, data []byte) error {
	root, closeDirectory, err := openRootArtifactDirectory(artifact.directory, false)
	if err != nil {
		return err
	}
	defer closeDirectory()
	if err := validateRootReplacementTarget(root, artifact.name); err != nil {
		return err
	}
	temporaryName, err := temporaryArtifactName()
	if err != nil {
		return err
	}
	file, err := root.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = root.Remove(temporaryName)
		}
	}()
	if err := writeRootTemporaryArtifact(file, data); err != nil {
		return err
	}
	if err := root.Rename(temporaryName, artifact.name); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func platformReadArtifact(artifact artifactFile, maximum int) ([]byte, error) {
	root, closeDirectory, err := openRootArtifactDirectory(artifact.directory, false)
	if err != nil {
		return nil, err
	}
	defer closeDirectory()
	info, err := root.Lstat(artifact.name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errUnsafeArtifact
	}
	file, err := root.Open(artifact.name)
	if err != nil {
		return nil, err
	}
	return readRootRestrictiveArtifact(file, maximum)
}

func platformRemoveArtifact(artifact artifactFile) error {
	root, closeDirectory, err := openRootArtifactDirectory(artifact.directory, false)
	if err != nil {
		return err
	}
	defer closeDirectory()
	if err := validateRootReplacementTarget(root, artifact.name); err != nil {
		return err
	}
	return root.Remove(artifact.name)
}

func openRootArtifactDirectory(directory artifactDirectory, create bool) (*os.Root, func() error, error) {
	root, err := os.OpenRoot(directory.root.path)
	if err != nil {
		return nil, nil, err
	}
	roots := []*os.Root{root}
	for _, component := range directory.components {
		if err := ensureRootDirectory(root, component, create); err != nil {
			closeRoots(roots)
			return nil, nil, err
		}
		next, err := root.OpenRoot(component)
		if err != nil {
			closeRoots(roots)
			return nil, nil, err
		}
		roots = append(roots, next)
		root = next
	}
	return root, func() error { return closeRoots(roots) }, nil
}

func ensureRootDirectory(root *os.Root, name string, create bool) error {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		err = root.Mkdir(name, 0o700)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		info, err = root.Lstat(name)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errUnsafeArtifact
	}
	return nil
}

func validateRootReplacementTarget(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errUnsafeArtifact
	}
	return nil
}

func writeRootTemporaryArtifact(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func readRootRestrictiveArtifact(file *os.File, maximum int) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(maximum) {
		_ = file.Close()
		return nil, errUnsafeArtifact
	}
	data, readErr := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(data) > maximum {
		return nil, errUnsafeArtifact
	}
	return data, nil
}

func closeRoots(roots []*os.Root) error {
	var result error
	for index := len(roots) - 1; index >= 0; index-- {
		result = errors.Join(result, roots[index].Close())
	}
	return result
}
