//go:build !windows

package engine

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockPackMigrationFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
