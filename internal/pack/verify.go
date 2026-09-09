package pack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	resolution := Resolution{}
	compileManifest(verifier.root, request.Pack, verifier.tree.Manifest, &resolution)
	Apply(&repo.Config, resolution)
	request.Provider = "pack." + request.Pack.Name + "." + fixture.Command + "." + fixture.Capability
	request.Profile = declared.Profiles[0]
	if err := prepareInputPaths(repo, &request, verifier.fixtureInputs(fixture)); err != nil {
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
	if response.Status == "operational-failure" {
		return fmt.Errorf("analyzer failed: %s", response.Failure)
	}
	if err := verifyAnalysisInputs(repo, request, response); err != nil {
		return err
	}
	findings := analysisFindings(repo, &policy.PackAdapter{PackName: request.Pack.Name, Capability: request.Capability}, response)
	status := response.Status
	if status == "pass" && slices.ContainsFunc(findings, func(finding policy.Finding) bool {
		return finding.Severity != policy.FindingInformation && finding.Severity != policy.FindingWarning
	}) {
		status = "findings"
	}
	if status != fixture.ExpectedStatus {
		return fmt.Errorf("expected %s, received %s", fixture.ExpectedStatus, status)
	}
	for _, rule := range fixture.ExpectedRules {
		if !slices.ContainsFunc(findings, func(finding policy.Finding) bool {
			return finding.Check == rule || finding.Check == "pack."+request.Pack.Name+"."+rule
		}) {
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

func (verifier fixtureVerifier) fixtureInputs(fixture Fixture) []string {
	paths := []string{}
	prefix := fixture.Project + "/"
	for _, file := range verifier.tree.Files {
		if strings.HasPrefix(file.Path, prefix) {
			paths = append(paths, strings.TrimPrefix(file.Path, prefix))
		}
	}
	return paths
}
