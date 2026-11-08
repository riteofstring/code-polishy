package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestOwnedResourceConsumersOverlapAndCleanupDoesNotWait(t *testing.T) {
	resource := policy.OwnedResource{Root: t.TempDir(), Key: "fixture-cache"}
	first, err := AcquireOwnedResource(t.Context(), resource, SharedResourceUse)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	second, err := AcquireOwnedResource(ctx, resource, SharedResourceUse)
	if err != nil {
		t.Fatalf("shared consumers serialized: %v", err)
	}
	defer second.Release()
	if _, err := AcquireOwnedResource(ctx, resource, ExclusiveResourceUse); !errors.Is(err, ErrResourceBusy) {
		t.Fatalf("cleanup did not report busy: %v", err)
	}
	maintenance, err := AcquireOwnedResource(ctx, resource, ResourceMaintenance)
	if err != nil {
		t.Fatalf("pending deletion blocked on live consumers: %v", err)
	}
	defer maintenance.Release()
	if _, err := AcquireOwnedResource(ctx, resource, ResourceMaintenance); !errors.Is(err, ErrResourceBusy) {
		t.Fatalf("competing cleanup was not excluded: %v", err)
	}
	first.Release()
	second.Release()
	exclusive, err := AcquireOwnedResource(ctx, resource, ExclusiveResourceUse)
	if err != nil {
		t.Fatalf("released consumers still block cleanup: %v", err)
	}
	defer exclusive.Release()
	wait, stop := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer stop()
	if _, err := AcquireOwnedResource(wait, resource, SharedResourceUse); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("consumer ignored exclusive detach or cancellation: %v", err)
	}
}

func TestOwnedResourceRejectsLinkedControlPathsAndLockFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink fixture")
	}
	for _, path := range []string{".tools", ".tools/code-polishy-cleanup", ".tools/code-polishy-cleanup/cache", ".tools/code-polishy-cleanup/cache/live-use"} {
		t.Run(path, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			if filepath.Base(path) == "live-use" {
				outside = filepath.Join(outside, "sentinel")
				if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, path)); err != nil {
				t.Fatal(err)
			}
			if lease, err := AcquireOwnedResource(t.Context(), policy.OwnedResource{Root: root, Key: "cache"}, SharedResourceUse); err == nil {
				lease.Release()
				t.Fatal("linked control path accepted")
			}
		})
	}
}

func TestOwnedResourcesStayInternalAndUseOriginalCheckoutFromAView(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell process fixture")
	}
	owner, view := t.TempDir(), t.TempDir()
	resource := policy.OwnedResource{Root: owner, Key: "fixture-cache"}
	marker := filepath.Join(view, "started")
	if err := os.WriteFile(filepath.Join(view, "wait.sh"), []byte("#!/bin/sh\n: > started\nsleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := policy.Command{Name: "view", Argv: []string{"./wait.sh"}, Cwd: ".", TimeoutSeconds: 60, OwnedResources: []policy.OwnedResource{resource}}
	data, err := json.Marshal(command)
	if err != nil || strings.Contains(string(data), "fixture-cache") || strings.Contains(string(data), owner) {
		t.Fatalf("internal resource serialized: %s %v", data, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunCommand(ctx, OSRunner{}, view, command) }()
	waitForResourceMarker(t, marker)
	if lease, err := AcquireOwnedResource(t.Context(), resource, ExclusiveResourceUse); !errors.Is(err, ErrResourceBusy) {
		lease.Release()
		t.Fatalf("view execution failed to lease original cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(view, ".tools")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lease polluted sealed view: %v", err)
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancellation was not reported")
	}
	lease, err := AcquireOwnedResource(t.Context(), resource, ExclusiveResourceUse)
	if err != nil {
		t.Fatalf("lease not released after process-tree cancellation: %v", err)
	}
	lease.Release()
}
