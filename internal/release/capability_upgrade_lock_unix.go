//go:build !windows

package release

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockCapabilityUpgradeFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
