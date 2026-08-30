//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package behaviorreview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

func openArtifactRoot(path string, components []string, create bool) (*artifactHandle, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, classifyArtifactPathError(err)
	}
	current := os.NewFile(uintptr(fd), path)
	if current == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open behavior review artifact root")
	}
	resolved := path
	for _, component := range components {
		next, err := openArtifactDirectoryAt(current, component, create)
		if err != nil {
			_ = current.Close()
			if errors.Is(err, unix.ENOENT) && !create {
				return nil, fmt.Errorf("%w: %w", errMissingArtifactRoot, os.ErrNotExist)
			}
			return nil, classifyArtifactPathError(err)
		}
		if err := current.Close(); err != nil {
			_ = next.Close()
			return nil, err
		}
		current = next
		resolved = filepath.Join(resolved, component)
	}
	return &artifactHandle{path: resolved, file: current}, nil
}

func openArtifactDirectoryAt(parent *os.File, name string, create bool) (*os.File, error) {
	if _, err := artifactNameComponents(name); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	runtime.KeepAlive(parent)
	if errors.Is(err, unix.ENOENT) && create {
		if mkdirErr := unix.Mkdirat(int(parent.Fd()), name, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
			runtime.KeepAlive(parent)
			return nil, mkdirErr
		}
		fd, err = unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		runtime.KeepAlive(parent)
	}
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open behavior review artifact directory")
	}
	return file, nil
}

func duplicateArtifactDirectory(directory *artifactHandle) (*os.File, error) {
	if directory == nil || directory.file == nil {
		return nil, errUnsafeArtifactPath
	}
	fd, err := unix.Dup(int(directory.file.Fd()))
	runtime.KeepAlive(directory.file)
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	file := os.NewFile(uintptr(fd), directory.path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("duplicate behavior review artifact directory")
	}
	return file, nil
}

func artifactParent(directory *artifactHandle, name string) (*os.File, string, error) {
	components, err := artifactNameComponents(name)
	if err != nil {
		return nil, "", err
	}
	parent, err := duplicateArtifactDirectory(directory)
	if err != nil {
		return nil, "", err
	}
	for _, component := range components[:len(components)-1] {
		next, err := openArtifactDirectoryAt(parent, component, false)
		if err != nil {
			_ = parent.Close()
			return nil, "", classifyArtifactPathError(err)
		}
		if err := parent.Close(); err != nil {
			_ = next.Close()
			return nil, "", err
		}
		parent = next
	}
	return parent, components[len(components)-1], nil
}

func openArtifactFile(directory *artifactHandle, name string) (*os.File, error) {
	parent, filename, err := artifactParent(directory, name)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), filename, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	runtime.KeepAlive(parent)
	if err != nil {
		return nil, classifyArtifactPathError(err)
	}
	file := os.NewFile(uintptr(fd), filename)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open behavior review artifact")
	}
	return file, nil
}

func writeArtifactAtomicAt(directory *artifactHandle, name string, data []byte) error {
	parent, filename, err := artifactParent(directory, name)
	if err != nil {
		return artifactWriteError("create behavior review artifact", err)
	}
	defer parent.Close()
	if err := rejectUnsafeArtifactTargetAt(parent, filename); err != nil {
		return artifactWriteError("inspect behavior review artifact", err)
	}
	temporary, temporaryName, err := createTemporaryArtifactAt(parent)
	if err != nil {
		return artifactWriteError("create behavior review artifact", err)
	}
	defer removeTemporaryArtifactAt(parent, temporaryName)
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
	if err := unix.Renameat(int(parent.Fd()), temporaryName, int(parent.Fd()), filename); err != nil {
		runtime.KeepAlive(parent)
		return artifactWriteError("replace behavior review artifact", err)
	}
	runtime.KeepAlive(parent)
	return nil
}

func rejectUnsafeArtifactTargetAt(parent *os.File, name string) error {
	var info unix.Stat_t
	err := unix.Fstatat(int(parent.Fd()), name, &info, unix.AT_SYMLINK_NOFOLLOW)
	runtime.KeepAlive(parent)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return classifyArtifactPathError(err)
	}
	if uint32(info.Mode)&uint32(unix.S_IFMT) != uint32(unix.S_IFREG) {
		return errUnsafeArtifactPath
	}
	return nil
}

func createTemporaryArtifactAt(parent *os.File) (*os.File, string, error) {
	for range 16 {
		name, err := temporaryArtifactName()
		if err != nil {
			return nil, "", err
		}
		fd, err := unix.Openat(int(parent.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
		runtime.KeepAlive(parent)
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return nil, "", classifyArtifactPathError(err)
		}
		file := os.NewFile(uintptr(fd), name)
		if file == nil {
			_ = unix.Close(fd)
			return nil, "", errors.New("create behavior review artifact")
		}
		return file, name, nil
	}
	return nil, "", errors.New("reserve behavior review artifact name")
}

func removeTemporaryArtifactAt(parent *os.File, name string) {
	_ = unix.Unlinkat(int(parent.Fd()), name, 0)
	runtime.KeepAlive(parent)
}

func ensureArtifactDirectoryAt(directory *artifactHandle, name, label string) error {
	child, err := openArtifactDirectoryAt(directory.file, name, true)
	if err != nil {
		if errors.Is(err, errUnsafeArtifactPath) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
			return fmt.Errorf("%w: %s artifact directory is not a contained directory", ErrInvalidInput, label)
		}
		return operational("create "+label+" artifact directory", err)
	}
	return child.Close()
}

func reserveArtifactDirectoryAt(directory *artifactHandle, parentName, prefix string) (string, error) {
	if strings.ContainsAny(prefix, "/\\") || prefix == "" {
		return "", fmt.Errorf("%w: regression proof worktree prefix is invalid", ErrInvalidInput)
	}
	if err := ensureArtifactDirectoryAt(directory, parentName, "regression proof worktree"); err != nil {
		return "", err
	}
	parent, err := artifactDirectoryAt(directory, parentName, false)
	if err != nil {
		return "", artifactWriteError("open regression proof worktree directory", err)
	}
	defer parent.Close()
	for range 16 {
		identifier, err := temporaryArtifactName()
		if err != nil {
			return "", operational("reserve regression proof worktree", err)
		}
		name := prefix + strings.TrimPrefix(identifier, ".behavior-review-")
		if err := unix.Mkdirat(int(parent.Fd()), name, 0o700); err != nil {
			runtime.KeepAlive(parent)
			if errors.Is(err, unix.EEXIST) {
				continue
			}
			return "", artifactWriteError("reserve regression proof worktree", err)
		}
		if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil {
			runtime.KeepAlive(parent)
			return "", artifactWriteError("prepare regression proof worktree", err)
		}
		runtime.KeepAlive(parent)
		return artifactPath(artifactPath(directory.path, parentName), name), nil
	}
	return "", operational("reserve regression proof worktree", errors.New("could not allocate a worktree path"))
}

func artifactDirectoryAt(directory *artifactHandle, name string, create bool) (*os.File, error) {
	components, err := artifactNameComponents(name)
	if err != nil {
		return nil, err
	}
	current, err := duplicateArtifactDirectory(directory)
	if err != nil {
		return nil, err
	}
	for _, component := range components {
		next, err := openArtifactDirectoryAt(current, component, create)
		if err != nil {
			_ = current.Close()
			return nil, classifyArtifactPathError(err)
		}
		if err := current.Close(); err != nil {
			_ = next.Close()
			return nil, err
		}
		current = next
	}
	return current, nil
}

func artifactWriteError(operation string, err error) error {
	if errors.Is(err, errUnsafeArtifactPath) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return fmt.Errorf("%w: behavior review artifact target is not a regular file", ErrInvalidInput)
	}
	return operational(operation, err)
}

func classifyArtifactPathError(err error) error {
	if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return fmt.Errorf("%w: %v", errUnsafeArtifactPath, err)
	}
	return err
}
