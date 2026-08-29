package runner

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type ResourceLease struct {
	files resourceLease
}

type resourceLease interface {
	release() error
}

type resourceWaitObserverKey struct{}

type ResourceWaitObserver func(time.Duration)

func WithResourceWaitObserver(ctx context.Context, observer ResourceWaitObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, resourceWaitObserverKey{}, observer)
}

func reportResourceWait(ctx context.Context, elapsed time.Duration) {
	observer, _ := ctx.Value(resourceWaitObserverKey{}).(ResourceWaitObserver)
	if observer != nil {
		observer(elapsed)
	}
}

func (lease *ResourceLease) Release() error {
	if lease == nil || lease.files == nil {
		return nil
	}
	err := lease.files.release()
	lease.files = nil
	return err
}

func AcquireExclusiveResources(ctx context.Context, resources []string) (*ResourceLease, time.Duration, error) {
	if err := policy.ValidateExclusiveResources(resources); err != nil {
		return nil, 0, err
	}
	if len(resources) == 0 {
		return &ResourceLease{}, 0, nil
	}
	ordered := append([]string{}, resources...)
	if !slices.IsSorted(ordered) {
		return nil, 0, fmt.Errorf("exclusive resources are not ordered")
	}
	started := time.Now()
	files, err := acquireResourceFiles(ctx, ordered)
	wait := time.Since(started)
	if err != nil {
		return nil, wait, err
	}
	return &ResourceLease{files: files}, wait, nil
}
