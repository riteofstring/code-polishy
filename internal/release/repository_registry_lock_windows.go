//go:build windows

package release

import (
	"os"

	"golang.org/x/sys/windows"
)

func waitForFileLock(file *os.File) error {
	lock := windows.Overlapped{}
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &lock)
}
