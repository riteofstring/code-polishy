package rustfacts

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustCompilationUsesReplacementConsumerAndPreservesDependencyTargets(t *testing.T) {
	t.Parallel()
	for _, selected := range []string{"Cargo.toml", "vendor/patch-source/src/lib.rs", "vendor/leaf/src/lib.rs"} {
		t.Run(selected, func(t *testing.T) {
			repo := consumedReplacementRepository(t)
			write(t, repo.Root, "vendor/patch-source/src/lib.rs", "pub fn value() -> u32 { patch_leaf::value() }\n")
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

func TestRustCompilationLintsGovernedReplacementsAndCapsNonLocalDependencies(t *testing.T) {
	t.Parallel()
	repo := consumedReplacementRepository(t)
	remote := t.TempDir()
	write(t, remote, "Cargo.toml", "[package]\nname='remote'\nversion='1.0.0'\nedition='2021'\n")
	write(t, remote, "src/lib.rs", "pub fn value() -> u32 { if needless(true) { 1 } else { 2 } }\nfn needless(value: bool) -> bool { if value { true } else { false } }\nfn remote_unused() {}\n")
	for _, arguments := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture"}} {
		if output, err := exec.Command("git", append([]string{"-C", remote}, arguments...)...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v; %s", err, output)
		}
	}
	manifest := string(readRustFile(t, repo, "Cargo.toml"))
	write(t, repo.Root, "Cargo.toml", string(bytes.Replace([]byte(manifest), []byte("[patch.crates-io]"), []byte("remote={git='file://"+filepath.ToSlash(remote)+"'}\n[patch.crates-io]"), 1)))
	write(t, repo.Root, "src/lib.rs", "pub fn value() -> u32 { patch_source::value() + remote::value() }\n")
	write(t, repo.Root, "vendor/patch-source/src/lib.rs", "pub fn value() -> u32 { if needless(true) { patch_leaf::value() } else { 2 } }\nfn needless(value: bool) -> bool { if value { true } else { false } }\n#[allow(dead_code)]\nfn source_unused() {}\n")
	write(t, repo.Root, "vendor/leaf/src/lib.rs", "macro_rules! quiet { ($i:item) => { #[allow(dead_code, clippy::needless_bool)] $i }; }\nquiet! { fn leaf_needless(value: bool) -> bool { if value { true } else { false } } }\npub fn value() -> u32 { 42 }\n")
	lock := exec.Command(repo.RustTool("cargo"), "generate-lockfile", "--manifest-path", filepath.Join(repo.Root, "Cargo.toml"))
	lock.Env = append(os.Environ(), "CARGO_HOME="+CargoHome(repo), "RUSTC="+repo.RustTool("rustc"))
	if output, err := lock.CombinedOutput(); err != nil {
		t.Fatalf("fixture lock: %v; %s", err, output)
	}
	plan, discovery := compilationDiscovery(t, repo, "vendor/patch-source/src/lib.rs")
	reported := [][3]string{}
	for _, finding := range Compile(t.Context(), repo, plan, discovery, runner.OSRunner{}) {
		reported = append(reported, [3]string{finding.Check, finding.Subject, finding.Path})
	}
	slices.SortFunc(reported, func(left, right [3]string) int { return slices.Compare(left[:], right[:]) })
	reported = slices.Compact(reported)
	want := [][3]string{
		{"quality.clippy", "clippy::needless_bool", "vendor/leaf/src/lib.rs"},
		{"quality.clippy", "clippy::needless_bool", "vendor/patch-source/src/lib.rs"},
		{"quality.rustCompiler", "dead_code", "vendor/leaf/src/lib.rs"},
		{"quality.rustCompiler", "dead_code", "vendor/patch-source/src/lib.rs"},
	}
	if !slices.Equal(reported, want) {
		t.Fatalf("replacement lint findings = %v, want %v", reported, want)
	}
}

func consumedReplacementRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := cargoPatchRepository(t)
	manifest := string(readRustFile(t, repo, "Cargo.toml"))
	write(t, repo.Root, "Cargo.toml", string(bytes.ReplaceAll([]byte(manifest), []byte("default=[]"), []byte("default=['desktop']"))))
	write(t, repo.Root, "src/lib.rs", "pub fn value() -> u32 { patch_source::value() }\n")
	write(t, repo.Root, "vendor/patch-source/build.rs", "fn main() {}\n")
	write(t, repo.Root, "vendor/patch-source/tests/unconsumed.rs", "compile_error!(\"dependency development targets must not compile\");\n")
	return repo
}
