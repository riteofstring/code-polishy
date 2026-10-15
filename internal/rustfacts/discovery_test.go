package rustfacts

import (
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

func TestCargoDiscoveryReadsRealWorkspaceWithoutExecutingBuildScript(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['crates/*']\nexclude=['scratch']\ndefault-members=['crates/service']\nresolver='3'\n[workspace.package]\nversion='1.2.3'\nedition='2024'\nrust-version='1.85'\n")
	write(t, repo.Root, "crates/service/Cargo.toml", "[package]\nname='actual-service'\nversion.workspace=true\nedition.workspace=true\nrust-version.workspace=true\n[features]\ndefault=['server']\nserver=[]\n")
	write(t, repo.Root, "crates/service/src/lib.rs", "pub fn value() -> u32 { 42 }\n")
	write(t, repo.Root, "crates/service/build.rs", "fn main() { std::fs::write(\"executed\", \"bad\").unwrap(); panic!(\"must not run\"); }\n")
	write(t, repo.Root, "scratch/Cargo.toml", "[package\n")
	files, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	plan := PlanDiscovery(repo, []string{"crates/service/src/lib.rs"}, files)
	if len(plan.Commands) != 3 || len(plan.Findings) != 0 {
		t.Fatalf("discovery plan = %+v", plan)
	}
	result := Discover(t.Context(), repo, plan, runner.OSRunner{})
	if len(result.Findings) != 0 || len(result.Workspaces) != 1 {
		t.Fatalf("discovery = %+v", result)
	}
	workspace := result.Workspaces[0]
	if workspace.Root != "." || len(workspace.Packages) != 1 || len(workspace.DefaultMembers) != 1 {
		t.Fatalf("workspace = %+v", workspace)
	}
	pkg := workspace.Packages[0]
	if pkg.Name != "actual-service" || pkg.Version != "1.2.3" || pkg.Edition != "2024" || pkg.RustVersion != "1.85" || pkg.Manifest != "crates/service/Cargo.toml" {
		t.Fatalf("package = %+v", pkg)
	}
	if !slices.ContainsFunc(pkg.Targets, func(target Target) bool { return slices.Contains(target.Kinds, "custom-build") }) {
		t.Fatalf("build script target was not inventoried: %+v", pkg.Targets)
	}
	for _, path := range []string{"Cargo.lock", "crates/service/executed", "executed"} {
		if _, err := os.Stat(filepath.Join(repo.Root, path)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("discovery modified %s: %v", path, err)
		}
	}
}

func TestCargoDiscoveryRejectsAmbientOverridesAndAdmitsMissingDependencies(t *testing.T) {
	repo := rustRepository(t)
	write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='0.1.0'\nedition='2021'\n[dependencies]\nserde='=1.0.228'\n")
	write(t, repo.Root, "src/lib.rs", "pub fn value() -> u32 { 42 }\n")
	for _, key := range []string{"RUSTC", "RUSTC_WRAPPER", "RUSTC_WORKSPACE_WRAPPER", "RUSTUP_TOOLCHAIN", "CARGO_HOME", "CARGO_BUILD_RUSTC", "CARGO_TARGET_DIR"} {
		t.Setenv(key, filepath.Join(t.TempDir(), "not-a-tool"))
	}
	plan := PlanDiscovery(repo, []string{"Cargo.toml"}, []string{"Cargo.toml", "src/lib.rs"})
	result := Discover(t.Context(), repo, plan, runner.OSRunner{})
	if len(result.Findings) != 0 || len(result.Workspaces) != 1 {
		t.Fatalf("sealed metadata discovery = %+v", result)
	}
	dependencies := result.Workspaces[0].Packages[0].Dependencies
	if len(dependencies) != 1 || dependencies[0].Name != "serde" || dependencies[0].Requirement != "=1.0.228" {
		t.Fatalf("declared dependency facts = %+v", dependencies)
	}
	if _, err := os.Stat(filepath.Join(CargoHome(repo), "registry")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("data-only discovery materialized the registry: %v", err)
	}
}

func TestCargoDiscoveryPreservesValidSiblingAfterManifestFailure(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "good/Cargo.toml", "[package]\nname='good'\nversion='1.0.0'\n")
	write(t, repo.Root, "good/src/lib.rs", "pub fn good() {}\n")
	write(t, repo.Root, "bad/Cargo.toml", "[package\n")
	plan := PlanDiscovery(repo, []string{"bad/Cargo.toml", "good/Cargo.toml"}, []string{"bad/Cargo.toml", "good/Cargo.toml"})
	result := Discover(t.Context(), repo, plan, runner.OSRunner{})
	if len(result.Findings) != 1 || result.Findings[0].Path != "bad/Cargo.toml" || len(result.Workspaces) != 1 || result.Workspaces[0].Packages[0].Name != "good" {
		t.Fatalf("partial discovery = %+v", result)
	}
}

func TestCargoDiscoveryRequiresPinnedToolsAndStructuredOutput(t *testing.T) {
	t.Parallel()
	repo := rustRepository(t)
	write(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='0.1.0'\n")
	write(t, repo.Root, "src/lib.rs", "pub fn app() {}\n")
	plan := PlanDiscovery(repo, []string{"Cargo.toml"}, []string{"Cargo.toml"})
	result := Discover(t.Context(), repo, plan, noOutputRunner{})
	if len(result.Findings) != 1 || !strings.Contains(result.Findings[0].Message, "bounded Cargo metadata") {
		t.Fatalf("unstructured discovery = %+v", result)
	}
	repo.PolicyRoot = t.TempDir()
	plan = PlanDiscovery(repo, []string{"Cargo.toml"}, []string{"Cargo.toml"})
	if len(plan.Commands) != 0 || len(plan.Findings) != 1 || plan.Findings[0].Check != "policy.tool" {
		t.Fatalf("missing tool plan = %+v", plan)
	}
}

type noOutputRunner struct{}

func (noOutputRunner) Run(context.Context, string, policy.Command) error {
	return errors.New("unexpected execution")
}

func rustRepository(t *testing.T) repository.Repository {
	t.Helper()
	policyRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.Repository{Root: t.TempDir(), PolicyRoot: policyRoot}
	if err := ToolProblem(repo); err != nil {
		t.Fatalf("Rust test prerequisite: %v", err)
	}
	return repo
}

func write(t *testing.T, root, path, data string) {
	t.Helper()
	destination := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
