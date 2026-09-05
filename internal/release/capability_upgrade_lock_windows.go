//go:build windows

package release

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockCapabilityUpgradeFile(file *os.File) error {
	lock := windows.Overlapped{}
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock)
}
