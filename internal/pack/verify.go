package pack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type VerificationResult struct {
	Manifest Manifest
	Fixtures int
}

func VerifySource(ctx context.Context, source string, commandRunner runner.Runner) (VerificationResult, error) {
	tree, err := readSourceTree(source, true)
	if err != nil {
		return VerificationResult{}, err
	}
	root, err := canonicalDirectory(source)
	if err != nil {
		return VerificationResult{}, err
	}
	for _, fixture := range tree.Manifest.Fixtures {
		declared := tree.Manifest.Commands[slices.IndexFunc(tree.Manifest.Commands, func(command Command) bool { return command.Name == fixture.Command })]
		command := policy.Command{Name: "pack-verify-" + fixture.Name, Argv: slices.Clone(declared.Argv), Cwd: ".", TimeoutSeconds: declared.TimeoutSeconds, Environment: slices.Clone(declared.Environment), ExclusiveResources: []string{}, SealedEnvironment: true}
		projectRoot, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(fixture.Project)))
		if err != nil {
			return VerificationResult{}, err
		}
		request := Request{ProtocolVersion: ProtocolVersion, Operation: "check", Capability: fixture.Capability, ProjectRoot: projectRoot, Files: slices.Clone(fixture.Files), Modules: []RequestModule{}, Mode: "check", Profile: "verify"}
		response, err := execute(ctx, root, command, commandRunner, request)
		if err != nil {
			return VerificationResult{}, fmt.Errorf("fixture %s: %w", fixture.Name, err)
		}
		if response.Status != fixture.ExpectedStatus {
			return VerificationResult{}, fmt.Errorf("fixture %s expected %s, received %s", fixture.Name, fixture.ExpectedStatus, response.Status)
		}
	}
	return VerificationResult{Manifest: tree.Manifest, Fixtures: len(tree.Manifest.Fixtures)}, nil
}

func DefaultRunner() runner.OSRunner {
	return runner.OSRunner{Stderr: os.Stderr}
}
