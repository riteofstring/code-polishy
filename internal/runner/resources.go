package runner

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

// ResourceLease owns every advisory lock acquired for a command. It is safe
// to release more than once, which keeps callers' error paths small and makes
// partial acquisition failure harmless.
type ResourceLease struct {
	files resourceLease
}

type resourceLease interface {
	release() error
}

type resourceWaitObserverKey struct{}

// ResourceWaitObserver receives periodic elapsed durations while a command is
// blocked on an exclusive resource. It is intentionally carried by context so
// the common runner remains the single locking boundary while direct and
// managed execution can render terminal-safe progress.
type ResourceWaitObserver func(time.Duration)

// WithResourceWaitObserver attaches optional host-resource wait visibility to
// one command execution.
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

// AcquireExclusiveResources waits for the command's complete declared set.
// A configuration parsed by policy is already normalized, but this defensive
// boundary rejects malformed direct callers before a lock namespace is touched.
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
