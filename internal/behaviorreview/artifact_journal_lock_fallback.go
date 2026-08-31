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
	info, err := root.Lstat(intentJournalLockFilename)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := root.OpenFile(intentJournalLockFilename, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil && !errors.Is(createErr, os.ErrExist) {
			return nil, operational("open behavior review journal lock", createErr)
		}
		if createErr == nil {
			info, err = file.Stat()
			if closeErr := file.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
		} else {
			info, err = root.Lstat(intentJournalLockFilename)
		}
	}
	if err != nil {
		return nil, operational("inspect behavior review journal lock", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: behavior review journal lock is not a contained regular file", ErrInvalidInput)
	}
	success = true
	return func() { intentJournalProcessLock.Unlock() }, nil
}
