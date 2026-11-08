package ownedoutput

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestMountGuardUsesComponentBoundariesAndRejectsIncompleteTopology(t *testing.T) {
	for _, test := range []struct {
		name, topology, path  string
		descendants, rejected bool
	}{
		{"nested bind", "1 0 8:1 /elsewhere /cache/nested rw - ext4 /dev/root rw\n", "/cache", true, true},
		{"escaped path", "1 0 8:1 / /cache\\040space/nested rw - ext4 /dev/root rw\n", "/cache space", true, true},
		{"sibling", "1 0 8:1 / /cache-old rw - ext4 /dev/root rw\n", "/cache", true, false},
		{"mounted root", "1 0 8:1 / /cache rw - ext4 /dev/root rw\n", "/cache", false, true},
		{"detach parent only", "1 0 8:1 / /cache/nested rw - ext4 /dev/root rw\n", "/cache", false, false},
		{"truncated", "1 0 8:1 / /cache rw\n", "/cache", true, true},
		{"empty", "", "/cache", true, true},
		{"mounted parent", "1 0 8:1 / /tools rw - ext4 /dev/root rw\n", "/tools/cache", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := inspectMounts(strings.NewReader(test.topology), "", test.path, test.descendants)
			if (err != nil) != test.rejected {
				t.Fatalf("guard=%v rejected=%t", err, test.rejected)
			}
		})
	}
}

func TestCleanupCrossDeviceStagingPreservesLiveCache(t *testing.T) {
	if os.Getenv("CODE_POLISHY_MOUNT_TESTS") != "1" {
		t.Skip("requires an explicitly selected private mount namespace")
	}
	resource := cacheFixture(t, "fixture-cache")
	cacheFile(t, resource, resource.Path+"/output", "keep")
	control := filepath.Join(resource.Use.Root, runner.OwnedResourcePath(resource.Use.Key))
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mount("tmpfs", control, "tmpfs", 0, "size=1m"); err != nil {
		t.Fatal(err)
	}
	defer syscall.Unmount(control, 0)
	entries, err := Clean(t.Context(), resource, false)
	if err != nil || len(entries) != 1 || entries[0].Action != "kept" {
		t.Fatalf("cross-device layout: %+v %v", entries, err)
	}
	if data, err := os.ReadFile(filepath.Join(resource.Use.Root, resource.Path, "output")); err != nil || string(data) != "keep" {
		t.Fatalf("live cache changed: %q %v", data, err)
	}
}

func TestCleanupMountSentinels(t *testing.T) {
	if os.Getenv("CODE_POLISHY_MOUNT_TESTS") != "1" {
		t.Skip("requires an explicitly selected private mount namespace")
	}
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "mounted-root", true: "nested-same-device-bind"}[nested], func(t *testing.T) {
			fixture := mountedCacheFixture(t, nested)
			resource := fixture.resource
			preview, err := Clean(t.Context(), resource, true)
			if err != nil || len(preview) != 1 || preview[0].Action != "kept" || preview[0].SizeKnown {
				t.Fatalf("unsafe preview measured mounted data: %+v %v", preview, err)
			}
			if _, err := os.Stat(filepath.Join(resource.Use.Root, ".tools/code-polishy-cleanup")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preview created controls: %v", err)
			}
			entries, err := Clean(t.Context(), resource, false)
			if err != nil || len(entries) != 1 || entries[0].Action != "kept" || entries[0].Bytes != 0 {
				t.Fatalf("unsafe removal: %+v %v", entries, err)
			}
			if nested {
				fixture.assertPendingRecovery(t, entries[0])
			}
			if data, err := os.ReadFile(filepath.Join(fixture.outside, "sentinel")); err != nil || string(data) != "external data" {
				t.Fatalf("mounted data changed: %q %v", data, err)
			}
		})
	}
}

func TestCleanupInterruptedDeletionRetainsRemainderAndFreshOutput(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary filesystem permissions")
	}
	resource := cacheFixture(t, "fixture-cache")
	cacheFile(t, resource, resource.Path+"/locked/output", "old")
	if err := os.Chmod(filepath.Join(resource.Use.Root, resource.Path, "locked"), 0o500); err != nil {
		t.Fatal(err)
	}
	entries, err := Clean(t.Context(), resource, false)
	if err == nil || len(entries) != 1 || entries[0].Action != "error" || entries[0].Bytes != 0 || entries[0].PendingPath == "" {
		t.Fatalf("partial deletion: %+v %v", entries, err)
	}
	pending := entries[0].PendingPath
	if err := os.Chmod(filepath.Join(resource.Use.Root, pending, "cache", "locked"), 0o700); err != nil {
		t.Fatal(err)
	}
	cacheFile(t, resource, resource.Path+"/fresh", "new")
	lease, err := runner.AcquireOwnedResource(t.Context(), resource.Use, runner.SharedResourceUse)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	entries, err = Clean(t.Context(), resource, false)
	if err != nil || len(entries) != 2 || entries[0].Action != "removed" || entries[1].Action != "busy" {
		t.Fatalf("partial retry: %+v %v", entries, err)
	}
	if data, err := os.ReadFile(filepath.Join(resource.Use.Root, resource.Path, "fresh")); err != nil || string(data) != "new" {
		t.Fatalf("fresh output changed: %q %v", data, err)
	}
}

type mountedCache struct {
	resource       Resource
	outside, mount string
}

func (fixture *mountedCache) assertPendingRecovery(t *testing.T, entry Entry) {
	t.Helper()
	if entry.PendingPath == "" || !strings.Contains(entry.Detail, "live cache detached") {
		t.Fatalf("missing detachment consequence: %+v", entry)
	}
	resource := fixture.resource
	fixture.mount = filepath.Join(resource.Use.Root, entry.PendingPath, "cache", "mounted space")
	cacheFile(t, resource, resource.Path+"/fresh", "fresh")
	if _, err := Clean(t.Context(), resource, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(resource.Use.Root, resource.Path, "fresh")); err != nil || string(data) != "fresh" {
		t.Fatalf("unsafe pending recovery touched fresh cache: %q %v", data, err)
	}
}

func mountedCacheFixture(t *testing.T, nested bool) *mountedCache {
	t.Helper()
	fixture := &mountedCache{resource: cacheFixture(t, "fixture-cache"), outside: t.TempDir()}
	cacheFile(t, fixture.resource, fixture.resource.Path+"/local", "generated")
	if err := os.WriteFile(filepath.Join(fixture.outside, "sentinel"), []byte("external data"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.mount = filepath.Join(fixture.resource.Use.Root, fixture.resource.Path)
	if nested {
		fixture.mount = filepath.Join(fixture.mount, "mounted space")
		if err := os.Mkdir(fixture.mount, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mount(fixture.outside, fixture.mount, "", syscall.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Unmount(fixture.mount, 0); err != nil {
			t.Error(err)
		}
	})
	return fixture
}
