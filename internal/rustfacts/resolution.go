package rustfacts

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
)

type ResolutionPlan struct {
	Discovery DiscoveryPlan
	Commands  []policy.Command
}

type ResolutionResult struct {
	Resolutions []Resolution
	Findings    []policy.Finding
}

func PlanResolution(repo repository.Repository, files []string, offline bool) ResolutionPlan {
	discovery := PlanDiscovery(repo, files, files)
	plan := ResolutionPlan{Discovery: discovery, Commands: slices.Clone(discovery.Commands)}
	if len(plan.Commands) == 0 {
		return plan
	}
	for _, manifest := range discovery.Manifests {
		plan.Commands = append(plan.Commands, resolutionCommand(repo, manifest, offline))
	}
	return plan
}

func resolutionCommand(repo repository.Repository, manifest string, offline bool) policy.Command {
	mode := "--frozen"
	if !offline {
		mode = "--locked"
	}
	command := Command(repo, "policy-cargo-resolution-"+manifest, "cargo", "metadata", "--format-version", "1", mode, "--manifest-path", manifest)
	if !offline {
		index := slices.Index(command.EnvironmentOverrides, "CARGO_NET_OFFLINE=true")
		command.EnvironmentOverrides[index] = "CARGO_NET_OFFLINE=false"
	}
	return command
}

func Resolve(ctx context.Context, repo repository.Repository, plan ResolutionPlan, commandRunner runner.Runner) ResolutionResult {
	discovery := Discover(ctx, repo, plan.Discovery, commandRunner)
	result := ResolutionResult{Findings: discovery.Findings}
	for index, command := range plan.Commands[len(plan.Discovery.Commands):] {
		manifest := plan.Discovery.Manifests[index]
		workspace := workspaceForManifest(discovery.Workspaces, manifest)
		if workspace == nil {
			result.Findings = append(result.Findings, resolutionFinding(manifest, "Cargo lock verification requires valid workspace discovery"))
			continue
		}
		resolution, findings := resolveManifest(ctx, repo, manifest, *workspace, command, commandRunner)
		result.Findings = append(result.Findings, findings...)
		if len(findings) == 0 {
			result.Resolutions = append(result.Resolutions, resolution)
		}
	}
	return result
}

func resolveManifest(ctx context.Context, repo repository.Repository, manifest string, workspace Workspace, command policy.Command, commandRunner runner.Runner) (Resolution, []policy.Finding) {
	path := filepath.ToSlash(filepath.Join(workspace.Root, "Cargo.lock"))
	lock, err := repo.ReadCargoLock(path)
	if err != nil {
		return Resolution{}, []policy.Finding{resolutionFinding(path, fmt.Sprintf("Cargo lock verification requires a reviewed, contained lockfile: %v", err))}
	}
	structured, ok := commandRunner.(runner.StructuredRunner)
	if !ok {
		return Resolution{}, []policy.Finding{resolutionFinding(manifest, "the runner cannot capture bounded Cargo resolution metadata")}
	}
	_, output, err := structured.RunStructured(ctx, repo.Root, command)
	if err != nil {
		return Resolution{}, []policy.Finding{resolutionFinding(path, fmt.Sprintf("Cargo could not verify locked resolution: %v; %s", err, diagnosticOutput(output.Stderr)))}
	}
	resolution, err := ParseResolution(repo, manifest, output.Stdout)
	if err == nil {
		err = validateResolutionLock(resolution, lock)
	}
	if err != nil {
		return Resolution{}, []policy.Finding{resolutionFinding(path, fmt.Sprintf("invalid Cargo resolution evidence: %v", err))}
	}
	resolution.Lock = lock
	return resolution, nil
}

func resolutionFinding(path, message string) policy.Finding {
	return policy.Finding{Check: "supplyChain.cargoLock", Path: path, Subject: "resolved-graph", Message: message}
}
