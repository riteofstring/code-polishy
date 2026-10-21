//go:build !windows

package durablefile

import "os"

func Replace(source, destination string) error {
	return os.Rename(source, destination)
}
