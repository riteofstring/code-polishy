//go:build !windows

package release

import "os"

func replaceLockFile(source, destination string) error {
	return os.Rename(source, destination)
}
