//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package behaviorreview

import (
	"errors"
	"fmt"
	"os"
	"sync"
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
	info, err := fallbackIntentJournalLockInfo(root)
	if err != nil {
		return nil, err
	}
	if !validFallbackIntentJournalLock(info) {
		return nil, fmt.Errorf("%w: behavior review journal lock is not a contained regular file", ErrInvalidInput)
	}
	success = true
	return func() { intentJournalProcessLock.Unlock() }, nil
}

func fallbackIntentJournalLockInfo(root *os.Root) (os.FileInfo, error) {
	info, err := root.Lstat(intentJournalLockFilename)
	if err == nil {
		return info, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, operational("inspect behavior review journal lock", err)
	}
	return createFallbackIntentJournalLock(root)
}

func createFallbackIntentJournalLock(root *os.Root) (os.FileInfo, error) {
	file, err := root.OpenFile(intentJournalLockFilename, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		info, inspectErr := root.Lstat(intentJournalLockFilename)
		if inspectErr != nil {
			return nil, operational("inspect behavior review journal lock", inspectErr)
		}
		return info, nil
	}
	if err != nil {
		return nil, operational("open behavior review journal lock", err)
	}
	return statAndCloseFallbackIntentJournalLock(file)
}

func statAndCloseFallbackIntentJournalLock(file *os.File) (os.FileInfo, error) {
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

func validFallbackIntentJournalLock(info os.FileInfo) bool {
	return info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0
}
