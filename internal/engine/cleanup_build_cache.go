package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/ownedoutput"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func buildCacheResources(root string) []ownedoutput.Resource {
	repo := repository.Repository{Root: root}
	path, _ := filepath.Rel(root, rustfacts.TargetDirectory(repo))
	return []ownedoutput.Resource{{Use: rustfacts.TargetResource(repo), Path: filepath.ToSlash(path)}}
}

func cleanupBuildCaches(ctx context.Context, root string, dryRun bool) ([]ownedoutput.Entry, error) {
	var entries []ownedoutput.Entry
	var problem error
	for _, resource := range buildCacheResources(root) {
		if err := ctx.Err(); err != nil {
			return entries, errors.Join(problem, err)
		}
		result, err := ownedoutput.Clean(ctx, resource, dryRun)
		entries = append(entries, result...)
		problem = errors.Join(problem, err)
	}
	manifest, err := os.Lstat(filepath.Join(root, "Cargo.toml"))
	if err == nil && manifest.Mode().IsRegular() {
		if _, err := os.Lstat(filepath.Join(root, "target")); err == nil {
			entries = append(entries, ownedoutput.Entry{Key: "unmanaged-target", Path: "target", Action: "kept", Reason: "unmanaged", Detail: "ordinary Cargo output is outside Code Polishy's managed cache; use cargo clean to remove it"})
		}
	}
	return entries, problem
}
