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
	inputs, err := onlineUVInputs(repo)
	if err != nil {
		return GitEvidenceState{}, err
	}
	state := GitEvidenceState{Receipts: []GitEvidenceReceipt{}}
	bindings := []gitEvidenceBinding{}
	for _, input := range inputs {
		if !gitLockPackages(input.Packages) {
			continue
		}
		findings, receipt := verifyGitLockEvidence(repo, input.Scope, input.Packages, now)
		binding := gitEvidenceBinding{Scope: input.Scope, InventorySHA256: gitEvidenceInventoryDigest(input.Packages), Findings: findings, Receipt: receipt}
		binding.ArtifactSHA256, binding.ArtifactStatus = gitEvidenceArtifactIdentity(repo, input.Scope)
		if receipt != nil {
			state.Receipts = append(state.Receipts, *receipt)
			identityReceipt := *receipt
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

func gitEvidenceArtifactIdentity(repo repository.Repository, scope string) (string, string) {
	declaration, _, err := configuredGitEvidence(repo.Config.SupplyChain.GitEvidence, scope)
	if err != nil {
		return "", "unavailable"
	}
	data, err := readGitEvidenceFile(repo, declaration.Path, maximumGitEvidenceBytes)
	if err != nil {
		return "", gitEvidenceFinding(scope, err).Subject
	}
	return gitEvidenceDigest(data), "read"
}
