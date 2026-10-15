package rustfacts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestProblem(repo repository.Repository, settings policy.RustTestSuite) error {
	plan := PlanDiscovery(repo, []string{settings.Manifest}, []string{settings.Manifest})
	if len(plan.Findings) != 0 {
		return errors.New(plan.Findings[0].Message)
	}
	if HostTarget() == "" {
		return errors.New("native Rust testing is unavailable on this host")
	}
	if err := compilationTargetConfiguration(repo); err != nil {
		return err
	}
	if settings.Doctests {
		if repo.ToolPin("rust") != policy.RustdocToolVersion {
			return errors.New("doctest evidence requires its matching pinned Rust compiler")
		}
		return nil
	}
	info, err := os.Stat(repo.PolicyTool("cargo-nextest"))
	if err != nil || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode()&0o111 == 0 || repo.ToolPin("cargo-nextest") == "" {
		return errors.New("pinned cargo-nextest is unavailable; install the policy-owned test runner")
	}
	return nextestConfigurationProblem(repo, settings.Manifest)
}

func nextestConfigurationProblem(repo repository.Repository, manifest string) error {
	inventory := repo.InspectCargoManifests([]string{manifest})
	if len(inventory.Problems) > 0 || len(inventory.Manifests) != 1 {
		return errors.New("nextest configuration requires a valid contained Cargo manifest")
	}
	if pkg := inventory.Manifests[0].Package; pkg != nil && pkg.Workspace != "" {
		workspace, err := repo.NormalizePath(filepath.ToSlash(filepath.Join(filepath.Dir(manifest), pkg.Workspace, "Cargo.toml")))
		if err != nil {
			return err
		}
		if err := nextestAncestorSettings(repo, workspace); err != nil {
			return err
		}
	}
	return nextestAncestorSettings(repo, manifest)
}

func nextestAncestorSettings(repo repository.Repository, manifest string) error {
	for directory := filepath.Dir(manifest); ; directory = filepath.Dir(directory) {
		name := filepath.ToSlash(filepath.Join(directory, ".config", "nextest.toml"))
		if _, err := os.Lstat(filepath.Join(repo.Root, filepath.FromSlash(name))); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("project Nextest configuration %s requires governed settings support; native tests cannot silently replace its filters or setup scripts", name)
		}
		if directory == "." {
			return nil
		}
	}
}
