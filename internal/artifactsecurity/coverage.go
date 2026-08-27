package artifactsecurity

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func CoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	findings := []policy.Finding{}
	for _, target := range repo.Config.SupplyChain.ArtifactSecurity.Targets {
		for label, path := range map[string]string{
			"dockerfile": target.Dockerfile, "archive": target.Archive, "openVex": target.OpenVEX,
		} {
			if path != "" && !slices.Contains(files, path) {
				findings = append(findings, policy.Finding{
					Check: "policy.artifactSecurityCoverage", Path: policy.ConfigFilename, Subject: target.Name + ":" + label,
					Message: fmt.Sprintf("artifact target references missing governed file %s", path),
				})
			}
		}
	}
	return findings
}

func ToolFindings(repo repository.Repository) []policy.Finding {
	if len(repo.Config.SupplyChain.ArtifactSecurity.Targets) == 0 {
		return nil
	}
	findings := []policy.Finding{}
	if _, err := exec.LookPath("docker"); err != nil {
		findings = append(findings, policy.Finding{
			Check: "policy.tool", Path: "repository", Subject: "docker",
			Message: "artifact security requires the Docker CLI and a reachable Docker server",
		})
	}
	if _, err := loadScannerPolicy(repo.PolicyRoot); err != nil {
		findings = append(findings, policy.Finding{
			Check: "policy.tool", Path: "repository", Subject: "trivy",
			Message: "policy-owned Trivy scanner inputs are invalid: " + boundedText(err.Error(), 1024),
		})
	}
	for _, target := range repo.Config.SupplyChain.ArtifactSecurity.Targets {
		if target.Mode != "command" || target.Producer == nil {
			continue
		}
		executable := target.Producer.Argv[0]
		if strings.ContainsAny(executable, "/\\") {
			if _, err := containedRepositoryPath(repo, executable); err != nil {
				findings = append(findings, policy.Finding{Check: "policy.tool", Path: policy.ConfigFilename, Subject: target.Name, Message: "artifact producer executable is unavailable or escapes the repository"})
			}
		} else if _, err := exec.LookPath(executable); err != nil {
			findings = append(findings, policy.Finding{Check: "policy.tool", Path: policy.ConfigFilename, Subject: target.Name, Message: "artifact producer executable is unavailable"})
		}
	}
	return findings
}
