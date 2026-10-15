package policy

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

const NextestJUnitArtifact = "nextest-junit"
const RustdocArtifact = "rustdoc"
const RustdocToolVersion = "1.97.1"

func validateSuiteRunner(suite TestSuite, label string) error {
	if suite.Rust == nil {
		if err := validateCommandArgv(suite.Argv, label); err != nil {
			return err
		}
		return validateTestCommandArgv(suite.Argv, label)
	}
	if len(suite.Argv) != 0 || len(suite.RetryArgv) != 0 || suite.Cwd != "." {
		return fmt.Errorf("%s.rust owns its command and repository-root invocation; argv, retryArgv and another cwd cannot replace it", label)
	}
	if err := validateRustTestSelection(*suite.Rust, label); err != nil {
		return err
	}
	if err := validateRustTestEnvironment(suite.Environment, label); err != nil {
		return err
	}
	return validateRustTestArtifacts(suite.Artifacts, label)
}

func validateRustTestSelection(settings RustTestSuite, label string) error {
	if settings.Doctests && settings.Filter != "" {
		return fmt.Errorf("%s.rust.filter is a Nextest expression and cannot select doctests", label)
	}
	if path.Base(settings.Manifest) != "Cargo.toml" {
		return fmt.Errorf("%s.rust.manifest must identify a Cargo.toml", label)
	}
	if err := repositoryPath(settings.Manifest, label+".rust.manifest"); err != nil {
		return err
	}
	for _, argument := range append(append([]string{}, settings.Packages...), settings.Features...) {
		if !ValidRustTestArgument(argument) {
			return fmt.Errorf("%s.rust package and feature selectors must be individual Cargo arguments", label)
		}
	}
	if strings.ContainsRune(settings.Filter, 0) {
		return fmt.Errorf("%s.rust.filter cannot contain a NUL byte", label)
	}
	return nil
}

func validateRustTestEnvironment(environment []string, label string) error {
	for _, name := range environment {
		if rustExecutionVariable(strings.ToUpper(name)) {
			return fmt.Errorf("%s.rust owns Rust, Cargo and Nextest execution settings; environment cannot inherit %s", label, name)
		}
	}
	return nil
}

func rustExecutionVariable(name string) bool {
	if slices.Contains([]string{"CARGO", "RUSTC", "RUSTDOC", "RUSTFLAGS", "RUSTDOCFLAGS"}, name) {
		return true
	}
	for _, prefix := range []string{"CARGO_", "NEXTEST_", "RUSTC_", "RUSTDOC_", "RUSTUP_", "RUST_TEST_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func validateRustTestArtifacts(artifacts []TestArtifact, label string) error {
	for _, artifact := range artifacts {
		if artifact.Path == "nextest.xml" || artifact.Path == "rustdoc.json" || artifact.Path == ".code-polishy-nextest.toml" || artifact.Type == NextestJUnitArtifact || artifact.Type == RustdocArtifact {
			return fmt.Errorf("%s.rust owns its required native test reports and configuration", label)
		}
	}
	return nil
}

func (suite TestSuite) ArtifactDeclarations() []TestArtifact {
	result := append([]TestArtifact{}, suite.Artifacts...)
	if suite.Rust != nil && suite.Rust.Doctests {
		result = append(result, TestArtifact{Path: "rustdoc.json", Type: RustdocArtifact, Required: true})
	} else if suite.Rust != nil {
		result = append(result, TestArtifact{Path: "nextest.xml", Type: NextestJUnitArtifact, Required: true})
	}
	return result
}

func (settings RustTestSuite) Clone() *RustTestSuite {
	settings.Packages = append([]string{}, settings.Packages...)
	settings.Features = append([]string{}, settings.Features...)
	return &settings
}

func (settings *RustTestSuite) Equal(other *RustTestSuite) bool {
	if settings == nil || other == nil {
		return settings == other
	}
	return settings.Manifest == other.Manifest && settings.Doctests == other.Doctests && settings.Filter == other.Filter && settings.NoDefaultFeatures == other.NoDefaultFeatures &&
		slices.Equal(settings.Packages, other.Packages) && slices.Equal(settings.Features, other.Features)
}

func ValidRustTestArgument(value string) bool {
	return value != "" && !strings.HasPrefix(value, "-") && !strings.ContainsAny(value, "\x00\n\r\t ")
}
