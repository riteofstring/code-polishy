//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package gaterun

import (
	"fmt"
	"runtime"
)

type artifactRootHandle struct{}

func makeArtifactRoot(string) (artifactRootHandle, error) {
	return artifactRootHandle{}, unsupportedArtifactPlatform()
}

func platformEnsureArtifactDirectory(artifactDirectory, bool) error {
	return unsupportedArtifactPlatform()
}

func platformCreateArtifactDirectory(artifactDirectory) error {
	return unsupportedArtifactPlatform()
}

func platformWriteArtifactAtomic(artifactFile, []byte) error {
	return unsupportedArtifactPlatform()
}

func platformReadArtifact(artifactFile, int) ([]byte, error) {
	return nil, unsupportedArtifactPlatform()
}

func unsupportedArtifactPlatform() error {
	return fmt.Errorf("%w: gate run artifacts are unsupported on %s", ErrOperational, runtime.GOOS)
}
