//go:build !windows

package release

import (
	"os"

	"golang.org/x/sys/unix"
)

func waitForFileLock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX)
}
