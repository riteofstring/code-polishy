package repository

import "testing"

func TestDependencyInputsCoverPinnedOSVPackageSources(t *testing.T) {
	t.Parallel()
	repo := Repository{}
	for _, path := range []string{
		"gradle.lockfile",
		"buildscript-gradle.lockfile",
		"gradle/verification-metadata.xml",
		"artifact.pom",
		"npm-shrinkwrap.json",
		"pylock.toml",
		"pylock.dev.toml",
		"dev-requirements.txt",
		"gems.locked",
		"app.csproj",
		"app.vbproj",
		"app.fsproj",
		"app.deps.json",
		"Directory.Packages.props",
		"Directory.Build.props",
		"packages.config",
		"packages.lock.json",
		"cabal.project.freeze",
		"osv-scanner-custom.json",
	} {
		if !repo.IsDependencyInput(path) {
			t.Errorf("IsDependencyInput(%q) = false", path)
		}
	}
}

func TestDependencyInputDoesNotBroadenStructuredFileNames(t *testing.T) {
	t.Parallel()
	repo := Repository{}
	for _, path := range []string{
		"verification-metadata.xml",
		"gradle/verification-metadata.json",
		"pylock.toml.json",
		"application.json",
		"notes.txt",
	} {
		if repo.IsDependencyInput(path) {
			t.Errorf("IsDependencyInput(%q) = true", path)
		}
	}
}
