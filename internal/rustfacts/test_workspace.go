package rustfacts

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

func TestWorkspaceCommand(repo repository.Repository, name, manifest string) policy.Command {
	return Command(repo, name, "cargo", "metadata", "--format-version", "1", "--no-deps", "--frozen", "--manifest-path", manifest)
}

func ValidateTestWorkspace(ctx context.Context, repo repository.Repository, root string, settings policy.RustTestSuite, command policy.Command, commandRunner runner.Runner) (Workspace, runner.Result, error) {
	view := repo
	view.Root = root
	if err := configurationProblem(view); err != nil {
		return Workspace{}, runner.Result{ExitStatus: -1}, err
	}
	result, output, err := runner.RunCaptured(ctx, commandRunner, root, command, runner.CaptureStructured)
	if err != nil {
		return Workspace{}, result, fmt.Errorf("native Rust workspace discovery failed: %w: %s", err, diagnosticOutput(output.Stderr))
	}
	workspace, err := validateTestWorkspaceOutput(view, settings, output.Stdout, TargetDirectory(repo))
	return workspace, result, err
}

func validateTestWorkspaceOutput(repo repository.Repository, settings policy.RustTestSuite, data []byte, target string) (Workspace, error) {
	wire, err := decodeWorkspace(data)
	if err != nil {
		return Workspace{}, err
	}
	workspace, err := workspaceFromWireWithTarget(repo, settings.Manifest, wire, target)
	if err != nil {
		return Workspace{}, err
	}
	lock := filepath.ToSlash(filepath.Join(workspace.Root, "Cargo.lock"))
	if _, err := repo.ReadContainedFile(lock, 8<<20); err != nil {
		return Workspace{}, fmt.Errorf("native Rust tests require a contained Cargo.lock at the discovered workspace root: %w", err)
	}
	if !settings.Doctests {
		return workspace, nextestAncestorSettings(repo, filepath.ToSlash(filepath.Join(workspace.Root, "Cargo.toml")))
	}
	return workspace, nil
}

func SameTestWorkspace(source, view Workspace) bool {
	return reflect.DeepEqual(testWorkspaceIdentity(source), testWorkspaceIdentity(view))
}

type testWorkspaceFacts struct {
	Root           string
	TargetDir      string
	Packages       map[string]Package
	DefaultMembers map[string]bool
}

func testWorkspaceIdentity(workspace Workspace) testWorkspaceFacts {
	facts := testWorkspaceFacts{Root: workspace.Root, TargetDir: workspace.TargetDir, Packages: map[string]Package{}, DefaultMembers: map[string]bool{}}
	manifests := map[string]string{}
	for _, pkg := range workspace.Packages {
		manifests[pkg.ID] = pkg.Manifest
		pkg.ID = ""
		facts.Packages[pkg.Manifest] = pkg
	}
	for _, member := range workspace.DefaultMembers {
		facts.DefaultMembers[manifests[member]] = true
	}
	return facts
}
