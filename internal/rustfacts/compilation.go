package rustfacts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type CompilationPlan struct {
	Manifests []string
	Commands  []policy.Command
	Findings  []policy.Finding
}

func PlanCompilation(repo repository.Repository, discovery DiscoveryPlan, allFiles []string) CompilationPlan {
	plan := CompilationPlan{}
	if len(discovery.Commands) == 0 {
		return plan
	}
	manifests, _, findings := cargoResolutionRoots(repo, discovery.Manifests)
	plan.Findings = append(plan.Findings, findings...)
	for _, manifest := range manifests {
		files := compilationSources(repo, manifest, allFiles)
		native, err := repo.NativeUnit(files, "lint", "typecheck", "dead-code")
		if err != nil {
			plan.Findings = append(plan.Findings, projectFinding(manifest, "compilation-unit", err.Error()))
			continue
		}
		if !native {
			continue
		}
		if err := compilationConfiguration(repo, manifest); err != nil {
			plan.Findings = append(plan.Findings, projectFinding(manifest, "configuration", err.Error()))
			continue
		}
		plan.Manifests = append(plan.Manifests, manifest)
		plan.Commands = append(plan.Commands, compilationCommand(repo, manifest))
	}
	return plan
}

func compilationSources(repo repository.Repository, manifest string, allFiles []string) []string {
	root := filepath.ToSlash(filepath.Dir(manifest))
	files := []string{}
	for _, path := range allFiles {
		if repo.Language(path) == "rust" && (root == "." || strings.HasPrefix(path, root+"/")) {
			files = append(files, path)
		}
	}
	return files
}

func compilationCommand(repo repository.Repository, manifest string) policy.Command {
	command := Command(repo, manifestCommandName("policy-clippy", manifest), "cargo", "check", "--frozen", "--all-targets", "--message-format=json", "--target", HostTarget(), "--manifest-path", manifest)
	command.Environment = append(command.Environment, "PATH", "LIB", "INCLUDE", "LIBPATH", "SDKROOT", "DEVELOPER_DIR")
	command.EnvironmentOverrides = append(command.EnvironmentOverrides, "RUSTC_WRAPPER="+repo.RustTool("clippy-driver"), "CLIPPY_ARGS="+strings.Join(compilationLintArguments(), clippyArgumentSeparator)+clippyArgumentSeparator, "CLIPPY_CONF_DIR="+filepath.Join(repo.PolicyRoot, "tools"))
	return command
}

func compilationLintArguments() []string {
	arguments := []string{"--force-warn", "clippy::all", "--force-warn", "unused"}
	for _, lint := range strings.Fields(compilerWarningLints) {
		arguments = append(arguments, "--force-warn", lint)
	}
	return arguments
}

const clippyArgumentSeparator = "__CLIPPY_HACKERY__"

func HostTarget() string {
	architecture := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	platform := map[string]string{"linux": "unknown-linux-gnu", "darwin": "apple-darwin", "windows": "pc-windows-msvc"}[runtime.GOOS]
	if architecture == "" || platform == "" || runtime.GOOS == "windows" && runtime.GOARCH != "amd64" {
		return ""
	}
	return architecture + "-" + platform
}

func Compile(ctx context.Context, repo repository.Repository, plan CompilationPlan, discovery Discovery, commandRunner runner.Runner) []policy.Finding {
	findings := slices.Clone(plan.Findings)
	for index, command := range plan.Commands {
		manifest := plan.Manifests[index]
		workspace := workspaceForManifest(discovery.Workspaces, manifest)
		if workspace == nil {
			findings = append(findings, projectFinding(manifest, "compilation", "compilation requires valid Cargo workspace discovery"))
			continue
		}
		findings = append(findings, compileManifest(ctx, repo, manifest, *workspace, command, commandRunner)...)
	}
	return findings
}

func workspaceForManifest(workspaces []Workspace, manifest string) *Workspace {
	for index := range workspaces {
		workspace := &workspaces[index]
		if filepath.ToSlash(filepath.Join(workspace.Root, "Cargo.toml")) == manifest || slices.ContainsFunc(workspace.Packages, func(pkg Package) bool { return pkg.Manifest == manifest }) {
			return workspace
		}
	}
	return nil
}

func compileManifest(ctx context.Context, repo repository.Repository, manifest string, workspace Workspace, command policy.Command, commandRunner runner.Runner) []policy.Finding {
	lock := filepath.ToSlash(filepath.Join(workspace.Root, "Cargo.lock"))
	if _, err := repo.ReadContainedFile(lock, 8<<20); err != nil {
		return []policy.Finding{projectFinding(lock, "lockfile", fmt.Sprintf("governed compilation requires a contained Cargo.lock; prepare and review dependency resolution before checking: %v", err))}
	}
	_, output, runErr := runner.RunCaptured(ctx, commandRunner, repo.Root, command, runner.CaptureStructured)
	report, parseErr := ParseCompilation(repo, manifest, workspace, output.Stdout)
	findings := report.Findings
	if parseErr != nil {
		findings = append(findings, projectFinding(manifest, "compiler-output", fmt.Sprintf("invalid Cargo compiler evidence: %v; %s", parseErr, diagnosticOutput(output.Stderr))))
	}
	if runErr != nil {
		findings = append(findings, projectFinding(manifest, "compilation", fmt.Sprintf("Cargo compilation failed: %v; %s", runErr, diagnosticOutput(output.Stderr))))
	}
	if parseErr == nil && runErr == nil && !report.Success {
		findings = append(findings, projectFinding(manifest, "compilation", "Cargo reported an unsuccessful build without a failing process exit"))
	}
	return findings
}

func compilationConfiguration(repo repository.Repository, manifest string) error {
	if HostTarget() == "" {
		return errors.New("governed Rust compilation is unavailable on this host")
	}
	if err := compilationTargetConfiguration(repo); err != nil {
		return err
	}
	for parent := filepath.Dir(manifest); ; parent = filepath.Dir(parent) {
		for _, name := range []string{"clippy.toml", ".clippy.toml"} {
			path := filepath.Join(repo.Root, parent, name)
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("project Clippy configuration %s needs governed settings support; policy-owned Clippy defaults are required", filepath.ToSlash(filepath.Join(parent, name)))
			}
		}
		if parent == "." || filepath.Dir(parent) == parent {
			return nil
		}
	}
}
