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
	return append(findings, result.Findings...), index + count
}
