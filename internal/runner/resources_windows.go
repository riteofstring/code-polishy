//go:build windows

package runner

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

type windowsResourceFile struct {
	file       *os.File
	overlapped windows.Overlapped
}
type resourceFiles []*windowsResourceFile

const (
	resourceLockPollInterval     = 20 * time.Millisecond
	resourceWaitProgressInterval = time.Second
)

func resourceLockNamespaceForUser(userID uint32) string {
	return fmt.Sprintf("code-polishy-resource-locks-%d", userID)
}

func acquireResourceFiles(ctx context.Context, resources []string) (resourceLease, error) {
	directory, err := windowsResourceLockDirectory()
	if err != nil {
		return nil, err
	}
	files := make(resourceFiles, 0, len(resources))
	for _, resource := range resources {
		file, err := acquireWindowsResource(ctx, directory, resource)
		if err != nil {
			_ = files.release()
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func windowsResourceLockDirectory() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(cache, "CodePolishy", "resource-locks")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Windows resource lock namespace is not a regular directory")
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil || !filepath.IsAbs(resolved) {
		return "", errors.New("Windows resource lock namespace is not canonical")
	}
	return resolved, nil
}

func acquireWindowsResource(ctx context.Context, directory, resource string) (*windowsResourceFile, error) {
	digest := sha256.Sum256([]byte(resource))
	path := filepath.Join(directory, fmt.Sprintf("%x.lock", digest))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := validateWindowsResourceFile(path); err != nil {
		file.Close()
		return nil, err
	}
	lock := &windowsResourceFile{file: file}
	waitStarted := time.Now()
	nextProgress := waitStarted.Add(resourceWaitProgressInterval)
	for {
		if err := ctx.Err(); err != nil {
			file.Close()
			return nil, err
		}
		err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, ^uint32(0), ^uint32(0), &lock.overlapped)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			file.Close()
			return nil, err
		}
		now := time.Now()
		if !now.Before(nextProgress) {
			reportResourceWait(ctx, now.Sub(waitStarted))
			nextProgress = now.Add(resourceWaitProgressInterval)
		}
		timer := time.NewTimer(min(resourceLockPollInterval, time.Until(nextProgress)))
		select {
		case <-ctx.Done():
			timer.Stop()
			file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func validateWindowsResourceFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Windows resource lock is not a regular file")
	}
	return nil
}

func (files resourceFiles) release() error {
	var result error
	for index := len(files) - 1; index >= 0; index-- {
		file := files[index]
		if err := windows.UnlockFileEx(windows.Handle(file.file.Fd()), 0, ^uint32(0), ^uint32(0), &file.overlapped); err != nil && result == nil {
			result = err
		}
		if err := file.file.Close(); err != nil && result == nil {
			result = err
		}
	}
	return result
}
