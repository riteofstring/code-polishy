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
	info, err := windowsIntentJournalLockInfo(root)
	if err != nil {
		return nil, err
	}
	if !validWindowsIntentJournalLock(info) {
		return nil, fmt.Errorf("%w: behavior review journal lock is not a contained regular file", ErrInvalidInput)
	}
	file, err := root.OpenFile(intentJournalLockFilename, os.O_RDWR, 0)
	if err != nil {
		return nil, operational("open behavior review journal lock", err)
	}
	if err := validateOpenedWindowsIntentJournalLock(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func windowsIntentJournalLockInfo(root *os.Root) (os.FileInfo, error) {
	info, err := root.Lstat(intentJournalLockFilename)
	if err == nil {
		return info, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, operational("inspect behavior review journal lock", err)
	}
	return createWindowsIntentJournalLock(root)
}

func createWindowsIntentJournalLock(root *os.Root) (os.FileInfo, error) {
	file, err := root.OpenFile(intentJournalLockFilename, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		return statAndCloseWindowsIntentJournalLock(file)
	}
	if errors.Is(err, os.ErrExist) {
		info, inspectErr := root.Lstat(intentJournalLockFilename)
		if inspectErr != nil {
			return nil, operational("inspect behavior review journal lock", inspectErr)
		}
		return info, nil
	}
	return nil, operational("create behavior review journal lock", err)
}

func statAndCloseWindowsIntentJournalLock(file *os.File) (os.FileInfo, error) {
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil {
		return nil, operational("inspect behavior review journal lock", statErr)
	}
	if closeErr != nil {
		return nil, operational("inspect behavior review journal lock", closeErr)
	}
	return info, nil
}

func validWindowsIntentJournalLock(info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}

func validateOpenedWindowsIntentJournalLock(file *os.File) error {
	info, err := file.Stat()
	runtime.KeepAlive(file)
	if err != nil {
		return operational("inspect behavior review journal lock", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: behavior review journal lock is not a regular file", ErrInvalidInput)
	}
	return nil
}
