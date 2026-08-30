//go:build !windows

package gaterun

import "os"

func replaceAtomicFile(source, destination string) error {
	return os.Rename(source, destination)
}
