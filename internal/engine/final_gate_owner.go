package engine

import (
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const finalGateCommandLiteral = "code-polishy merge-gate --base"

func finalGateOwnerFindings(repo repository.Repository, files []string) []policy.Finding {
	if repo.Config.Verification.EffectiveFinalGateOwner() != policy.FinalGateOwnerCI {
		return nil
	}
	for _, path := range files {
		if !finalGateWorkflowPath(repo, path) {
			continue
		}
		data, err := repo.Read(path)
		if err == nil && strings.Contains(string(data), finalGateCommandLiteral) {
			return nil
		}
	}
	return []policy.Finding{{
		Check: "policy.finalGateOwner", Path: receiptConfigurationPath(repo), Subject: "ci",
		Message: "verification.finalGateOwner is ci, but no checked-in GitHub or GitLab workflow contains the literal command `code-polishy merge-gate --base`",
	}}
}

func finalGateWorkflowPath(repo repository.Repository, path string) bool {
	normalized := filepath.ToSlash(path)
	extension := strings.ToLower(filepath.Ext(normalized))
	if extension != ".yml" && extension != ".yaml" {
		return false
	}
	if strings.HasPrefix(normalized, ".github/workflows/") || normalized == ".gitlab-ci.yml" || normalized == ".gitlab-ci.yaml" {
		return true
	}
	for _, control := range repo.DynamicControlInputs {
		if normalized == control {
			return true
		}
	}
	return false
}
