package behaviorreview

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestRustRegressionExecutionRequiresRealTestOutcomes(t *testing.T) {
	t.Parallel()
	testRustRegressionExecution(t, false)
}

func TestRustDoctestRegressionExecutionRequiresRealTestOutcomes(t *testing.T) {
	t.Parallel()
	testRustRegressionExecution(t, true)
}

func testRustRegressionExecution(t *testing.T, doctests bool) {
	t.Helper()
	policyRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	suite := policy.TestSuite{Name: "rust-unit", Cwd: ".", TimeoutSeconds: 30, Rust: &policy.RustTestSuite{Manifest: "Cargo.toml", Doctests: doctests}}
	tests := map[string]struct {
		source      string
		redExit     int
		operational bool
	}{
		"assertion": {source: "#[test]\nfn behavior() { assert_eq!(2 + 2, 5); }\n", redExit: 100},
		"compiler":  {source: "#[test]\nfn behavior() { missing_function(); }\n", redExit: 101, operational: true},
		"empty":     {source: "pub fn answer() -> u32 { 42 }\n", redExit: 4, operational: true},
	}
	greenSource := "#[test]\nfn behavior() { assert_eq!(2 + 2, 4); }\n"
	if doctests {
		greenSource = "/// ```\n/// assert_eq!(2 + 2, 4);\n/// ```\npub fn answer() {}\n"
		assertion := tests["assertion"]
		assertion.source = "/// ```\n/// assert_eq!(2 + 2, 5);\n/// ```\npub fn answer() {}\n"
		assertion.redExit = 101
		tests["assertion"] = assertion
		compiler := tests["compiler"]
		compiler.source = "pub fn answer() { missing_function(); }\n"
		tests["compiler"] = compiler
		empty := tests["empty"]
		empty.redExit = 0
		tests["empty"] = empty
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := repository.Repository{Root: t.TempDir(), PolicyRoot: policyRoot}
			writeBehaviorFile(t, repo.Root, "Cargo.toml", "[package]\nname='proof'\nversion='1.0.0'\nedition='2024'\n[workspace]\n")
			writeBehaviorFile(t, repo.Root, "Cargo.lock", "version=4\n[[package]]\nname='proof'\nversion='1.0.0'\n")
			writeBehaviorFile(t, repo.Root, "src/lib.rs", test.source)
			result, output, runErr := runSuite(t.Context(), repo, runner.OSRunner{}, repo.Root, suite)
			if runErr == nil || len(output.Stdout)+len(output.Stderr) == 0 || errors.Is(runErr, ErrOperational) != test.operational {
				t.Fatalf("native regression outcome: %+v, %v", result, runErr)
			}
			err := validateReplayBaseline(t.Context(), result.ExitStatus, runErr, regressionProof{ExpectedRedExitStatus: test.redExit})
			if (err == nil) == test.operational {
				t.Fatalf("invalid regression red: %+v, %v", result, err)
			}
			writeBehaviorFile(t, repo.Root, "src/lib.rs", greenSource)
			green, _, err := runSuite(t.Context(), repo, runner.OSRunner{}, repo.Root, suite)
			if err != nil || green.ExitStatus != 0 {
				t.Fatalf("valid green rejected: %+v, %v", green, err)
			}
		})
	}
}
