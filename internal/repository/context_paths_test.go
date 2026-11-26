package repository

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestContextPlannedPathsPreserveStrictExistingSelectionAndContainment(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	repo.Config.Scope.Exclude = []string{"private/**"}
	writeFile(t, repo.Root, "src/app.go", "package app\n")
	if err := os.Symlink(t.TempDir(), filepath.Join(repo.Root, "linked")); err != nil {
		t.Fatal(err)
	}
	for _, planned := range [][]string{
		{"../escape.go"}, {"linked/new.go"}, {"private/new.go"}, {"src/app.go/child.go"},
		{"src/app.go"}, {"."}, {"new.go", "./new.go"}, {"bad\\name.go"},
	} {
		if _, err := repo.ContextPathSelection(nil, planned); err == nil {
			t.Fatalf("accepted planned paths %v", planned)
		}
	}
	if _, err := repo.ContextPathSelection([]string{"src/missing.go"}, []string{"src/new.go"}); err == nil || !strings.Contains(err.Error(), "--new-path") {
		t.Fatalf("strict missing existing selection = %v", err)
	}
	selection, err := repo.ContextPathSelection([]string{"src/app.go"}, []string{"next/sub/new.go", "src/new.go"})
	if err != nil || !slices.Equal(selection.Requested.Expanded, []string{"src/app.go"}) || len(selection.Files) != 3 || len(selection.Requested.Planned) != 2 {
		t.Fatalf("mixed context selection = %+v, %v", selection, err)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, "next")); !os.IsNotExist(err) {
		t.Fatalf("planned context created a directory: %v", err)
	}
}

func TestPlannedMappedSourceResolvesRationaleWithoutPretendingFileExists(t *testing.T) {
	t.Parallel()
	repo := newGitRepository(t)
	writeFile(t, repo.Root, "docs/design/app.md", "# Application design\n")
	repo.Config.Modules = []policy.Module{{Name: "app", Paths: []string{"src/**"}}}
	repo.Config.Documentation.Design = []policy.DesignDocument{{Path: "docs/design/app.md", SourcePaths: []string{"src/new.go"}}}
	selection, err := repo.ContextPathSelection(nil, []string{"src/new.go"})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := repo.ResolveDesignContext(selection.Files, nil)
	if err != nil || !slices.Equal(resolution.DocumentPaths(), []string{"docs/design/app.md"}) {
		t.Fatalf("prospective rationale = %+v, %v", resolution, err)
	}
	if findings := repo.SelectedDesignDocumentFindings(resolution.DocumentPaths(), selection.Requested.Planned...); len(findings) != 0 {
		t.Fatalf("planned mapping rejected: %+v", findings)
	}
	if findings := repo.DesignDocumentFindings(); len(findings) == 0 {
		t.Fatal("ordinary policy verification accepted the missing source")
	}
}
