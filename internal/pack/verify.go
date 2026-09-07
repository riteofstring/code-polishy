package pack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type VerificationResult struct {
	Manifest Manifest
	Fixtures int
}

func VerifySource(ctx context.Context, source, policyRoot string, commandRunner runner.Runner) (VerificationResult, error) {
	tree, err := readSourceTree(source, true)
	if err != nil {
		return VerificationResult{}, err
	}
	root, err := canonicalDirectory(source)
	if err != nil {
		return VerificationResult{}, err
	}
	verifier := fixtureVerifier{ctx: ctx, root: root, policyRoot: policyRoot, tree: tree, runner: commandRunner}
	for _, fixture := range tree.Manifest.Fixtures {
		if err := verifier.run(fixture); err != nil {
			return VerificationResult{}, fmt.Errorf("fixture %s: %w", fixture.Name, err)
		}
	}

	current, err := readSourceTree(source, true)
	if err != nil {
		return VerificationResult{}, err
	}
	if current.Receipt.Digest != tree.Receipt.Digest {
		return VerificationResult{}, fmt.Errorf("pack source changed during fixture verification")
	}
	return VerificationResult{Manifest: tree.Manifest, Fixtures: len(tree.Manifest.Fixtures)}, nil
}

type fixtureVerifier struct {
	ctx              context.Context
	root, policyRoot string
	tree             sourceTree
	runner           runner.Runner
}

func (verifier fixtureVerifier) run(fixture Fixture) error {
	declared := verifier.tree.Manifest.Commands[slices.IndexFunc(verifier.tree.Manifest.Commands, func(command Command) bool { return command.Name == fixture.Command })]
	command := policy.Command{Name: "pack-verify-" + fixture.Name, Argv: slices.Clone(declared.Argv), Cwd: ".", TimeoutSeconds: declared.TimeoutSeconds, Environment: slices.Clone(declared.Environment), ExclusiveResources: []string{}, SealedEnvironment: true, Adapter: &policy.PackAdapter{PackRoot: verifier.root}}
	projectRoot, err := filepath.EvalSymlinks(filepath.Join(verifier.root, filepath.FromSlash(fixture.Project)))
	if err != nil {
		return err
	}
	repo, err := fixtureRepository(projectRoot, verifier.policyRoot)
	if err != nil {
		return err
	}
	request := verifier.request(projectRoot, fixture)
	if err := prepareInputs(repo, &request); err != nil {
		return err
	}
	prepared, identity, err := runtimeCommand(repo, command, declared.Runtime)
	if err != nil {
		return err
	}
	request.Runtime = identity
	response, err := execute(verifier.ctx, verifier.root, prepared, verifier.runner, request)
	if err != nil {
		return err
	}
	if err := verifyRuntimeIdentity(repo, command, identity, declared.Runtime); err != nil {
		return err
	}
	return verifyFixtureResult(repo, fixture, request, response)
}

func (verifier fixtureVerifier) request(projectRoot string, fixture Fixture) Request {
	operation := "check"
	if fixture.Capability == "format" {
		operation = "format"
	}
	return Request{ProtocolVersion: ProtocolVersion, Operation: operation, Capability: fixture.Capability, ProjectRoot: projectRoot, Files: slices.Clone(fixture.Files), Modules: []RequestModule{}, Mode: "check", Profile: "verify", Complete: true, Pack: policy.PackSelection{Name: verifier.tree.Manifest.Name, Version: verifier.tree.Manifest.Version, Digest: verifier.tree.Receipt.Digest}}
}

func verifyFixtureResult(repo repository.Repository, fixture Fixture, request Request, response Response) error {
	if response.Status != fixture.ExpectedStatus {
		return fmt.Errorf("expected %s, received %s", fixture.ExpectedStatus, response.Status)
	}
	if err := verifyAnalysisInputs(repo, request, response); err != nil {
		return err
	}
	for _, rule := range fixture.ExpectedRules {
		if !slices.ContainsFunc(response.Findings, func(finding ResponseFinding) bool { return finding.Rule == rule }) {
			return fmt.Errorf("did not detect expected rule %s", rule)
		}
	}
	return nil
}

func fixtureRepository(root, policyRoot string) (repository.Repository, error) {
	config := policy.Config{Quality: policy.EffectiveQuality(policy.Quality{})}
	if _, err := os.Stat(filepath.Join(root, policy.ConfigFilename)); err == nil {
		loaded, err := policy.Load(root, "")
		if err != nil {
			return repository.Repository{}, err
		}
		config = loaded
	}
	return repository.Open(root, policyRoot, config)
}

func DefaultRunner() runner.OSRunner {
	return runner.OSRunner{Stderr: os.Stderr}
}
