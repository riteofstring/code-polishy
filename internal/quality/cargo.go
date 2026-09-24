package quality

import (
	"context"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	"github.com/riteofstring/code-polishy/internal/runner"
	"github.com/riteofstring/code-polishy/internal/rustfacts"
)

func cargoDiscoveryFindings(ctx context.Context, repo repository.Repository, files []string, commandRunner runner.Runner) []policy.Finding {
	allFiles, err := repo.AllFiles()
	if err != nil {
		return []policy.Finding{{Check: "policy.rustProject", Path: "repository", Subject: "inventory", Message: err.Error()}}
	}
	plan := rustfacts.PlanDiscovery(repo, files, allFiles)
	return rustfacts.Discover(ctx, repo, plan, commandRunner).Findings
}
