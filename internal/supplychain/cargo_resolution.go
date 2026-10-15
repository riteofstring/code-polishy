package supplychain

import (
	"context"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func CargoResolutionCommands(repo repository.Repository, files []string, offline bool) []policy.Command {
	return rustfacts.PlanResolution(repo, files, offline).Commands
}

func CheckCargoResolution(ctx context.Context, repo repository.Repository, files []string, offline bool, commandRunner runner.Runner) []policy.Finding {
	plan := rustfacts.PlanResolution(repo, files, offline)
	return rustfacts.Resolve(ctx, repo, plan, commandRunner).Findings
}

func onlineCargoFindings(ctx context.Context, repo repository.Repository, files []string, commands []policy.Command, commandRunner runner.Runner, findings []policy.Finding, index int) ([]policy.Finding, int) {
	plan := rustfacts.PlanResolution(repo, files, false)
	count := len(plan.Commands)
	plan.Commands = commands[index : index+count]
	result := rustfacts.Resolve(ctx, repo, plan, commandRunner)
	findings = append(findings, result.Findings...)
	locks := []repository.CargoLock{}
	for _, resolution := range result.Resolutions {
		locks = append(locks, resolution.Lock)
		findings = append(findings, cargoAdvisoryScopeFindings(repo, files, resolution)...)
	}
	findings = append(findings, cargoReleaseAgeFindings(ctx, repo, files, locks)...)
	return findings, index + count
}

func cargoAdvisoryScopeFindings(repo repository.Repository, files []string, resolution rustfacts.Resolution) []policy.Finding {
	if !osvCoversScope(repo.Config, resolution.Manifest) || osvCoversScope(repo.Config, resolution.Lock.Path) {
		return nil
	}
	for _, pkg := range resolution.Lock.Packages {
		if pkg.Source != repository.CargoCratesIOSource {
			continue
		}
		findings := manifestCapabilityFindings(repo, files, resolution.Manifest, "cargo", []string{"security"})
		for index := range findings {
			findings[index].Path = resolution.Lock.Path
			findings[index].Message = "the resolved Cargo workspace lock is outside the active OSV scope; cover the workspace lock with OSV or a governed security provider"
		}
		return findings
	}
	return nil
}
