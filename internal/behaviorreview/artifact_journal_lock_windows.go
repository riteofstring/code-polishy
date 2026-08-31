//go:build windows

package behaviorreview

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"

	"golang.org/x/sys/windows"
)

var intentJournalProcessLock sync.Mutex

func lockIntentJournal(directory *artifactHandle) (func(), error) {
	intentJournalProcessLock.Lock()
	success := false
	defer func() {
		if !success {
			intentJournalProcessLock.Unlock()
		}
	}()
	root := artifactRoot(directory)
	if root == nil {
		return nil, fmt.Errorf("%w: behavior review journal lock is not a contained regular file", ErrInvalidInput)
	}
	file, err := openWindowsIntentJournalLock(root)
	if err != nil {
		return nil, err
	}
	lock := windows.Overlapped{}
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, ^uint32(0), ^uint32(0), &lock); err != nil {
		_ = file.Close()
		return nil, operational("lock behavior review journal", err)
	}
	success = true
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, ^uint32(0), ^uint32(0), &lock)
		_ = file.Close()
		intentJournalProcessLock.Unlock()
	}, nil
}

func openWindowsIntentJournalLock(root *os.Root) (*os.File, error) {
	info, err := root.Lstat(intentJournalLockFilename)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := root.OpenFile(intentJournalLockFilename, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr == nil {
			info, err = file.Stat()
			if closeErr := file.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		} else if errors.Is(createErr, os.ErrExist) {
			info, err = root.Lstat(intentJournalLockFilename)
		} else {
			return nil, operational("create behavior review journal lock", createErr)
		}
	}
	if err != nil {
		return nil, operational("inspect behavior review journal lock", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: behavior review journal lock is not a contained regular file", ErrInvalidInput)
	}
	file, err := root.OpenFile(intentJournalLockFilename, os.O_RDWR, 0)
	if err != nil {
		return nil, operational("open behavior review journal lock", err)
	}
	info, err = file.Stat()
	runtime.KeepAlive(file)
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err != nil {
			return nil, operational("inspect behavior review journal lock", err)
		}
		return nil, fmt.Errorf("%w: behavior review journal lock is not a regular file", ErrInvalidInput)
	}
	return file, nil
}
