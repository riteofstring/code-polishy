package testing

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestChangedTestPathsFiltersAndOrdersAddedOrModifiedTestPaths(t *testing.T) {
	t.Parallel()
	candidate := repository.CandidateDelta{AddedOrModified: []string{
		"web/widget.spec.ts",
		"internal/testing/reminder_test.go",
		"cmd/code-polishy/main.go",
		"tests/acceptance/reminder.feature",
		"web/widget.spec.ts",
		"__tests__/entry.js",
	}}
	paths := ChangedTestPaths(repository.Repository{}, candidate)
	want := []string{
		"__tests__/entry.js",
		"internal/testing/reminder_test.go",
		"tests/acceptance/reminder.feature",
		"web/widget.spec.ts",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
}

func TestChangedTestPathsIgnoresDeletedOnlyAndProductionCandidates(t *testing.T) {
	t.Parallel()
	for name, candidate := range map[string]repository.CandidateDelta{
		"deleted test only": {Deleted: []string{"internal/testing/reminder_test.go"}},
		"production only":   {AddedOrModified: []string{"cmd/code-polishy/main.go"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if paths := ChangedTestPaths(repository.Repository{}, candidate); len(paths) != 0 {
				t.Fatalf("paths = %v", paths)
			}
		})
	}
}
