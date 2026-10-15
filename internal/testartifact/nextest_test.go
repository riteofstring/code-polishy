package testartifact

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func nextestTestReport(outcome string, tests, failures, errors, skipped int) string {
	return fmt.Sprintf(`<testsuites name="rust-unit" uuid="90dd5f16-e586-406d-b753-173845b40a69" tests="%d" failures="%d" errors="%d"><testsuite name="crate::integration" tests="%d" failures="%d" errors="%d" disabled="%d"><testcase name="module::behavior" classname="crate::integration">%s</testcase></testsuite></testsuites>`, tests, failures, errors, tests, failures, errors, skipped, outcome)
}

func TestNextestEvidenceAgreesWithExecutedCasesAndExit(t *testing.T) {
	t.Parallel()
	pass := nextestTestReport("", 1, 0, 0, 0)
	failure := nextestTestReport(`<failure message="assertion failed"/><system-out>captured &amp; escaped</system-out>`, 1, 1, 0, 0)
	cases := []struct {
		name, report string
		exit         int
		valid        bool
	}{
		{"passing", pass, 0, true},
		{"assertion", failure, 100, true},
		{"test error", nextestTestReport(`<error type="timeout"/>`, 1, 0, 1, 0), 100, true},
		{"failure hidden by success", failure, 0, false},
		{"failed exit with passing report", pass, 100, false},
		{"operational exit", failure, 101, false},
		{"all ignored", `<testsuites name="rust-unit" uuid="run" tests="0" failures="0" errors="0"/>`, 0, false},
		{"all skipped", nextestTestReport(`<skipped/>`, 1, 0, 0, 1), 0, false},
		{"missing case", strings.Replace(pass, `tests="1"`, `tests="2"`, -1), 0, false},
		{"wrong suite", strings.Replace(pass, `name="rust-unit"`, `name="other"`, 1), 0, false},
		{"missing count", strings.Replace(pass, ` failures="0"`, "", 1), 0, false},
		{"conflicting outcome", nextestTestReport(`<failure/><skipped/>`, 1, 1, 0, 1), 100, false},
		{"retried pass", nextestTestReport(`<flakyFailure/>`, 1, 0, 0, 0), 0, false},
		{"truncated", strings.TrimSuffix(pass, "</testsuites>"), 0, false},
		{"two roots", pass + pass, 0, false},
		{"directive", "<!DOCTYPE testsuites>" + pass, 0, false},
		{"duplicate attribute", strings.Replace(pass, `tests="1"`, `tests="0" tests="1"`, 1), 0, false},
		{"trailing text", pass + "incomplete", 0, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			err := validateNextest([]byte(test.report), "rust-unit", test.exit)
			if (err == nil) != test.valid {
				t.Fatalf("evidence accepted=%v, want %v: %v", err == nil, test.valid, err)
			}
		})
	}
}

func TestNextestEvidenceRejectsDuplicateCaseAndBinaryIdentity(t *testing.T) {
	t.Parallel()
	pass := nextestTestReport("", 1, 0, 0, 0)
	item := `<testcase name="module::behavior" classname="crate::integration"></testcase>`
	duplicate := strings.Replace(pass, item, item+item, 1)
	duplicate = strings.ReplaceAll(duplicate, `tests="1"`, `tests="2"`)
	if err := validateNextest([]byte(duplicate), "rust-unit", 0); err == nil || !strings.Contains(err.Error(), "duplicate test") {
		t.Fatalf("duplicate test evidence: %v", err)
	}
	start := strings.Index(pass, "<testsuite ")
	end := strings.Index(pass, "</testsuites>")
	duplicate = strings.Replace(pass, "</testsuites>", pass[start:end]+"</testsuites>", 1)
	if err := validateNextest([]byte(duplicate), "rust-unit", 0); err == nil || !strings.Contains(err.Error(), "duplicate binary") {
		t.Fatalf("duplicate binary evidence: %v", err)
	}
}

func TestNextestPreparationRetainsCompilerEnvironmentAndFreshEvidence(t *testing.T) {
	t.Parallel()
	execution, err := Start(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	suite := policy.TestSuite{Name: "rust-unit", Rust: &policy.RustTestSuite{Manifest: "Cargo.toml"}}
	command, err := execution.PrepareCommand(suite, policy.Command{
		Argv:                 []string{"cargo-nextest", "--config-file", DirectoryToken + "/" + NextestConfigName},
		EnvironmentOverrides: []string{"RUSTC=/governed/rustc", "CARGO_NET_OFFLINE=true"},
	})
	if err != nil || !slices.Contains(command.EnvironmentOverrides, "RUSTC=/governed/rustc") {
		t.Fatalf("compiler environment: %+v, %v", command, err)
	}
	assertNextestSettings(t, command)
	writeArtifactFile(t, command.TestArtifactDirectory, "nextest.xml", nextestTestReport("<failure/>", 1, 1, 0, 0))
	artifacts, err := execution.ValidateCommand(command, 100)
	if err != nil || len(artifacts) != 1 || artifacts[0].SHA256 == "" || !artifacts[0].Required {
		t.Fatalf("failed test artifacts: %+v, %v", artifacts, err)
	}
	retry, err := execution.PrepareCommand(suite, command)
	if err != nil || retry.TestArtifactDirectory == command.TestArtifactDirectory {
		t.Fatalf("fresh retry: %+v, %v", retry, err)
	}
	if _, err := execution.ValidateCommand(retry, 0); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("stale report satisfied retry: %v", err)
	}
}

func assertNextestSettings(t *testing.T, command policy.Command) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(command.TestArtifactDirectory, NextestConfigName))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Profile map[string]struct {
			Retries  int  `toml:"retries"`
			FailFast bool `toml:"fail-fast"`
			JUnit    struct {
				Path string `toml:"path"`
				Name string `toml:"report-name"`
			} `toml:"junit"`
		} `toml:"profile"`
	}
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	profile, exists := parsed.Profile["code-polishy"]
	if !exists || profile.Retries != 0 || profile.FailFast || profile.JUnit.Name != command.TestArtifactSuite || profile.JUnit.Path != filepath.Join(command.TestArtifactDirectory, "nextest.xml") {
		t.Fatalf("Nextest settings: %s", data)
	}
}
