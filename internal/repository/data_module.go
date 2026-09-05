package repository

import (
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func (repo Repository) isDataModule(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".js" && extension != ".mjs" {
		return false
	}
	if repo.IsControlInput(path) || !policy.MatchesAny(path, repo.Config.Scope.Data) {
		return false
	}
	for _, rule := range repo.Config.Scope.Languages {
		if policy.MatchesAny(path, rule.Paths) {
			return false
		}
	}
	return true
}
