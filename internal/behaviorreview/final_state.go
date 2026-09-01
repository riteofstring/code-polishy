package behaviorreview

import (
	"context"
	"fmt"
	pathpkg "path"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/finalstate"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func buildFinalStateEvidence(repo repository.Repository, base, candidate string, delta repository.CandidateDelta) (finalstate.Evidence, error) {
	deleted := make(map[string]bool, len(delta.Deleted))
	for _, path := range delta.Deleted {
		deleted[path] = true
	}
	inputs := make([]finalstate.BuildInput, 0, len(delta.Paths()))
	for _, path := range delta.Paths() {
		patch, err := repo.Patch(base, candidate, []string{path})
		if err != nil {
			return finalstate.Evidence{}, operational("build final-state patch evidence", err)
		}
		var source []byte
		if !deleted[path] {
			source, _ = repo.ReadAt(candidate, path)
		}
		inputs = append(inputs, finalstate.BuildInput{Path: path, Role: finalStatePathRole(repo, path), Patch: patch, Source: source})
	}
	evidence, err := finalstate.Build(inputs)
	if err != nil {
		return finalstate.Evidence{}, operational("build final-state evidence", err)
	}
	return evidence, nil
}

func finalStatePathRole(repo repository.Repository, path string) finalstate.PathRole {
	if repo.IsGenerated(path) {
		return finalstate.RoleGenerated
	}
	if slices.Contains(repo.Config.Documentation.ProductInputs, path) {
		return finalstate.RoleProductInput
	}
	if finalStateFixturePath(path) {
		return finalstate.RoleFixture
	}
	if repo.IsTest(path) {
		return finalstate.RoleTest
	}
	if strings.HasPrefix(strings.ToLower(path), "docs/plans/") {
		return finalstate.RolePlan
	}
	name := strings.ToLower(pathpkg.Base(path))
	if strings.HasPrefix(name, "changelog") || strings.HasPrefix(name, "release-notes") {
		return finalstate.RoleChangelog
	}
	if policy.IsMarkdownPath(path) {
		return finalstate.RoleDocumentation
	}
	return finalstate.RoleProductionSource
}

func finalStateFixturePath(path string) bool {
	for _, segment := range strings.Split(strings.ToLower(path), "/") {
		if segment == "fixture" || segment == "fixtures" || segment == "testdata" || segment == "__fixtures__" {
			return true
		}
	}
	return false
}

func validateReviewFinalState(review ReviewResult, packet reviewPacket) error {
	intentIDs := make([]string, 0, len(packet.Intents))
	for _, intent := range packet.Intents {
		intentIDs = append(intentIDs, intent.ID)
	}
	if err := finalstate.ValidateFindings(review.FinalStateFindings, packet.FinalState, intentIDs); err != nil {
		return fmt.Errorf("%w: final-state findings are invalid: %v", ErrInvalidReview, err)
	}
	return nil
}

func finalStateFindings(ctx context.Context, repo repository.Repository, options ValidateGateReceiptOptions) ([]finalstate.Finding, error) {
	state, err := currentPreparedPacket(reviewContext(ctx), repo, options)
	if err != nil {
		return nil, err
	}
	defer state.root.Close()
	data, err := state.root.readRegularUTF8(defaultResultFilename, maximumResultBytes, true, "behavior review result")
	if err != nil {
		return nil, err
	}
	var review ReviewResult
	if err := decodeStrict(data, &review); err != nil {
		return nil, err
	}
	if err := validateReviewResult(review); err != nil {
		return nil, err
	}
	if !reviewMatchesPacket(review, state.packet, state.candidate) {
		return nil, fmt.Errorf("%w: result identifiers do not match the prepared packet", ErrStaleReview)
	}
	if err := validateReviewFinalState(review, state.packet); err != nil {
		return nil, err
	}
	return cloneFinalStateFindings(review.FinalStateFindings), nil
}

func currentPreparedPacket(ctx context.Context, repo repository.Repository, options ValidateGateReceiptOptions) (finalizationState, error) {
	if err := ctx.Err(); err != nil {
		return finalizationState{}, operational("inspect final-state review", err)
	}
	selection, err := NormalizeReviewSelection(options.Selection)
	if err != nil {
		return finalizationState{}, err
	}
	base, candidate, err := cleanCandidateAtMergeBase(repo, options.Base)
	if err != nil {
		return finalizationState{}, err
	}
	root, err := existingBehaviorReviewRoot(repo)
	if err != nil {
		return finalizationState{}, err
	}
	prepared, err := preparedPacketFor(repo, root, base, candidate)
	if err != nil {
		_ = root.Close()
		return finalizationState{}, err
	}
	if !sameReviewSelection(prepared.packet.Selection, selection) {
		_ = root.Close()
		return finalizationState{}, fmt.Errorf("%w: prepared selection differs from the current decision", ErrStaleReview)
	}
	return finalizationState{root: root, base: base, candidate: candidate, packet: prepared.packet}, nil
}

func cloneFinalStateFindings(findings []finalstate.Finding) []finalstate.Finding {
	result := make([]finalstate.Finding, len(findings))
	for index, finding := range findings {
		result[index] = finding
		result[index].IntentIDs = slices.Clone(finding.IntentIDs)
	}
	return result
}
