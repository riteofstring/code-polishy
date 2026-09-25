package testing

import (
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
	"github.com/riteofstring/code-polishy/internal/testartifact"
)

func SuiteCommand(repo repository.Repository, suite policy.TestSuite) policy.Command {
	command := policy.Command{
		Name: suite.Name, Argv: slices.Clone(suite.Argv), Cwd: suite.Cwd,
		Paths: slices.Clone(suite.Paths), Modules: slices.Clone(suite.Modules),
		Environment: slices.Clone(suite.Environment), ExclusiveResources: slices.Clone(suite.ExclusiveResources),
		TimeoutSeconds: suite.TimeoutSeconds, SealedEnvironment: suite.Reusable,
		TestArtifacts: suite.ArtifactDeclarations(),
	}
	if suite.Rust == nil {
		return command
	}
	native := rustfacts.Command(repo, suite.Name, "cargo")
	command.Argv = append([]string{repo.PolicyTool("cargo-nextest")}, rustTestArguments(*suite.Rust)...)
	command.Managed = true
	command.SealedEnvironment = true
	command.EnvironmentOverrides = append(native.EnvironmentOverrides, "CARGO="+repo.RustTool("cargo"))
	for _, key := range append(native.Environment, "PATH", "LIB", "INCLUDE", "LIBPATH", "SDKROOT", "DEVELOPER_DIR") {
		if !slices.Contains(command.Environment, key) {
			command.Environment = append(command.Environment, key)
		}
	}
	return command
}

func rustTestArguments(settings policy.RustTestSuite) []string {
	arguments := []string{
		"nextest", "run", "--frozen", "--manifest-path", settings.Manifest, "--target", rustfacts.HostTarget(),
		"--config-file", testartifact.DirectoryToken + "/" + testartifact.NextestConfigName,
		"--user-config-file", "none", "--profile", "code-polishy", "--retries", "0", "--no-fail-fast", "--no-tests", "fail",
	}
	for _, name := range settings.Packages {
		arguments = append(arguments, "--package", name)
	}
	for _, feature := range settings.Features {
		arguments = append(arguments, "--features", feature)
	}
	if settings.NoDefaultFeatures {
		arguments = append(arguments, "--no-default-features")
	}
	if settings.Filter != "" {
		arguments = append(arguments, "--filter-expr", settings.Filter)
	}
	return arguments
}

func suiteProblem(repo repository.Repository, suite policy.TestSuite) error {
	if suite.Rust != nil {
		return rustfacts.TestProblem(repo, *suite.Rust)
	}
	return nil
}
