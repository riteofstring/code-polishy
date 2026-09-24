package repository

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRustToolsJoinGovernedEnvironmentWhenInstalled(t *testing.T) {
	t.Parallel()
	repo := Repository{PolicyRoot: t.TempDir()}
	writeFile(t, repo.PolicyRoot, "tools/rust-version.txt", "1.97.1\n")
	if slices.ContainsFunc(repo.CommandEnvironment().Tools, func(tool GovernedTool) bool { return tool.Name == "cargo" }) {
		t.Fatal("absent Rust runtime entered the governed environment")
	}
	if err := os.MkdirAll(filepath.Dir(repo.RustTool("cargo")), 0o755); err != nil {
		t.Fatal(err)
	}
	environment := repo.CommandEnvironment()
	if !slices.Contains(environment.PathEntries, filepath.Dir(repo.RustTool("cargo"))) {
		t.Fatalf("Rust executable directory missing from environment: %+v", environment)
	}
	for _, name := range []string{"cargo", "rustc", "rustdoc", "rustfmt", "cargo-fmt", "cargo-clippy", "clippy-driver"} {
		if !slices.ContainsFunc(environment.Tools, func(tool GovernedTool) bool {
			return tool.Name == name && tool.Path == repo.RustTool(name) && tool.Version == "1.97.1"
		}) {
			t.Errorf("missing governed tool %s: %+v", name, environment.Tools)
		}
	}
}
