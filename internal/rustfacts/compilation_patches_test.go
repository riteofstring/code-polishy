package rustfacts

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustCompilationUsesReplacementConsumerAndPreservesDependencyTargets(t *testing.T) {
	t.Parallel()
	for _, selected := range []string{"Cargo.toml", "vendor/patch-source/src/lib.rs", "vendor/leaf/src/lib.rs"} {
		t.Run(selected, func(t *testing.T) {
			repo := cargoPatchRepository(t)
			manifest := string(readRustFile(t, repo, "Cargo.toml"))
			manifest = string(bytes.ReplaceAll([]byte(manifest), []byte("default=[]"), []byte("default=['desktop']")))
			write(t, repo.Root, "Cargo.toml", manifest)
			write(t, repo.Root, "src/lib.rs", "pub fn value() -> u32 { patch_source::value() }\n")
			write(t, repo.Root, "vendor/patch-source/src/lib.rs", "pub fn value() -> u32 { patch_leaf::value() }\n")
			write(t, repo.Root, "vendor/patch-source/build.rs", "fn main() {}\n")
			write(t, repo.Root, "vendor/patch-source/tests/unconsumed.rs", "compile_error!(\"dependency development targets must not compile\");\n")
			write(t, repo.Root, "vendor/leaf/src/lib.rs", "pub fn value() -> u32 { 42 }\n")
			before := readRustFile(t, repo, "Cargo.lock")
			plan, discovery := compilationDiscovery(t, repo, selected)
			if findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{}); len(findings) != 0 {
				t.Fatalf("consumer compilation failed: %+v", findings)
			}
			if !bytes.Equal(before, readRustFile(t, repo, "Cargo.lock")) {
				t.Fatal("compilation changed the consumer lock")
			}
			for _, path := range []string{"vendor/patch-source/Cargo.lock", "vendor/leaf/Cargo.lock"} {
				if _, err := os.Stat(filepath.Join(repo.Root, filepath.FromSlash(path))); !os.IsNotExist(err) {
					t.Fatalf("compilation created a replacement lock at %s: %v", path, err)
				}
			}
			write(t, repo.Root, "vendor/leaf/src/lib.rs", "pub fn value() -> u32 { \"wrong\" }\n")
			findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{})
			if !slices.ContainsFunc(findings, func(f policy.Finding) bool {
				return f.Check == "quality.rustCompiler" && f.Subject == "E0308" && f.Path == "vendor/leaf/src/lib.rs"
			}) {
				t.Fatalf("consumer failed to check the replacement source: %+v", findings)
			}
		})
	}
}
