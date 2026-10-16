package testing

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestNativeRustSuiteProducesNonemptyEvidence(t *testing.T) {
	t.Parallel()
	repo, suite := rustSuiteRepository(t, "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n")
	before, err := os.ReadFile(filepath.Join(repo.Root, "Cargo.lock"))
	if err != nil {
		t.Fatal(err)
	}
	execution := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 1)
	if execution.Failed() || len(execution.Artifacts) != 1 || execution.Artifacts[0].Type != policy.NextestJUnitArtifact || execution.Artifacts[0].SHA256 == "" {
		t.Fatalf("native Rust execution: %+v", execution)
	}
	after, err := os.ReadFile(filepath.Join(repo.Root, "Cargo.lock"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("frozen testing changed the lock: %v", err)
	}
}

func TestNativeRustSuiteDistinguishesTestAndOperationalFailure(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		source, build string
		category      runner.FailureCategory
		artifact      bool
	}{
		"assertion":    {source: "#[test]\nfn behavior() { assert_eq!(2 + 2, 5); }\n", category: runner.FailureCommandExit, artifact: true},
		"empty":        {source: "pub fn answer() -> u32 { 42 }\n", category: runner.FailureOperational, artifact: true},
		"ignored":      {source: "#[test]\n#[ignore]\nfn behavior() { panic!(\"ignored\"); }\n", category: runner.FailureOperational, artifact: true},
		"compiler":     {source: "#[test]\nfn behavior() { let value: u32 = \"wrong\"; assert_eq!(value, 42); }\n", category: runner.FailureOperational},
		"build script": {source: "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n", build: "fn main() { panic!(\"build failed\"); }\n", category: runner.FailureOperational},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo, suite := rustSuiteRepository(t, test.source)
			if test.build != "" {
				writeRustSuiteFile(t, repo.Root, "build.rs", test.build)
			}
			execution := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 1)
			if !execution.Failed() || execution.FailureCategory != test.category || (len(execution.Artifacts) == 1) != test.artifact {
				t.Fatalf("failure classification: %+v", execution)
			}
		})
	}
}

func TestNativeRustSuiteHonorsCargoFeaturesAndFilter(t *testing.T) {
	t.Parallel()
	repo, suite := rustSuiteRepository(t, "#[test]\nfn selected() { assert!(!cfg!(feature = \"ordinary\")); assert!(cfg!(feature = \"chosen\")); }\n#[test]\nfn outside_filter() { panic!(\"not selected\"); }\n")
	writeRustSuiteFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n[workspace]\n[features]\ndefault=['ordinary']\nordinary=[]\nchosen=[]\n")
	suite.Rust.Features = []string{"chosen"}
	suite.Rust.NoDefaultFeatures = true
	suite.Rust.Filter = "test(=selected)"
	execution := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 1)
	if execution.Failed() || len(execution.Artifacts) != 1 {
		t.Fatalf("Cargo feature/filter selection: %+v", execution)
	}
	suite.Rust.Filter = "test(=absent)"
	empty := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 2)
	if !empty.Failed() || empty.FailureCategory != runner.FailureOperational {
		t.Fatalf("empty filter was accepted: %+v", empty)
	}
}

func TestNativeRustSuiteUsesWorkspaceDefaultsAndExplicitPackages(t *testing.T) {
	t.Parallel()
	repo, suite := rustSuiteRepository(t, "pub fn outside_workspace() {}\n")
	writeRustSuiteFile(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['crates/core','crates/service']\ndefault-members=['crates/core']\nresolver='3'\n")
	writeRustSuiteFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='core'\nversion='1.0.0'\n[[package]]\nname='service'\nversion='1.0.0'\n")
	writeRustSuiteFile(t, repo.Root, "crates/core/Cargo.toml", "[package]\nname='core'\nversion='1.0.0'\nedition='2024'\n")
	writeRustSuiteFile(t, repo.Root, "crates/service/Cargo.toml", "[package]\nname='service'\nversion='1.0.0'\nedition='2024'\n")
	writeRustSuiteFile(t, repo.Root, "crates/core/src/lib.rs", "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n")
	writeRustSuiteFile(t, repo.Root, "crates/service/src/lib.rs", "#[test]\nfn behavior() { assert_eq!(2 + 2, 5); }\n")
	defaultRun := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 1)
	if defaultRun.Failed() {
		t.Fatalf("unselected workspace member was tested: %+v", defaultRun)
	}
	suite.Rust.Packages = []string{"service"}
	explicit := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 2)
	if !explicit.Failed() || explicit.FailureCategory != runner.FailureCommandExit {
		t.Fatalf("explicit package was not tested: %+v", explicit)
	}
	suite.Rust.Packages = []string{"core"}
	writeRustSuiteFile(t, repo.Root, "crates/core/tests/public.rs", "#[test]\nfn public_behavior() { assert_eq!(2 + 2, 5); }\n")
	integration := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 3)
	if !integration.Failed() || integration.FailureCategory != runner.FailureCommandExit {
		t.Fatalf("integration target was not tested: %+v", integration)
	}
}

func TestNativeRustSuiteNeverRepairsLocksOrIgnoresProjectSettings(t *testing.T) {
	t.Parallel()
	repo, suite := rustSuiteRepository(t, "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n")
	writeRustSuiteFile(t, repo.Root, "Cargo.lock", "version = 4\n")
	execution := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 1)
	if !execution.Failed() || execution.FailureCategory != runner.FailureOperational {
		t.Fatalf("stale lock was accepted: %+v", execution)
	}
	lock, err := os.ReadFile(filepath.Join(repo.Root, "Cargo.lock"))
	if err != nil || string(lock) != "version = 4\n" {
		t.Fatalf("stale lock was changed: %q, %v", lock, err)
	}
	writeRustSuiteFile(t, repo.Root, ".config/nextest.toml", "[profile.default]\ndefault-filter='not test(live_probe)'\n")
	preflight := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 2)
	if !strings.Contains(preflight.FailureMessage, "cannot silently replace") {
		t.Fatalf("project settings were ignored: %+v", preflight)
	}
}

func TestNativeRustSuiteFindsExplicitWorkspaceNextestSettings(t *testing.T) {
	t.Parallel()
	repo, suite := rustSuiteRepository(t, "pub fn answer() -> u32 { 42 }\n")
	writeRustSuiteFile(t, repo.Root, "crates/app/Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\nworkspace='../../workspace'\n")
	writeRustSuiteFile(t, repo.Root, "workspace/Cargo.toml", "[workspace]\nmembers=['../crates/app']\nresolver='3'\n")
	writeRustSuiteFile(t, repo.Root, "workspace/.config/nextest.toml", "[profile.default]\ndefault-filter='not test(live_probe)'\n")
	suite.Rust.Manifest = "crates/app/Cargo.toml"
	execution := ExecuteSuite(t.Context(), repo, rustSuiteRunner(t), suite, 1)
	if !execution.Failed() || !strings.Contains(execution.FailureMessage, "workspace/.config/nextest.toml") {
		t.Fatalf("explicit workspace configuration was ignored: %+v", execution)
	}
}

func TestNativeRustCommandAndCopiesRetainGovernedSelection(t *testing.T) {
	t.Parallel()
	repo, suite := rustSuiteRepository(t, "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n")
	suite.Rust.Packages = []string{"app"}
	command := SuiteCommand(repo, suite)
	if command.Argv[0] != repo.PolicyTool("cargo-nextest") || !command.Managed || !command.SealedEnvironment ||
		!slices.Contains(command.EnvironmentOverrides, "CARGO="+repo.RustTool("cargo")) || !slices.Contains(command.EnvironmentOverrides, "CARGO_NET_OFFLINE=true") {
		t.Fatalf("ungoverned test command: %+v", command)
	}
	retry := RetrySuite(suite)
	retry.Rust.Packages[0] = "other"
	if suite.Rust.Packages[0] != "app" {
		t.Fatal("retry changed the selected original package")
	}
	plan := ExecutionPlanForDirectTest(Plan{Suites: []policy.TestSuite{suite}})
	if len(plan.Commands) != 2 || !slices.Contains(plan.Commands[1].Argv, "--package") {
		t.Fatalf("native command missing from execution plan: %+v", plan)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	execution := ExecuteSuite(ctx, repo, rustSuiteRunner(t), suite, 1)
	if !execution.Failed() || execution.FailureCategory != runner.FailureCanceled {
		t.Fatalf("canceled test became another outcome: %+v", execution)
	}
}

func rustSuiteRepository(t *testing.T, source string) (repository.Repository, policy.TestSuite) {
	t.Helper()
	policyRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.Repository{Root: t.TempDir(), PolicyRoot: policyRoot}
	writeRustSuiteFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n[workspace]\n")
	writeRustSuiteFile(t, repo.Root, "Cargo.lock", "version = 4\n[[package]]\nname = 'app'\nversion = '1.0.0'\n")
	writeRustSuiteFile(t, repo.Root, "src/lib.rs", source)
	suite := policy.TestSuite{Name: "rust-unit", Kind: "unit", Scope: "repository", Cwd: ".", TimeoutSeconds: 30, Rust: &policy.RustTestSuite{Manifest: "Cargo.toml"}}
	return repo, suite
}

func writeRustSuiteFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rustSuiteRunner(t *testing.T) runner.OSRunner {
	t.Helper()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Rust command stdout:\n%s\nstderr:\n%s", stdout, stderr)
		}
	})
	return runner.OSRunner{Stdout: stdout, Stderr: stderr}
}
