package repository

import (
	"path/filepath"
	"strings"
)

func (repo Repository) IsDependencyInput(path string) bool {
	name := filepath.Base(path)
	if name == "pyproject.toml" {
		data, err := repo.Read(path)
		return err == nil && (strings.Contains(string(data), "[project]") || strings.Contains(string(data), "[dependency-groups]"))
	}
	if strings.HasPrefix(name, "requirements") && strings.HasSuffix(name, ".txt") {
		return true
	}
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".lock") || strings.Contains(lower, "-lock.") {
		return true
	}
	switch name {
	case "go.mod", "go.sum", "go.work", "package.json", "pnpm-lock.yaml", "bun.lockb", "uv.lock",
		"Cargo.toml", "Pipfile", "pom.xml", "build.gradle", "build.gradle.kts", "Gemfile", "composer.json", "Package.swift", "Package.resolved", "pubspec.yaml":
		return true
	default:
		return false
	}
}
