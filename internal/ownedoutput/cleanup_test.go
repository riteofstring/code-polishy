package ownedoutput

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func cacheFixture(t *testing.T, key string) Resource {
	t.Helper()
	return Resource{Use: policy.OwnedResource{Root: t.TempDir(), Key: key}, Path: ".tools/" + key}
}

func cacheFile(t *testing.T, resource Resource, path, value string) {
	t.Helper()
	path = filepath.Join(resource.Use.Root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireLinuxDisposal(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("whole-tree disposal is enabled only with Linux mount guards")
	}
}

func TestCleanupAbsentOutputDoesNotCreateControlStorage(t *testing.T) {
	resource := cacheFixture(t, "fixture-cache")
	for _, preview := range []bool{true, false} {
		entries, err := Clean(t.Context(), resource, preview)
		if err != nil || len(entries) != 0 {
			t.Fatalf("absent output: %+v %v", entries, err)
		}
	}
	entries, err := os.ReadDir(resource.Use.Root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("no-work created storage: %v %v", entries, err)
	}
}

func TestCleanupReusesWholeTreeDisposalForDifferentOwners(t *testing.T) {
	requireLinuxDisposal(t)
	for _, key := range []string{"rust-target", "fixture-generator-output"} {
		t.Run(key, func(t *testing.T) {
			resource := cacheFixture(t, key)
			cacheFile(t, resource, resource.Path+"/nested/output", "generated")
			cacheFile(t, resource, ".tools/cargo-home/download", "offline dependency")
			cacheFile(t, resource, "target/manual", "unmanaged")
			outside := filepath.Join(t.TempDir(), "sentinel")
			if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(resource.Use.Root, resource.Path, "link")); err != nil {
				t.Fatal(err)
			}
			preview, err := Clean(t.Context(), resource, true)
			if err != nil || len(preview) != 1 || preview[0].Action != "would-remove" || preview[0].Bytes != 9 || !preview[0].SizeKnown {
				t.Fatalf("preview: %+v %v", preview, err)
			}
			if _, err := os.Stat(filepath.Join(resource.Use.Root, runner.OwnedResourcePath(key))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preview wrote controls: %v", err)
			}
			result, err := Clean(t.Context(), resource, false)
			if err != nil || len(result) != 1 || result[0].Action != "removed" || result[0].Bytes != 9 {
				t.Fatalf("removal: %+v %v", result, err)
			}
			if _, err := os.Stat(filepath.Join(resource.Use.Root, resource.Path)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("live tree remained: %v", err)
			}
			for _, path := range []string{outside, filepath.Join(resource.Use.Root, ".tools/cargo-home/download"), filepath.Join(resource.Use.Root, "target/manual")} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("unowned content changed: %s: %v", path, err)
				}
			}
		})
	}
}

func TestCleanupRetriesPendingOutputWhileLiveCacheIsBusy(t *testing.T) {
	requireLinuxDisposal(t)
	for _, live := range []bool{false, true} {
		resource := cacheFixture(t, "fixture-cache")
		pending := runner.OwnedResourcePath(resource.Use.Key) + "/removals/interrupted/cache"
		cacheFile(t, resource, pending+"/old", "old")
		if live {
			cacheFile(t, resource, resource.Path+"/fresh", "fresh")
		}
		lease, err := runner.AcquireOwnedResource(t.Context(), resource.Use, runner.SharedResourceUse)
		if err != nil {
			t.Fatal(err)
		}
		entries, err := Clean(t.Context(), resource, false)
		lease.Release()
		want := 1
		if live {
			want = 2
		}
		if err != nil || len(entries) != want || entries[0].Action != "removed" || entries[0].Bytes != 3 {
			t.Fatalf("pending retry: %+v %v", entries, err)
		}
		if live {
			if entries[1].Action != "busy" || entries[1].Reason != "active" {
				t.Fatalf("live was not kept busy: %+v", entries)
			}
			data, err := os.ReadFile(filepath.Join(resource.Use.Root, resource.Path, "fresh"))
			if err != nil || string(data) != "fresh" {
				t.Fatalf("replacement cache changed: %q %v", data, err)
			}
		}
	}
}

func TestCleanupRejectsLinkedRootAndParents(t *testing.T) {
	requireLinuxDisposal(t)
	for _, relative := range []string{".tools", ".tools/fixture-cache", ".tools/code-polishy-cleanup"} {
		resource := cacheFixture(t, "fixture-cache")
		outside := t.TempDir()
		if err := os.WriteFile(filepath.Join(outside, "sentinel"), []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(resource.Use.Root, relative)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(resource.Use.Root, relative)); err != nil {
			t.Fatal(err)
		}
		entries, err := Clean(t.Context(), resource, false)
		if err != nil || len(entries) == 0 || entries[0].Action != "kept" {
			t.Fatalf("unsafe path %s: %+v %v", relative, entries, err)
		}
		if data, err := os.ReadFile(filepath.Join(outside, "sentinel")); err != nil || string(data) != "keep" {
			t.Fatalf("outside changed: %q %v", data, err)
		}
	}
}

func TestCleanupOnUnsupportedPlatformPreservesOutput(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("unsupported-host contract")
	}
	resource := cacheFixture(t, "fixture-cache")
	cacheFile(t, resource, resource.Path+"/output", "keep")
	entries, err := Clean(t.Context(), resource, false)
	if err != nil || len(entries) != 1 || entries[0].Action != "kept" || entries[0].Reason != "unsupported-layout" {
		t.Fatalf("unsupported result: %+v %v", entries, err)
	}
	if data, err := os.ReadFile(filepath.Join(resource.Use.Root, resource.Path, "output")); err != nil || string(data) != "keep" {
		t.Fatalf("unsupported cleanup mutated output: %q %v", data, err)
	}
}
