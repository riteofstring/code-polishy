package repository

import (
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestDataModuleClassificationRetainsControlsAndCustomLanguages(t *testing.T) {
	repo := newGitRepository(t)
	repo.Config.Scope.Data = []string{"catalog.js", "eslint.config.mjs", "custom.js"}
	repo.Config.Scope.Languages = []policy.LanguageRule{{Name: "template", Paths: []string{"custom.js"}}}
	if !repo.IsData("catalog.js") || repo.IsExecutableSource("catalog.js") {
		t.Fatal("declared JSON module did not enter parse-only data validation")
	}
	for _, path := range []string{"eslint.config.mjs", "custom.js", "application.js"} {
		if repo.IsData(path) || !repo.IsExecutableSource(path) {
			t.Errorf("executable input %s was classified as data", path)
		}
	}
}
