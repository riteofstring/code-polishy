package rustfacts

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type DiscoveryPlan struct {
	Manifests []string
	Commands  []policy.Command
	Findings  []policy.Finding
}

type Discovery struct {
	Workspaces []Workspace
	Findings   []policy.Finding
}

func PlanDiscovery(repo repository.Repository, selected, allFiles []string) DiscoveryPlan {
	plan := DiscoveryPlan{}
	manifests := selectedManifests(selected, allFiles)
	if len(manifests) == 0 {
		return plan
	}
	if err := configurationProblem(repo); err != nil {
		plan.Findings = append(plan.Findings, projectFinding(manifests[0], "configuration", err.Error()))
		return plan
	}
	inventory := repo.InspectCargoManifests(manifests)
	for _, problem := range inventory.Problems {
		finding := projectFinding(problem.Path, "manifest", problem.Message)
		finding.Line = problem.Line
		plan.Findings = append(plan.Findings, finding)
	}
	for _, manifest := range inventory.Manifests {
		if err := manifestPathProblem(repo, manifest); err != nil {
			plan.Findings = append(plan.Findings, projectFinding(manifest.Path, "manifest", err.Error()))
			continue
		}
		plan.Manifests = append(plan.Manifests, manifest.Path)
	}
	if len(plan.Manifests) == 0 {
		return plan
	}
	if err := ToolProblem(repo); err != nil {
		plan.Findings = append(plan.Findings, policy.Finding{Check: "policy.tool", Path: manifests[0], Subject: "rust", Message: err.Error()})
		return plan
	}
	plan.Commands = append(plan.Commands, Command(repo, "policy-rust-version", "rustc", "--version"), Command(repo, "policy-cargo-version", "cargo", "--version"))
	for _, manifest := range plan.Manifests {
		plan.Commands = append(plan.Commands, Command(repo, manifestCommandName("policy-cargo-metadata", manifest), "cargo", "metadata", "--format-version", "1", "--no-deps", "--frozen", "--manifest-path", manifest))
	}
	return plan
}

func Discover(ctx context.Context, repo repository.Repository, plan DiscoveryPlan, commandRunner runner.Runner) Discovery {
	result := Discovery{Findings: slices.Clone(plan.Findings)}
	if len(plan.Commands) == 0 {
		return result
	}
	seen := map[string]bool{}
	for index, command := range plan.Commands {
		manifest := plan.Manifests[max(0, index-2)]
		_, output, err := runner.RunCaptured(ctx, commandRunner, repo.Root, command, runner.CaptureStructured)
		if err != nil {
			result.Findings = append(result.Findings, projectFinding(manifest, command.Name, fmt.Sprintf("Cargo discovery failed: %v: %s", err, diagnosticOutput(output.Stderr))))
			if index < 2 || ctx.Err() != nil {
				break
			}
			continue
		}
		if index < 2 {
			if !validToolVersion(output.Stdout, command, repo.ToolPin("rust")) {
				result.Findings = append(result.Findings, projectFinding(plan.Manifests[0], command.Name, "Rust tool version does not match the governed release"))
				break
			}
			continue
		}
		workspace, err := ParseWorkspace(repo, manifest, output.Stdout)
		if err != nil {
			result.Findings = append(result.Findings, projectFinding(manifest, "metadata", err.Error()))
			continue
		}
		if !seen[workspace.Root] {
			seen[workspace.Root] = true
			result.Workspaces = append(result.Workspaces, workspace)
		}
	}
	return result
}

func selectedManifests(selected, allFiles []string) []string {
	available := map[string]bool{}
	for _, path := range allFiles {
		if filepath.Base(path) == "Cargo.toml" {
			available[path] = true
		}
	}
	chosen := map[string]bool{}
	for _, path := range selected {
		if available[path] {
			chosen[path] = true
			continue
		}
		if filepath.Ext(path) != ".rs" || filepath.IsAbs(path) {
			continue
		}
		if manifest := nearestManifest(path, available); manifest != "" {
			chosen[manifest] = true
		}
	}
	result := make([]string, 0, len(chosen))
	for path := range chosen {
		result = append(result, path)
	}
	slices.Sort(result)
	return result
}

func nearestManifest(path string, available map[string]bool) string {
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		manifest := filepath.ToSlash(filepath.Join(parent, "Cargo.toml"))
		if available[manifest] {
			return manifest
		}
		if parent == "." || filepath.Dir(parent) == parent {
			return ""
		}
	}
}

func validToolVersion(output []byte, command policy.Command, pin string) bool {
	fields := strings.Fields(string(output))
	name := strings.TrimSuffix(filepath.Base(command.Argv[0]), ".exe")
	return len(fields) >= 2 && fields[0] == name && fields[1] == pin
}

func diagnosticOutput(data []byte) string {
	if len(data) > 4096 {
		data = data[:4096]
	}
	return strings.TrimSpace(string(data))
}

func projectFinding(path, subject, message string) policy.Finding {
	return policy.Finding{Check: "policy.rustProject", Path: path, Subject: subject, Message: message}
}
