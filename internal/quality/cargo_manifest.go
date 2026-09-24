package quality

import (
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func cargoManifestFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, problem := range repo.InspectCargoManifests(files).Problems {
		findings = append(findings, policy.Finding{
			Check: "policy.rustProject", Path: problem.Path, Line: problem.Line,
			Subject: "manifest", Message: problem.Message,
		})
	}
	return findings
}
