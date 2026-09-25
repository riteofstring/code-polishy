package rustfacts

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustCompilationChecksRealCrateAndKeepsLockUnchanged(t *testing.T) {
	t.Parallel()
	repo := compilationRepository(t, "pub fn answer() -> u32 { 42 }\n")
	before := readRustFile(t, repo, "Cargo.lock")
	plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
	if findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{}); len(findings) != 0 {
		t.Fatalf("valid crate failed: %+v", findings)
	}
	if after := readRustFile(t, repo, "Cargo.lock"); !bytes.Equal(before, after) {
		t.Fatal("compiler changed the reviewed lockfile")
	}
	if _, err := os.Stat(filepath.Join(TargetDirectory(repo), HostTarget(), "debug", "deps")); err != nil {
		t.Fatalf("compilation did not use its governed native target: %v", err)
	}
}

func TestRustCompilationReportsOriginalTypeAndLintLocations(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ source, check, subject string }{
		"type":              {"pub fn answer() -> u32 {\n    \"wrong\"\n}\n", "quality.rustCompiler", "E0308"},
		"clippy":            {"pub fn answer(value: bool) -> bool {\n    if value { true } else { false }\n}\n", "quality.clippy", "clippy::needless_bool"},
		"private dead code": {"fn unused() {}\npub fn answer() {}\n", "quality.rustCompiler", "dead_code"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := compilationRepository(t, test.source)
			plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
			findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{})
			if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
				return finding.Check == test.check && finding.Subject == test.subject && finding.Path == "src/lib.rs" && finding.Line > 0 && finding.Column > 0 && finding.Fields["target"] == "app"
			}) {
				t.Fatalf("missing original compiler diagnostic: %+v", findings)
			}
		})
	}
}

func TestRustCompilationCollectsLintsDespiteSourceLevels(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ source, subject string }{
		"allow clippy":  {"#[allow(clippy::needless_bool)]\npub fn answer(value: bool) -> bool { if value { true } else { false } }\n", "clippy::needless_bool"},
		"expect clippy": {"#[expect(clippy::needless_bool)]\npub fn answer(value: bool) -> bool { if value { true } else { false } }\n", "clippy::needless_bool"},
		"deny clippy":   {"#[deny(clippy::needless_bool)]\npub fn answer(value: bool) -> bool { if value { true } else { false } }\n", "clippy::needless_bool"},
		"forbid clippy": {"#[forbid(clippy::needless_bool)]\npub fn answer(value: bool) -> bool { if value { true } else { false } }\n", "clippy::needless_bool"},
		"allow unused":  {"#[allow(dead_code)]\nfn unused() {}\npub fn answer() {}\n", "dead_code"},
		"expect unused": {"#[expect(dead_code)]\nfn unused() {}\npub fn answer() {}\n", "dead_code"},
		"allow warning": {"#[allow(unreachable_code)]\npub fn answer() { return; println!(\"unreachable\"); }\n", "unreachable_code"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := compilationRepository(t, test.source)
			plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
			result, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, plan.Commands[0])
			if err != nil || result.ExitStatus != 0 {
				t.Fatalf("lint collection failed compilation: %+v, %v; %s", result, err, output.Stderr)
			}
			report, err := ParseCompilation(repo, "Cargo.toml", discovery.Workspaces[0], output.Stdout)
			if err != nil || !report.Success {
				t.Fatalf("lint report lacks successful compilation evidence: %+v, %v", report, err)
			}
			if !slices.ContainsFunc(report.Findings, func(finding policy.Finding) bool {
				return finding.Subject == test.subject && finding.Path == "src/lib.rs" && finding.Line > 0
			}) {
				t.Fatalf("source levels hid the governed lint: %+v", report.Findings)
			}
		})
	}
}

func TestRustCompilationFailureCannotBecomeAnExceptableLint(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct{ source, build, subject string }{
		"type error":   {source: "pub fn answer() -> u32 { \"wrong\" }\n", subject: "E0308"},
		"denied lint":  {source: "pub fn answer() { let mutex = std::sync::Mutex::new(0); let _ = mutex.lock().unwrap(); }\n", subject: "let_underscore_lock"},
		"build script": {source: "pub fn answer() {}\n", build: "fn main() { let unused = 1; panic!(\"deliberate build failure\"); }\n", subject: "unused_variables"},
	} {
		t.Run(name, func(t *testing.T) {
			repo := compilationRepository(t, test.source)
			if test.build != "" {
				write(t, repo.Root, "build.rs", test.build)
			}
			plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
			findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{})
			for _, expected := range []struct{ check, subject string }{{"quality.rustCompiler", test.subject}, {"policy.rustProject", "compilation"}} {
				if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
					return finding.Check == expected.check && finding.Subject == expected.subject
				}) {
					t.Errorf("failed compilation lost %s/%s: %+v", expected.check, expected.subject, findings)
				}
			}
		})
	}
}

func TestRustCompilationUsesWorkspaceRelativeSourcePaths(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "nested/Cargo.toml", "[workspace]\nmembers=['crates/service']\nresolver='3'\n")
	write(t, repo.Root, "nested/Cargo.lock", "version = 4\n[[package]]\nname = 'service'\nversion = '1.0.0'\n")
	write(t, repo.Root, "nested/crates/service/Cargo.toml", "[package]\nname='service'\nversion='1.0.0'\nedition='2024'\n")
	write(t, repo.Root, "nested/crates/service/src/lib.rs", "pub fn answer() -> u32 { \"bad\" }\n")
	write(t, repo.Root, "src/lib.rs", "this is not the selected Rust source\n")
	plan, discovery := compilationDiscovery(t, repo, "nested/crates/service/src/lib.rs")
	findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{})
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Subject == "E0308" && finding.Path == "nested/crates/service/src/lib.rs" && finding.Line == 1
	}) {
		t.Fatalf("nested workspace source was lost or misattributed: %+v", findings)
	}
}

func TestRustCompilationRequiresLockWithoutExecuting(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\n")
	write(t, repo.Root, "src/lib.rs", "pub fn app() {}\n")
	plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
	findings := Compile(t.Context(), repo, plan, discovery, noOutputRunner{})
	if len(findings) != 1 || findings[0].Path != "Cargo.lock" || findings[0].Subject != "lockfile" {
		t.Fatalf("missing lock diagnostic: %+v", findings)
	}
	if _, err := os.Stat(filepath.Join(repo.Root, "Cargo.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check generated a lockfile: %v", err)
	}
}

func TestRustCompilationRejectsLockDriftWithoutRepair(t *testing.T) {
	t.Parallel()
	repo := compilationRepository(t, "pub fn answer() {}\n")
	write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.1'\nedition='2024'\n")
	before := readRustFile(t, repo, "Cargo.lock")
	plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
	findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{})
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool { return strings.Contains(finding.Message, "lock file") }) {
		t.Fatalf("lock drift did not fail explicitly: %+v", findings)
	}
	if !bytes.Equal(before, readRustFile(t, repo, "Cargo.lock")) {
		t.Fatal("check repaired the lockfile")
	}
}

func TestRustCompilationInventoriesGeneratedBuildScriptFacts(t *testing.T) {
	t.Parallel()
	repo := compilationRepository(t, "pub fn answer() -> u32 { 42 }\n")
	write(t, repo.Root, "build.rs", "fn main() { println!(\"cargo::rustc-cfg=generated_build\"); println!(\"cargo::rustc-check-cfg=cfg(generated_build)\"); println!(\"cargo::rustc-env=GENERATED_VALUE=42\"); }\n")
	plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, plan.Commands[0])
	if err != nil {
		t.Fatalf("build script compilation: %v %s", err, output.Stderr)
	}
	report, err := ParseCompilation(repo, "Cargo.toml", discovery.Workspaces[0], output.Stdout)
	if err != nil || !report.Success || len(report.BuildScripts) != 1 {
		t.Fatalf("generated build evidence: %+v, %v", report, err)
	}
	build := report.BuildScripts[0]
	if !slices.Contains(build.Cfgs, "generated_build") || !slices.Contains(build.Environment, [2]string{"GENERATED_VALUE", "42"}) || !withinDirectory(TargetDirectory(repo), build.OutputDir) {
		t.Fatalf("incomplete generated build facts: %+v", build)
	}
}

func TestRustCompilationIgnoresAmbientCompilerOverrides(t *testing.T) {
	repo := compilationRepository(t, "pub fn answer() -> u32 { 42 }\n")
	for _, key := range []string{"CARGO", "RUSTC", "RUSTC_WRAPPER", "RUSTC_WORKSPACE_WRAPPER", "CLIPPY_ARGS", "CLIPPY_CONF_DIR", "RUSTFLAGS", "CARGO_ENCODED_RUSTFLAGS", "RUSTUP_TOOLCHAIN"} {
		t.Setenv(key, "unapproved-compiler-override")
	}
	plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
	if findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{}); len(findings) != 0 {
		t.Fatalf("ambient override affected governed compilation: %+v", findings)
	}
}

func TestRustCompilationCancellationIsNotDiagnosticSuccess(t *testing.T) {
	t.Parallel()
	repo := compilationRepository(t, "pub fn answer() -> u32 { 42 }\n")
	plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	findings := Compile(ctx, repo, plan, discovery, runner.OSRunner{})
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool { return strings.Contains(finding.Message, "canceled") }) {
		t.Fatalf("canceled compilation was not a failure: %+v", findings)
	}
}

func TestRustCompilationKeepsDefaultFeaturesAndSkipsGatedTargets(t *testing.T) {
	t.Parallel()
	repo := compilationRepository(t, "#[cfg(all(feature = \"first\", feature = \"second\"))]\ncompile_error!(\"mutually exclusive features\");\npub fn answer() -> u32 { 42 }\n")
	manifest := string(readRustFile(t, repo, "Cargo.toml"))
	write(t, repo.Root, "Cargo.toml", manifest+"[features]\ndefault=['first']\nfirst=[]\nsecond=[]\n[[bin]]\nname='gated'\npath='gated.rs'\nrequired-features=['second']\n")
	write(t, repo.Root, "gated.rs", "compile_error!(\"unselected target\");\nfn main() {}\n")
	plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
	_, output, err := (runner.OSRunner{}).RunStructured(t.Context(), repo.Root, plan.Commands[0])
	if err != nil {
		t.Fatalf("default configuration failed: %v %s", err, output.Stderr)
	}
	report, err := ParseCompilation(repo, "Cargo.toml", discovery.Workspaces[0], output.Stdout)
	if err != nil || !report.Success || len(report.Artifacts) == 0 {
		t.Fatalf("default feature evidence: %+v, %v", report, err)
	}
	for _, artifact := range report.Artifacts {
		if !slices.Contains(artifact.Features, "first") || slices.Contains(artifact.Features, "second") || artifact.Target.Name == "gated" {
			t.Errorf("compiled an unselected feature/target: %+v", artifact)
		}
	}
}

func TestRustCompilationReportsUnavailableNativeLinker(t *testing.T) {
	t.Parallel()
	repo := compilationRepository(t, "pub fn answer() {}\n")
	write(t, repo.Root, "build.rs", "fn main() {}\n")
	plan, discovery := compilationDiscovery(t, repo, "src/lib.rs")
	linkerKey := "CARGO_TARGET_" + strings.ToUpper(strings.ReplaceAll(HostTarget(), "-", "_")) + "_LINKER="
	plan.Commands[0].EnvironmentOverrides = append(plan.Commands[0].EnvironmentOverrides, linkerKey+filepath.Join(t.TempDir(), "missing-linker"))
	findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{})
	if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return strings.Contains(finding.Message, "linker") || strings.Contains(finding.Message, "link.exe")
	}) {
		t.Fatalf("missing SDK/linker did not fail explicitly: %+v", findings)
	}
}

func TestRustCompilationScopesMinimumVersionToSelectedPackages(t *testing.T) {
	t.Parallel()
	for _, selected := range []string{"Cargo.toml", "current/src/lib.rs"} {
		t.Run(selected, func(t *testing.T) {
			repo := rustRepository(t)
			write(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['current', 'future']\ndefault-members=['current']\nresolver='3'\n")
			write(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='current'\nversion='1.0.0'\n[[package]]\nname='future'\nversion='1.0.0'\n")
			write(t, repo.Root, "current/Cargo.toml", "[package]\nname='current'\nversion='1.0.0'\nedition='2024'\nrust-version='1.85'\n")
			write(t, repo.Root, "current/src/lib.rs", "pub fn current() {}\n")
			write(t, repo.Root, "future/Cargo.toml", "[package]\nname='future'\nversion='1.0.0'\nedition='2024'\nrust-version='1.99'\n")
			write(t, repo.Root, "future/src/lib.rs", "pub fn future() {}\n")
			plan, discovery := compilationDiscovery(t, repo, selected)
			if findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{}); len(findings) != 0 {
				t.Fatalf("unselected workspace MSRV blocked the selected build: %+v", findings)
			}
			plan, discovery = compilationDiscovery(t, repo, "future/src/lib.rs")
			findings := Compile(t.Context(), repo, plan, discovery, runner.OSRunner{})
			if !slices.ContainsFunc(findings, func(finding policy.Finding) bool { return strings.Contains(finding.Message, "rustc 1.99") }) {
				t.Fatalf("selected incompatible package did not fail: %+v", findings)
			}
		})
	}
}

func TestRustCompilationRejectsSettingsAndSplitProviderUnits(t *testing.T) {
	t.Parallel()
	for name, configure := range map[string]func(repository.Repository) repository.Repository{
		"project clippy settings": func(repo repository.Repository) repository.Repository {
			write(t, repo.Root, "clippy.toml", "too-many-arguments-threshold=1000\n")
			return repo
		},
		"foreign target": func(repo repository.Repository) repository.Repository {
			write(t, repo.Root, ".cargo/config.toml", "[build]\ntarget='wasm32-unknown-unknown'\n")
			return repo
		},
		"partial provider": func(repo repository.Repository) repository.Repository {
			repo.Config.Checks = []policy.Command{{Name: "custom-lint", Provides: []string{"lint"}, Paths: []string{"src/lib.rs"}, RunOn: []string{"check"}, Adapter: &policy.PackAdapter{PackName: "custom", Capability: "lint"}}}
			return repo
		},
	} {
		t.Run(name, func(t *testing.T) {
			repo := configure(compilationRepository(t, "pub fn app() {}\n"))
			files := []string{"Cargo.toml", "src/lib.rs"}
			plan := PlanCompilation(repo, PlanDiscovery(repo, files, files), files)
			if len(plan.Commands) != 0 || len(plan.Findings) != 1 {
				t.Fatalf("unsupported compilation scope reached execution: %+v", plan)
			}
		})
	}
}

func compilationRepository(t *testing.T, source string) repository.Repository {
	t.Helper()
	repo := rustRepository(t)
	write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n")
	write(t, repo.Root, "Cargo.lock", "version = 4\n[[package]]\nname = 'app'\nversion = '1.0.0'\n")
	write(t, repo.Root, "src/lib.rs", source)
	return repo
}

func compilationDiscovery(t *testing.T, repo repository.Repository, selected string) (CompilationPlan, Discovery) {
	t.Helper()
	files, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	discoveryPlan := PlanDiscovery(repo, []string{selected}, files)
	discovery := Discover(t.Context(), repo, discoveryPlan, runner.OSRunner{})
	plan := PlanCompilation(repo, discoveryPlan, files)
	if len(discovery.Findings) != 0 || len(plan.Findings) != 0 || len(plan.Commands) != 1 {
		t.Fatalf("compilation preparation: %+v, %+v", discovery, plan)
	}
	return plan, discovery
}

func readRustFile(t *testing.T, repo repository.Repository, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo.Root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
