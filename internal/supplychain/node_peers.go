package supplychain

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/riteofstring/code-polishy/internal/javascript"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func nodePeerRangeFindings(ctx context.Context, repo repository.Repository, files []string) []policy.Finding {
	paths := []string{}
	for _, path := range files {
		if filepath.Base(path) != "package.json" {
			continue
		}
		data, err := repo.Read(path)
		var payload packageJSON
		if err == nil && json.Unmarshal(data, &payload) == nil && len(payload.PeerDependencies) > 0 {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	result, err := javascript.Bundle{PolicyRoot: repo.PolicyRoot}.Peers(ctx, repo.Root, paths)
	if err != nil {
		return []policy.Finding{{Check: "policy.tool", Path: paths[0], Subject: "javascript-bundle:peers", Message: err.Error()}}
	}
	findings := []policy.Finding{}
	for _, finding := range result.Findings {
		findings = append(findings, policy.Finding{Check: "supplyChain.peerDependency", Path: finding.Path, Subject: finding.Name, Message: finding.Message})
	}
	for _, unsupported := range result.Unsupported {
		findings = append(findings, policy.Finding{Check: "supplyChain.peerDependency", Path: unsupported.Path, Subject: "range coverage", Message: unsupported.Reason})
	}
	return findings
}
