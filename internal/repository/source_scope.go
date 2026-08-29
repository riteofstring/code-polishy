package repository

import (
	"os"
	"path/filepath"
	"strings"
)

func (repo Repository) SourceCommentLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".css":
		return "css"
	case ".htm", ".html":
		return "html"
	case ".ps1", ".psd1", ".psm1":
		return "powershell"
	}
	return repo.Language(path)
}

func (repo Repository) IsSourceCommentSource(path string) bool {
	return repo.SourceCommentLanguage(path) != ""
}

func (repo Repository) IsRegularFile(path string) bool {
	resolved, err := repo.Resolve(path)
	if err != nil {
		return false
	}
	info, err := os.Stat(resolved)
	return err == nil && info.Mode().IsRegular()
}
