//go:build windows

package runner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWindowsResourceLockIsExclusive(t *testing.T) {
	first, err := acquireResourceFiles(context.Background(), []string{"windows-test-resource"})
	if err != nil {
		t.Fatal(err)
	}
	defer first.release()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := acquireResourceFiles(ctx, []string{"windows-test-resource"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
}
