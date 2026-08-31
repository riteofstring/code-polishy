//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package behaviorreview

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"

	"golang.org/x/sys/unix"
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
	parent, name, err := artifactParent(directory, intentJournalLockFilename)
	if err != nil {
		return nil, journalLockError(err)
	}
	defer parent.Close()
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	runtime.KeepAlive(parent)
	if err != nil {
		return nil, journalLockError(err)
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return nil, operational("open behavior review journal lock", errors.New("create journal lock file"))
	}
	var info unix.Stat_t
	if err := unix.Fstat(fd, &info); err != nil || uint32(info.Mode)&uint32(unix.S_IFMT) != uint32(unix.S_IFREG) {
		_ = file.Close()
		if err != nil {
			return nil, journalLockError(err)
		}
		return nil, fmt.Errorf("%w: behavior review journal lock is not a regular file", ErrInvalidInput)
	}
	if err := unix.Flock(fd, unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, operational("lock behavior review journal", err)
	}
	success = true
	return func() {
		_ = unix.Flock(fd, unix.LOCK_UN)
		_ = file.Close()
		intentJournalProcessLock.Unlock()
	}, nil
}

func journalLockError(err error) error {
	if errors.Is(err, errUnsafeArtifactPath) || errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENOTDIR) {
		return fmt.Errorf("%w: behavior review journal lock is not a contained regular file", ErrInvalidInput)
	}
	return operational("open behavior review journal lock", err)
}
