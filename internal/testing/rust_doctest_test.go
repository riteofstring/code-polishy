package testing

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestNativeRustDoctestsRespectExampleSemantics(t *testing.T) {
	t.Parallel()
	source := "/// ```\n/// assert_eq!(app::answer(), 42);\n/// ```\n" +
		"/// ```compile_fail\n/// let value: u32 = \"wrong\";\n/// ```\n" +
		"/// ```no_run\n/// panic!(\"must not execute\");\n/// ```\n" +
		"/// ```ignore\n/// unavailable();\n/// ```\n" +
		"/// ```should_panic\n/// panic!(\"expected\");\n/// ```\n" +
		"pub fn answer() -> u32 { 42 }\n"
	repo, suite := rustSuiteRepository(t, source)
	suite.Rust.Doctests = true
	writeRustSuiteFile(t, repo.Root, ".config/nextest.toml", "[profile.default]\ndefault-filter='none()'\n")
	before, err := os.ReadFile(filepath.Join(repo.Root, "Cargo.lock"))
	if err != nil {
		t.Fatal(err)
	}
	execution := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 1)
	if execution.Failed() || len(execution.Artifacts) != 1 || execution.Artifacts[0].Type != policy.RustdocArtifact {
		t.Fatalf("doctest execution: %+v", execution)
	}
	after, err := os.ReadFile(filepath.Join(repo.Root, "Cargo.lock"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("doctesting changed the frozen lock: %v", err)
	}
}

func TestNativeRustDoctestsKeepFailuresDistinct(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		source, build string
		category      runner.FailureCategory
		artifact      bool
	}{
		"assertion":                        {source: "/// ```\n/// assert_eq!(2 + 2, 5);\n/// ```\npub fn answer() {}\n", category: runner.FailureCommandExit, artifact: true},
		"invalid example":                  {source: "/// ```\n/// let value: u8 = \"wrong\";\n/// ```\npub fn answer() {}\n", category: runner.FailureCommandExit, artifact: true},
		"compile fail unexpectedly passes": {source: "/// ```compile_fail\n/// let _value: u8 = 42;\n/// ```\npub fn answer() {}\n", category: runner.FailureCommandExit, artifact: true},
		"empty":                            {source: "pub fn answer() {}\n", category: runner.FailureOperational, artifact: true},
		"ignored":                          {source: "/// ```ignore\n/// panic!();\n/// ```\npub fn answer() {}\n", category: runner.FailureOperational, artifact: true},
		"compiler":                         {source: "/// ```\n/// assert_eq!(2 + 2, 4);\n/// ```\npub fn answer() -> u8 { \"wrong\" }\n", category: runner.FailureOperational},
		"build script":                     {source: "/// ```\n/// assert_eq!(2 + 2, 4);\n/// ```\npub fn answer() {}\n", build: "fn main() { panic!(\"cannot build\"); }\n", category: runner.FailureOperational},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo, suite := rustSuiteRepository(t, test.source)
			suite.Rust.Doctests = true
			if test.build != "" {
				writeRustSuiteFile(t, repo.Root, "build.rs", test.build)
			}
			execution := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 1)
			if !execution.Failed() || execution.FailureCategory != test.category || (len(execution.Artifacts) == 1) != test.artifact {
				t.Fatalf("doctest classification: %+v", execution)
			}
		})
	}
}

func TestNativeRustDoctestsHonorFeaturesAndCancellation(t *testing.T) {
	t.Parallel()
	repo, suite := rustSuiteRepository(t, "/// ```\n/// assert!(app::chosen());\n/// ```\npub fn chosen() -> bool { cfg!(feature = \"chosen\") && !cfg!(feature = \"ordinary\") }\n")
	writeRustSuiteFile(t, repo.Root, "Cargo.toml", "[package]\nname='app'\nversion='1.0.0'\nedition='2024'\n[workspace]\n[features]\ndefault=['ordinary']\nordinary=[]\nchosen=[]\n")
	suite.Rust.Doctests = true
	suite.Rust.Features = []string{"chosen"}
	suite.Rust.NoDefaultFeatures = true
	if execution := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 1); execution.Failed() {
		t.Fatalf("doctest features: %+v", execution)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if execution := ExecuteSuite(ctx, repo, runner.OSRunner{}, suite, 2); execution.FailureCategory != runner.FailureCanceled {
		t.Fatalf("doctest cancellation: %+v", execution)
	}
	writeRustSuiteFile(t, repo.Root, "Cargo.lock", "version=4\n")
	if execution := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 3); execution.FailureCategory != runner.FailureOperational {
		t.Fatalf("doctest stale lock: %+v", execution)
	}
	data, err := os.ReadFile(filepath.Join(repo.Root, "Cargo.lock"))
	if err != nil || string(data) != "version=4\n" {
		t.Fatalf("stale lock changed: %s, %v", data, err)
	}
}

func TestNativeRustDoctestsKeepWorkspaceBatches(t *testing.T) {
	t.Parallel()
	repo, suite := rustSuiteRepository(t, "pub fn outside_workspace() {}\n")
	writeRustSuiteFile(t, repo.Root, "Cargo.toml", "[workspace]\nmembers=['first','second']\nresolver='3'\n")
	writeRustSuiteFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='first'\nversion='1.0.0'\n[[package]]\nname='second'\nversion='1.0.0'\n")
	for _, name := range []string{"first", "second"} {
		writeRustSuiteFile(t, repo.Root, name+"/Cargo.toml", "[package]\nname='"+name+"'\nversion='1.0.0'\nedition='2024'\n")
		writeRustSuiteFile(t, repo.Root, name+"/src/lib.rs", "/// ```\n/// assert_eq!(2 + 2, 4);\n/// ```\npub fn answer() {}\n")
	}
	suite.Rust.Doctests = true
	if execution := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 1); execution.Failed() {
		t.Fatalf("workspace doctests: %+v", execution)
	}
	writeRustSuiteFile(t, repo.Root, "second/src/lib.rs", "/// ```\n/// assert_eq!(2 + 2, 5);\n/// ```\npub fn answer() {}\n")
	if execution := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 2); execution.FailureCategory != runner.FailureCommandExit {
		t.Fatalf("workspace doctest failure: %+v", execution)
	}
	suite.Rust.Packages = []string{"first"}
	if execution := ExecuteSuite(t.Context(), repo, runner.OSRunner{}, suite, 3); execution.Failed() {
		t.Fatalf("selected package doctests: %+v", execution)
	}
	plan := ExecutionPlanForDirectTest(Plan{Suites: []policy.TestSuite{suite}})
	if len(plan.Commands) != 1 || plan.Commands[0].Argv[0] != "cargo" || !strings.Contains(strings.Join(plan.Commands[0].Argv, " "), "test --doc --frozen") {
		t.Fatalf("doctest execution plan: %+v", plan)
	}
}
