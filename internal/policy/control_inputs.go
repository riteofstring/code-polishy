package policy

import "strings"

var sensitiveControlPatterns = []string{
	".code-polishy.json", ".code-polishy.lock.json",
	".github/workflows/**", ".gitlab-ci.yaml", ".gitlab-ci.yml",
	".github/actionlint.yaml", ".github/actionlint.yml",
	"**/Dockerfile*", "**/Containerfile*",
	"**/go.mod", "**/go.sum", "**/go.work", "**/go.work.sum",
	"**/package.json", "**/package-lock.json", "**/npm-shrinkwrap.json", "**/pnpm-lock.yaml", "**/pnpm-workspace.yaml", "**/pnpm-workspace.yml", "**/bun.lockb",
	"**/pyproject.toml", "**/requirements*.txt", "**/uv.lock", "**/Pipfile",
	"**/Cargo.toml", "**/pom.xml", "**/build.gradle", "**/build.gradle.kts", "**/Gemfile", "**/composer.json", "**/Package.swift", "**/Package.resolved", "**/pubspec.yaml",
	"**/*.lock", "**/*-lock.*",
	"**/eslint.config.*", "**/.eslintrc*", "**/knip.*", "**/.knip.*", "**/prettier.config.*", "**/.prettierrc*", "**/tsconfig*.json",
	"**/ruff.toml", "**/.ruff.toml", "**/ty.toml", "**/osv-scanner.toml",
}

func IsSensitiveControlInput(path string) bool {
	path = strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")
	return MatchesAny(path, sensitiveControlPatterns)
}
