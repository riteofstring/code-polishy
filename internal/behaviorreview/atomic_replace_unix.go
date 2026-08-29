//go:build !windows

package behaviorreview

import "os"

func replaceAtomicFile(source, destination string) error {
	return os.Rename(source, destination)
}
