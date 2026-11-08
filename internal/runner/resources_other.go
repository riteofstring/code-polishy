//go:build !unix && !windows

package runner

import (
	"context"
	"errors"
	"os"
)

type resourceFiles struct{}

func acquireResourceFiles(context.Context, []string) (resourceLease, error) {
	return nil, errors.New("exclusive resource locking requires a Unix host")
}

func (resourceFiles) release() error { return nil }

func lockOwnedResourceFile(context.Context, *os.File, bool, bool) (resourceLease, error) {
	return nil, errors.New("owned resource locking is unsupported on this host")
}
