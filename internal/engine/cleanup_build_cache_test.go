package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestBuildCacheCatalogHasDisjointRootsAndControlStorage(t *testing.T) {
	root := t.TempDir()
	keys := map[string]bool{}
	paths := []string{}
	for _, resource := range buildCacheResources(root) {
		if resource.Use.Root != root || keys[resource.Use.Key] {
			t.Fatalf("invalid resource identity: %+v", resource)
		}
		keys[resource.Use.Key] = true
		paths = append(paths, resource.Path, runner.OwnedResourcePath(resource.Use.Key))
	}
	for index, path := range paths {
		if !filepath.IsLocal(path) || filepath.ToSlash(filepath.Clean(path)) != path {
			t.Fatalf("uncontained root: %s", path)
		}
		for otherIndex, other := range paths {
			if index != otherIndex && (path == other || strings.HasPrefix(path, other+"/")) {
				t.Fatalf("overlapping resources: %s %s", path, other)
			}
		}
	}
}

func TestBuildCacheCleanupUsesOwnedOutputWithoutLanguageInventory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux disposal")
	}
	root := t.TempDir()
	writeEngineFile(t, root, ".tools/rust-target/debug/old", "object", 0o600)
	writeEngineFile(t, root, ".tools/cargo-home/registry/source", "offline", 0o600)
	result, err := CleanupRepository(root, CleanupOptions{BuildCache: true})
	if err != nil || len(result.BuildCaches) != 1 || result.BuildCaches[0].Action != "removed" || result.ReclaimedBytes() != 6 {
		t.Fatalf("removed-language output: %+v %v", result, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, ".tools/cargo-home/registry/source")); err != nil || string(data) != "offline" {
		t.Fatalf("dependency cache changed: %q %v", data, err)
	}
}

func TestBuildCacheScopePreservesUnmanagedTargetAndContinuesAfterReportFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux disposal")
	}
	root := t.TempDir()
	writeEngineFile(t, root, "Cargo.toml", "manifest", 0o600)
	writeEngineFile(t, root, "target/debug/manual", "keep", 0o600)
	writeEngineFile(t, root, ".tools/rust-target/debug/owned", "remove", 0o600)
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".code-polishy-reports")); err != nil {
		t.Fatal(err)
	}
	result, err := CleanupRepository(root, CleanupOptions{BuildCache: true, Reports: true})
	if err == nil || len(result.BuildCaches) != 2 || result.BuildCaches[0].Action != "removed" || result.BuildCaches[1].Reason != "unmanaged" {
		t.Fatalf("independent scope continuation: %+v %v", result, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "target/debug/manual")); err != nil || string(data) != "keep" {
		t.Fatalf("manual target changed: %q %v", data, err)
	}
}
