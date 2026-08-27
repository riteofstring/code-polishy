//go:build unix

package runner

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const resourceLockNamespaceRoot = "/tmp"

const (
	resourceLockPollInterval     = 20 * time.Millisecond
	resourceWaitProgressInterval = time.Second
)

type resourceFiles []*os.File

func acquireResourceFiles(ctx context.Context, resources []string) (resourceLease, error) {
	directory, err := secureResourceLockDirectory()
	if err != nil {
		return nil, err
	}
	files := make(resourceFiles, 0, len(resources))
	for _, resource := range resources {
		file, lockErr := acquireResourceFile(ctx, directory, resource)
		if lockErr != nil {
			_ = files.release()
			return nil, lockErr
		}
		files = append(files, file)
	}
	return files, nil
}

func secureResourceLockDirectory() (string, error) {
	namespace := resourceLockNamespaceForUser(uint32(os.Geteuid()))
	if err := os.MkdirAll(namespace, 0o700); err != nil {
		return "", fmt.Errorf("create resource lock namespace: %w", err)
	}
	info, err := os.Lstat(namespace)
	if err != nil {
		return "", fmt.Errorf("inspect resource lock namespace: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return "", errors.New("resource lock namespace is not a private regular directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return "", errors.New("resource lock namespace is not owned by the current user")
	}
	resolved, err := filepath.EvalSymlinks(namespace)
	if err != nil {
		return "", errors.New("resource lock namespace is not canonical")
	}
	return resolved, nil
}

func resourceLockNamespaceForUser(uid uint32) string {
	return filepath.Join(resourceLockNamespaceRoot, fmt.Sprintf("code-polishy-resource-locks-%d", uid))
}

func acquireResourceFile(ctx context.Context, directory, resource string) (*os.File, error) {
	file, err := openResourceLock(directory, resource)
	if err != nil {
		return nil, err
	}
	if err := waitForResourceLock(ctx, file, resource); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func openResourceLock(directory, resource string) (*os.File, error) {
	digest := sha256.Sum256([]byte(resource))
	path := filepath.Join(directory, fmt.Sprintf("%x.lock", digest))
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open resource lock %q: %w", resource, err)
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err == nil && validatePrivateResourceLock(info, uint32(os.Geteuid())) == nil {
		return file, nil
	}
	_ = file.Close()
	return nil, fmt.Errorf("resource lock %q is not a private regular file", resource)
}

func waitForResourceLock(ctx context.Context, file *os.File, resource string) error {
	waitStarted := time.Now()
	nextProgress := waitStarted.Add(resourceWaitProgressInterval)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if resourceLockWaitError(err) != nil {
			return fmt.Errorf("lock resource %q: %w", resource, err)
		}
		now := time.Now()
		if !now.Before(nextProgress) {
			reportResourceWait(ctx, now.Sub(waitStarted))
			nextProgress = now.Add(resourceWaitProgressInterval)
		}
		if err := waitForResourceRetry(ctx, nextProgress); err != nil {
			return err
		}
	}
}

func resourceLockWaitError(err error) error {
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return nil
	}
	return err
}

func waitForResourceRetry(ctx context.Context, nextProgress time.Time) error {
	delay := min(resourceLockPollInterval, time.Until(nextProgress))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validatePrivateResourceLock(info os.FileInfo, uid uint32) error {
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return errors.New("resource lock is not a private regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uid {
		return errors.New("resource lock is not owned by the current user")
	}
	return nil
}

func (files resourceFiles) release() error {
	var result error
	for index := len(files) - 1; index >= 0; index-- {
		if err := files[index].Close(); err != nil && result == nil {
			result = err
		}
	}
	return result
}
