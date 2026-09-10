package behaviorreview

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/riteofstring/code-polishy/internal/repository"
)

const reportsDirectory = ".code-polishy-reports"

func cleanupArtifacts(ctx context.Context, repo repository.Repository) (CleanupResult, error) {
	ctx = reviewContext(ctx)
	result := CleanupResult{Path: artifactDirectory}
	if err := ctx.Err(); err != nil {
		return result, operational("clean behavior review artifacts", err)
	}
	root, err := os.OpenRoot(repo.Root)
	if err != nil {
		return result, operational("open repository root", err)
	}
	defer root.Close()
	reports, present, err := openCleanupDirectory(root, reportsDirectory)
	if err != nil || !present {
		return result, err
	}
	defer reports.Close()
	target, present, err := openCleanupDirectory(reports, "behavior-review")
	if err != nil || !present {
		return result, err
	}
	if err := target.Close(); err != nil {
		return result, operational("close behavior review cleanup path", err)
	}
	if err := reports.RemoveAll("behavior-review"); err != nil {
		return result, cleanup("remove behavior review artifacts", err)
	}
	result.Removed = true
	return result, nil
}

func openCleanupDirectory(root *os.Root, name string) (*os.Root, bool, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, operational("inspect behavior review cleanup path", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, fmt.Errorf("%w: behavior review cleanup path is not a contained directory", ErrInvalidInput)
	}
	directory, err := root.OpenRoot(name)
	if err != nil {
		return nil, false, operational("open behavior review cleanup path", err)
	}
	return directory, true, nil
}
