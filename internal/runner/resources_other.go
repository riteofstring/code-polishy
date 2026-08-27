//go:build !unix && !windows

package runner

import (
	"context"
	"errors"
)

type resourceFiles struct{}

func acquireResourceFiles(context.Context, []string) (resourceLease, error) {
	return nil, errors.New("exclusive resource locking requires a Unix host")
}

func (resourceFiles) release() error { return nil }
