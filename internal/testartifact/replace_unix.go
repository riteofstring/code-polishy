//go:build !windows

package testartifact

import "os"

func replaceManifest(source, destination string) error {
	return os.Rename(source, destination)
}
