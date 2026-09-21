//go:build windows

package engine

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockPackMigrationFile(file *os.File) error {
	lock := windows.Overlapped{}
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock)
}
