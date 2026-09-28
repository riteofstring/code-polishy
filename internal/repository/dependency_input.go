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
	lower := strings.ToLower(name)
	if strings.Contains(lower, "requirements") && strings.HasSuffix(lower, ".txt") {
		return true
	}
	if dependencyInputSuffix(lower) {
		return true
	}
	if lower == "verification-metadata.xml" && filepath.Base(filepath.Dir(path)) == "gradle" {
		return true
	}
	switch name {
	case "go.mod", "go.sum", "go.work", "package.json", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "bun.lockb",
		"Cargo.toml", "Pipfile", "pylock.toml", "pom.xml", "build.gradle", "build.gradle.kts", "gradle.lockfile", "buildscript-gradle.lockfile",
		"Gemfile", "gems.locked", "composer.json", "Package.swift", "Package.resolved", "pubspec.yaml", "Directory.Packages.props", "Directory.Build.props",
		"packages.config", "packages.lock.json", "cabal.project.freeze", "osv-scanner-custom.json":
		return true
	default:
		return false
	}
}

func dependencyInputSuffix(name string) bool {
	if strings.HasSuffix(name, ".lock") || strings.Contains(name, "-lock.") {
		return true
	}
	if strings.HasPrefix(name, "pylock.") && strings.HasSuffix(name, ".toml") {
		return true
	}
	for _, suffix := range []string{".lockfile", ".pom", ".csproj", ".vbproj", ".fsproj", ".deps.json"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
