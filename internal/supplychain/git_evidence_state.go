package supplychain

import (
	"encoding/json"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type GitEvidenceState struct {
	IdentitySHA256 string
	Receipts       []GitEvidenceReceipt
}

type gitEvidenceBinding struct {
	Scope           string              `json:"scope"`
	InventorySHA256 string              `json:"inventorySha256"`
	ArtifactSHA256  string              `json:"artifactSha256"`
	ArtifactStatus  string              `json:"artifactStatus"`
	Findings        []policy.Finding    `json:"findings"`
	Receipt         *GitEvidenceReceipt `json:"receipt"`
}

func ReadGitEvidenceState(repo repository.Repository, now time.Time) (GitEvidenceState, error) {
	if !repo.Config.SupplyChain.GitEvidence.Required {
		return GitEvidenceState{Receipts: []GitEvidenceReceipt{}}, nil
	}
	inputs, err := resolvedLockInputs(repo, map[string]func([]byte, string) ([]resolvedPackage, error){"uv.lock": parseUVLock, "Cargo.lock": osvCargoInventory})
	if err != nil {
		return GitEvidenceState{}, err
	}
	state := GitEvidenceState{Receipts: []GitEvidenceReceipt{}}
	bindings := []gitEvidenceBinding{}
	for _, input := range inputs {
		if !gitLockPackages(input.Packages) {
			continue
		}
		assessment := assessRequiredGitLock(repo, input.Scope, input.Packages, now)
		binding := gitEvidenceBinding{Scope: input.Scope, InventorySHA256: assessment.InventorySHA256, Findings: assessment.Findings, Receipt: assessment.Receipt, ArtifactSHA256: assessment.ArtifactSHA256, ArtifactStatus: assessment.ArtifactStatus}
		if assessment.Receipt != nil {
			state.Receipts = append(state.Receipts, *assessment.Receipt)
			identityReceipt := *assessment.Receipt
			identityReceipt.RetrievedAt = time.Time{}
			binding.Receipt = &identityReceipt
		}
		bindings = append(bindings, binding)
	}
	if len(bindings) == 0 {
		return state, nil
	}
	data, err := json.Marshal(bindings)
	if err != nil {
		return GitEvidenceState{}, err
	}
	state.IdentitySHA256 = gitEvidenceDigest(data)
	return state, nil
}
