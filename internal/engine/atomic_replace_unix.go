//go:build !windows

package engine

import "os"

func replaceAtomicFile(source, destination string) error {
	return os.Rename(source, destination)
}
