//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package gaterun

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

type artifactRootHandle struct {
	device uint64
	inode  uint64
}

func makeArtifactRoot(path string) (artifactRootHandle, error) {
	fd, err := unix.Open(path, artifactDirectoryFlags, 0)
	if err != nil {
		return artifactRootHandle{}, normalizeArtifactUnixError(err)
	}
	defer unix.Close(fd)
	info, err := descriptorStat(fd)
	if err != nil {
		return artifactRootHandle{}, err
	}
	if !directoryStat(info) {
		return artifactRootHandle{}, errUnsafeArtifact
	}
	return artifactRootHandle{device: uint64(info.Dev), inode: uint64(info.Ino)}, nil
}

func platformEnsureArtifactDirectory(directory artifactDirectory, create bool) error {
	fd, err := openArtifactDirectory(directory, create)
	if err != nil {
		return err
	}
	return unix.Close(fd)
}

func platformCreateArtifactDirectory(directory artifactDirectory) error {
	if len(directory.components) == 0 {
		return errUnsafeArtifact
	}
	parent := artifactDirectory{root: directory.root, components: append([]string{}, directory.components[:len(directory.components)-1]...)}
	fd, err := openArtifactDirectory(parent, false)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	name := directory.components[len(directory.components)-1]
	if err := unix.Mkdirat(fd, name, 0o700); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("%w: gate execution directory exists", os.ErrExist)
		}
		return normalizeArtifactUnixError(err)
	}
	child, err := unix.Openat(fd, name, artifactDirectoryFlags, 0)
	if err != nil {
		return normalizeArtifactUnixError(err)
	}
	defer unix.Close(child)
	if err := unix.Fchmod(child, 0o700); err != nil {
		return err
	}
	return nil
}

func platformWriteArtifactAtomic(artifact artifactFile, data []byte) error {
	fd, err := openArtifactDirectory(artifact.directory, false)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := validateReplacementTarget(fd, artifact.name); err != nil {
		return err
	}
	temporaryName, temporary, err := openTemporaryArtifactFile(fd)
	if err != nil {
		return err
	}
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = unix.Unlinkat(fd, temporaryName, 0)
		}
	}()
	if err := writeTemporaryArtifact(temporary, data); err != nil {
		return err
	}
	if err := unix.Renameat(fd, temporaryName, fd, artifact.name); err != nil {
		return normalizeArtifactUnixError(err)
	}
	removeTemporary = false
	return nil
}

func platformReadArtifact(artifact artifactFile, maximum int) ([]byte, error) {
	fd, err := openArtifactDirectory(artifact.directory, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	fileDescriptor, err := unix.Openat(fd, artifact.name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, normalizeArtifactUnixError(err)
	}
	file := os.NewFile(uintptr(fileDescriptor), artifact.name)
	return readRestrictiveArtifact(file, maximum)
}

func platformRemoveArtifact(artifact artifactFile) error {
	fd, err := openArtifactDirectory(artifact.directory, false)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := validateReplacementTarget(fd, artifact.name); err != nil {
		return err
	}
	if err := unix.Unlinkat(fd, artifact.name, 0); err != nil {
		return normalizeArtifactUnixError(err)
	}
	return nil
}

func openArtifactDirectory(directory artifactDirectory, create bool) (int, error) {
	fd, err := openArtifactRoot(directory.root)
	if err != nil {
		return -1, err
	}
	for _, component := range directory.components {
		child, openErr := openArtifactDirectoryComponent(fd, component, create)
		if openErr != nil {
			_ = unix.Close(fd)
			return -1, openErr
		}
		_ = unix.Close(fd)
		fd = child
	}
	return fd, nil
}

func openArtifactRoot(root artifactRoot) (int, error) {
	fd, err := unix.Open(root.path, artifactDirectoryFlags, 0)
	if err != nil {
		return -1, normalizeArtifactUnixError(err)
	}
	info, err := descriptorStat(fd)
	if err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if !directoryStat(info) || uint64(info.Dev) != root.handle.device || uint64(info.Ino) != root.handle.inode {
		_ = unix.Close(fd)
		return -1, errUnsafeArtifact
	}
	return fd, nil
}

func openArtifactDirectoryComponent(parent int, name string, create bool) (int, error) {
	fd, err := unix.Openat(parent, name, artifactDirectoryFlags, 0)
	if errors.Is(err, unix.ENOENT) && create {
		err = unix.Mkdirat(parent, name, 0o700)
		if err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, normalizeArtifactUnixError(err)
		}
		fd, err = unix.Openat(parent, name, artifactDirectoryFlags, 0)
	}
	if err != nil {
		return -1, normalizeArtifactUnixError(err)
	}
	info, err := descriptorStat(fd)
	if err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if !directoryStat(info) {
		_ = unix.Close(fd)
		return -1, errUnsafeArtifact
	}
	if create {
		if err := unix.Fchmod(fd, 0o700); err != nil {
			_ = unix.Close(fd)
			return -1, err
		}
	}
	return fd, nil
}

func validateReplacementTarget(directory int, name string) error {
	var info unix.Stat_t
	err := unix.Fstatat(directory, name, &info, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return normalizeArtifactUnixError(err)
	}
	if !restrictiveFileStat(info) {
		return errUnsafeArtifact
	}
	return nil
}

func openTemporaryArtifactFile(directory int) (string, *os.File, error) {
	for range 32 {
		name, err := temporaryArtifactName()
		if err != nil {
			return "", nil, err
		}
		fd, err := unix.Openat(directory, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return "", nil, normalizeArtifactUnixError(err)
		}
		if err := unix.Fchmod(fd, 0o600); err != nil {
			_ = unix.Close(fd)
			_ = unix.Unlinkat(directory, name, 0)
			return "", nil, err
		}
		return name, os.NewFile(uintptr(fd), name), nil
	}
	return "", nil, fmt.Errorf("%w: could not allocate a temporary gate artifact", ErrOperational)
}

func writeTemporaryArtifact(file *os.File, data []byte) error {
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

func readRestrictiveArtifact(file *os.File, maximum int) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > int64(maximum) {
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

func descriptorStat(fd int) (unix.Stat_t, error) {
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil {
		return unix.Stat_t{}, err
	}
	return info, nil
}

func directoryStat(info unix.Stat_t) bool {
	return info.Mode&unix.S_IFMT == unix.S_IFDIR
}

func restrictiveFileStat(info unix.Stat_t) bool {
	return info.Mode&unix.S_IFMT == unix.S_IFREG && info.Mode&0o077 == 0
}

func normalizeArtifactUnixError(err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return fmt.Errorf("%w: %v", errUnsafeArtifact, err)
	}
	return err
}

const artifactDirectoryFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
