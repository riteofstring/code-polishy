package supplychain

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
	workflowfacts "github.com/riteofstring/code-polishy/internal/workflow"
)

const nodeReleaseScheduleEndpoint = "https://raw.githubusercontent.com/nodejs/Release/main/schedule.json"

func nodeRuntimeInventory(repo repository.Repository) map[string]string {
	inventory := map[string]string{}
	for _, artifact := range repo.Config.SupplyChain.ReleaseArtifacts {
		if artifact.Source == "node-runtime" {
			inventory[artifact.VersionFile], _ = readReleaseArtifactVersion(repo, artifact)
		}
	}
	return inventory
}

func nodeRuntimeCoverageFindings(repo repository.Repository, files []string) []policy.Finding {
	inventory := nodeRuntimeInventory(repo)
	findings := []policy.Finding{}
	for _, path := range files {
		if name := filepath.Base(path); name == ".node-version" || name == ".nvmrc" {
			if _, exists := inventory[path]; !exists {
				findings = append(findings, nodeRuntimeCoverageFinding(path, "declare this Node pin in supplyChain.releaseArtifacts with source node-runtime so release age and upstream support are checked"))
			}
		}
	}
	return append(findings, nodeWorkflowCoverageFindings(repo, files, inventory)...)
}

func nodeWorkflowCoverageFindings(repo repository.Repository, files []string, inventory map[string]string) []policy.Finding {
	findings := []policy.Finding{}
	for _, path := range securityWorkflowPaths(files) {
		facts, err := workflowfacts.Read(path, repo.Read)
		if err != nil {
			continue
		}
		for _, job := range facts.Jobs {
			for _, step := range job.Steps {
				if strings.HasPrefix(strings.ToLower(step.Uses), "actions/setup-node@") {
					findings = append(findings, setupNodeCoverageFindings(path, step.Inputs, inventory)...)
				}
			}
		}
	}
	return findings
}

func nodeRuntimeCoverageFinding(path, message string) policy.Finding {
	return policy.Finding{Check: "supplyChain.runtimeCoverage", Path: path, Subject: "node-runtime", Message: message}
}

func setupNodeCoverageFindings(path string, inputs, inventory map[string]string) []policy.Finding {
	if version := inputs["node-version"]; version != "" {
		for _, declared := range inventory {
			if declared != "" && version == declared {
				return nil
			}
		}
		return []policy.Finding{nodeRuntimeCoverageFinding(path, fmt.Sprintf("setup-node version %q must be an exact version covered by a node-runtime release artifact; use node-version-file to reference its versionFile directly", version))}
	}
	pathInput := inputs["node-version-file"]
	if version, exists := inventory[pathInput]; exists && version != "" {
		return nil
	}
	return []policy.Finding{nodeRuntimeCoverageFinding(path, "setup-node must select a declared node-runtime versionFile or an exact inventoried node-version; dynamic and ambient runtime selection cannot prove coverage")}
}

func nodeRuntimeSupportFindings(ctx context.Context, repo repository.Repository, client artifactHTTPClient, now time.Time) []policy.Finding {
	pins := nodeRuntimeInventory(repo)
	for path, version := range pins {
		if version == "" {
			delete(pins, path)
		}
	}
	if len(pins) == 0 {
		return nil
	}
	var schedule map[string]nodeLifecycle
	err := fetchArtifactMetadata(ctx, client, nodeReleaseScheduleEndpoint, &schedule)
	findings := []policy.Finding{}
	for path, version := range pins {
		message := ""
		if err != nil {
			message = "resolve Node support schedule: " + err.Error()
		} else {
			message = nodeLifecycleMessage(schedule, version, now)
		}
		if message != "" {
			findings = append(findings, policy.Finding{Check: "supplyChain.runtimeSupport", Path: path, Subject: "node@" + version, Message: message})
		}
	}
	return uniqueFindings(findings)
}

type nodeLifecycle struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

func nodeLifecycleMessage(schedule map[string]nodeLifecycle, version string, now time.Time) string {
	major, _, _ := strings.Cut(version, ".")
	entry := schedule["v"+major]
	start, startErr := time.Parse(time.DateOnly, entry.Start)
	end, endErr := time.Parse(time.DateOnly, entry.End)
	switch {
	case startErr != nil || endErr != nil || !end.After(start):
		return "upstream support schedule has no valid lifecycle for Node " + major
	case now.Before(start):
		return "Node " + major + " has not entered upstream support"
	case !now.Before(end):
		return "Node " + major + " reached end of life on " + entry.End + "; select a reviewed release from a supported line"
	}
	return ""
}
