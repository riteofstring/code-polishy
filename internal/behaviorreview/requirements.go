package behaviorreview

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

type taskRequirementMaterial struct {
	ID             string   `json:"id"`
	TaskBase       string   `json:"task_base"`
	IntentIDs      []string `json:"intent_ids"`
	IntentSHA256   string   `json:"intent_sha256"`
	Features       []string `json:"features"`
	PreviousSHA256 string   `json:"previous_requirement_sha256"`
}

func require(ctx context.Context, repo repository.Repository, options RequireOptions) (RequireResult, error) {
	ctx = reviewContext(ctx)
	if err := ctx.Err(); err != nil {
		return RequireResult{}, operational("require behavior review features", err)
	}
	if strings.TrimSpace(options.Base) == "" {
		return RequireResult{}, fmt.Errorf("%w: task base is required", ErrInvalidInput)
	}
	features, err := configuredFeatureNames(repo.Config, options.Features)
	if err != nil {
		return RequireResult{}, err
	}
	if len(features) == 0 {
		return RequireResult{}, fmt.Errorf("%w: at least one behavior review feature is required", ErrInvalidInput)
	}
	candidate, err := repo.CleanHead()
	if err != nil {
		return RequireResult{}, err
	}
	taskBase, err := repo.ResolveAncestor(options.Base)
	if err != nil {
		return RequireResult{}, err
	}
	root, err := behaviorReviewRoot(repo)
	if err != nil {
		return RequireResult{}, err
	}
	defer root.Close()
	release, err := lockIntentJournal(root)
	if err != nil {
		return RequireResult{}, err
	}
	defer release()
	journal, err := readIntentJournal(repo, root, false)
	if err != nil {
		return RequireResult{}, err
	}
	intents, intentSHA256, err := selectIntentCaptures(repo, journal, taskBase, candidate)
	if err != nil {
		return RequireResult{}, err
	}
	requirement, err := newTaskRequirement(taskBase, intents, features, journal.Requirements)
	if err != nil {
		return RequireResult{}, err
	}
	journal.Requirements = append(journal.Requirements, requirement)
	if err := validateIntentJournal(repo, journal); err != nil {
		return RequireResult{}, err
	}
	data, err := marshalArtifact(journal)
	if err != nil {
		return RequireResult{}, err
	}
	if len(data) > maximumIntentJournal {
		return RequireResult{}, fmt.Errorf("%w: intent journal exceeds %d bytes", ErrInvalidInput, maximumIntentJournal)
	}
	current, err := repo.CleanHead()
	if err != nil {
		return RequireResult{}, err
	}
	if current != candidate {
		return RequireResult{}, fmt.Errorf("%w: candidate changed while behavior review features were required", ErrCandidateChanged)
	}
	if err := root.writeArtifactAtomic(intentJournalFilename, data); err != nil {
		return RequireResult{}, err
	}
	return RequireResult{
		ID: requirement.ID, Base: taskBase, Candidate: candidate, IntentSHA256: intentSHA256,
		RequirementSHA256: requirement.EntrySHA256, JournalPath: artifactDisplayPath(intentJournalFilename), Features: slices.Clone(features),
	}, nil
}

func taskRequirements(ctx context.Context, repo repository.Repository, base string) (TaskRequirementsResult, error) {
	ctx = reviewContext(ctx)
	if err := ctx.Err(); err != nil {
		return TaskRequirementsResult{}, operational("read behavior review task requirements", err)
	}
	if strings.TrimSpace(base) == "" {
		return TaskRequirementsResult{}, fmt.Errorf("%w: task requirements base is required", ErrInvalidInput)
	}
	resolvedBase, candidate, err := taskRequirementReadIdentity(repo, base)
	if err != nil {
		return TaskRequirementsResult{}, err
	}
	root, err := existingBehaviorReviewRoot(repo)
	if errors.Is(err, ErrMissingReceipt) {
		return emptyTaskRequirements(resolvedBase, candidate)
	}
	if err != nil {
		return TaskRequirementsResult{}, err
	}
	defer root.Close()
	journal, err := readIntentJournal(repo, root, true)
	if err != nil {
		return TaskRequirementsResult{}, err
	}
	if len(journal.Entries) == 0 {
		return emptyTaskRequirements(resolvedBase, candidate)
	}
	return taskRequirementsFor(repo, journal, resolvedBase, candidate)
}

func taskRequirementReadIdentity(repo repository.Repository, reference string) (string, string, error) {
	base, err := repo.MergeBase(reference)
	if err != nil {
		return "", "", err
	}
	candidate, err := repo.MergeBase("HEAD")
	if err != nil {
		return "", "", err
	}
	return base, candidate, nil
}

func emptyTaskRequirements(base, candidate string) (TaskRequirementsResult, error) {
	digest, err := taskRequirementsDigest([]TaskRequirement{})
	if err != nil {
		return TaskRequirementsResult{}, err
	}
	return TaskRequirementsResult{Base: base, Candidate: candidate, Requirements: []TaskRequirement{}, RequestedFeatures: []string{}, SHA256: digest}, nil
}

func taskRequirementsFor(repo repository.Repository, journal intentJournal, base, candidate string) (TaskRequirementsResult, error) {
	if len(journal.Entries) == 0 {
		return emptyTaskRequirements(base, candidate)
	}
	selected := make([]TaskRequirement, 0, len(journal.Requirements))
	for _, requirement := range journal.Requirements {
		afterBase, err := repo.IsAncestor(base, requirement.TaskBase)
		if err != nil {
			return TaskRequirementsResult{}, operational("select behavior review task requirements", err)
		}
		if !afterBase {
			continue
		}
		beforeCandidate, err := repo.IsAncestor(requirement.TaskBase, candidate)
		if err != nil {
			return TaskRequirementsResult{}, operational("select behavior review task requirements", err)
		}
		if !beforeCandidate {
			continue
		}
		intents, err := requirementIntents(repo, journal.Entries, requirement)
		if err != nil {
			return TaskRequirementsResult{}, err
		}
		included := true
		for _, intent := range intents {
			ancestor, err := repo.IsAncestor(intent.CapturedAtCommit, candidate)
			if err != nil {
				return TaskRequirementsResult{}, operational("select behavior review task requirements", err)
			}
			if !ancestor {
				included = false
				break
			}
		}
		if included {
			selected = append(selected, cloneTaskRequirement(requirement))
		}
	}
	requested := requestedRequirementFeatures(selected)
	digest, err := taskRequirementsDigest(selected)
	if err != nil {
		return TaskRequirementsResult{}, err
	}
	return TaskRequirementsResult{Base: base, Candidate: candidate, Requirements: selected, RequestedFeatures: requested, SHA256: digest}, nil
}

func configuredFeatureNames(config policy.Config, values []string) ([]string, error) {
	features := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !validFeatureName(value) {
			return nil, fmt.Errorf("%w: behavior review feature %q is malformed", ErrInvalidInput, value)
		}
		if _, err := ConfiguredFeatureSelection(config, value, []string{SelectionReasonTaskRequested}); err != nil {
			return nil, err
		}
		if !seen[value] {
			seen[value] = true
			features = append(features, value)
		}
	}
	sort.Strings(features)
	return features, nil
}

func newTaskRequirement(taskBase string, intents []intentCapture, features []string, requirements []TaskRequirement) (TaskRequirement, error) {
	if len(requirements) >= maximumTaskRequirements {
		return TaskRequirement{}, fmt.Errorf("%w: task feature requirements exceed %d entries", ErrInvalidInput, maximumTaskRequirements)
	}
	if !validRevision(taskBase) || len(intents) == 0 || len(features) == 0 {
		return TaskRequirement{}, fmt.Errorf("%w: task feature requirement inputs are invalid", ErrInvalidInput)
	}
	intentIDs := make([]string, 0, len(intents))
	for _, intent := range intents {
		intentIDs = append(intentIDs, intent.ID)
	}
	intentSHA256, err := intentSelectionDigest(intents)
	if err != nil {
		return TaskRequirement{}, err
	}
	previous := ""
	if len(requirements) > 0 {
		previous = requirements[len(requirements)-1].EntrySHA256
	}
	id, err := newTaskRequirementID()
	if err != nil {
		return TaskRequirement{}, operational("generate behavior review task requirement identifier", err)
	}
	requirement := TaskRequirement{
		ID: id, TaskBase: taskBase, IntentIDs: intentIDs, IntentSHA256: intentSHA256, Features: slices.Clone(features), PreviousSHA256: previous,
	}
	requirement.EntrySHA256, err = taskRequirementDigest(requirement)
	if err != nil {
		return TaskRequirement{}, err
	}
	return requirement, nil
}

func newTaskRequirementID() (string, error) {
	data := make([]byte, 16)
	if _, err := cryptorand.Read(data); err != nil {
		return "", err
	}
	return fmt.Sprintf("requirement-%x", data), nil
}

func taskRequirementDigest(requirement TaskRequirement) (string, error) {
	material := taskRequirementMaterial{
		ID: requirement.ID, TaskBase: requirement.TaskBase, IntentIDs: requirement.IntentIDs, IntentSHA256: requirement.IntentSHA256,
		Features: requirement.Features, PreviousSHA256: requirement.PreviousSHA256,
	}
	data, err := json.Marshal(material)
	if err != nil {
		return "", operational("encode behavior review task requirement", err)
	}
	return sha256Hex(data), nil
}

func taskRequirementsDigest(requirements []TaskRequirement) (string, error) {
	data, err := json.Marshal(requirements)
	if err != nil {
		return "", operational("encode behavior review task requirements", err)
	}
	return sha256Hex(data), nil
}

func validateTaskRequirements(repo repository.Repository, journal intentJournal) error {
	if journal.Requirements == nil || len(journal.Requirements) > maximumTaskRequirements {
		return fmt.Errorf("%w: task feature requirements are invalid", ErrInvalidInput)
	}
	seen := map[string]bool{}
	previous := ""
	for index, requirement := range journal.Requirements {
		if err := validateTaskRequirement(repo, journal.Entries, requirement, previous, seen); err != nil {
			return fmt.Errorf("%w: task feature requirement %d is invalid: %v", ErrInvalidInput, index+1, err)
		}
		seen[requirement.ID] = true
		previous = requirement.EntrySHA256
	}
	return nil
}

func validateTaskRequirement(repo repository.Repository, entries []intentCapture, requirement TaskRequirement, expectedPrevious string, seen map[string]bool) error {
	if !allValid(
		validIdentifier(requirement.ID), !seen[requirement.ID], validRevision(requirement.TaskBase), validSHA256(requirement.IntentSHA256),
		requirement.PreviousSHA256 == expectedPrevious, validSHA256(requirement.EntrySHA256),
	) {
		return errors.New("identifiers are malformed")
	}
	if err := validateTaskRequirementFeatures(requirement.Features); err != nil {
		return err
	}
	intents, err := requirementIntents(repo, entries, requirement)
	if err != nil {
		return err
	}
	digest, err := intentSelectionDigest(intents)
	if err != nil || digest != requirement.IntentSHA256 {
		return errors.New("intent binding is invalid")
	}
	entryDigest, err := taskRequirementDigest(requirement)
	if err != nil || entryDigest != requirement.EntrySHA256 {
		return errors.New("entry digest is invalid")
	}
	return nil
}

func validateTaskRequirementFeatures(features []string) error {
	if len(features) == 0 {
		return errors.New("feature selection is empty")
	}
	previous := ""
	for _, feature := range features {
		if !validFeatureName(feature) || feature <= previous {
			return errors.New("feature selection is invalid")
		}
		previous = feature
	}
	return nil
}

func requirementIntents(repo repository.Repository, entries []intentCapture, requirement TaskRequirement) ([]intentCapture, error) {
	if len(requirement.IntentIDs) == 0 {
		return nil, errors.New("intent references are empty")
	}
	byID := make(map[string]int, len(entries))
	for index, entry := range entries {
		byID[entry.ID] = index
	}
	selected := make([]intentCapture, 0, len(requirement.IntentIDs))
	previousIndex := -1
	for _, id := range requirement.IntentIDs {
		index, ok := byID[id]
		if !ok || index <= previousIndex {
			return nil, errors.New("intent references are invalid")
		}
		entry := entries[index]
		ancestor, err := repo.IsAncestor(requirement.TaskBase, entry.CapturedAtCommit)
		if err != nil {
			return nil, operational("validate task requirement intent ancestry", err)
		}
		if !ancestor {
			return nil, errors.New("intent references precede the task base")
		}
		selected = append(selected, entry)
		previousIndex = index
	}
	if selected[0].CapturedAtCommit != requirement.TaskBase {
		return nil, errors.New("first intent does not match the task base")
	}
	return selected, nil
}

func requestedRequirementFeatures(requirements []TaskRequirement) []string {
	set := map[string]bool{}
	for _, requirement := range requirements {
		for _, feature := range requirement.Features {
			set[feature] = true
		}
	}
	features := make([]string, 0, len(set))
	for feature := range set {
		features = append(features, feature)
	}
	sort.Strings(features)
	return features
}

func cloneTaskRequirement(requirement TaskRequirement) TaskRequirement {
	return TaskRequirement{
		ID: requirement.ID, TaskBase: requirement.TaskBase, IntentIDs: slices.Clone(requirement.IntentIDs), IntentSHA256: requirement.IntentSHA256,
		Features: slices.Clone(requirement.Features), PreviousSHA256: requirement.PreviousSHA256, EntrySHA256: requirement.EntrySHA256,
	}
}

func cloneTaskRequirements(requirements []TaskRequirement) []TaskRequirement {
	cloned := make([]TaskRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		cloned = append(cloned, cloneTaskRequirement(requirement))
	}
	return cloned
}

func sameTaskRequirements(left, right []TaskRequirement) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].ID != right[index].ID || left[index].TaskBase != right[index].TaskBase ||
			left[index].IntentSHA256 != right[index].IntentSHA256 || left[index].PreviousSHA256 != right[index].PreviousSHA256 ||
			left[index].EntrySHA256 != right[index].EntrySHA256 || !slices.Equal(left[index].IntentIDs, right[index].IntentIDs) ||
			!slices.Equal(left[index].Features, right[index].Features) {
			return false
		}
	}
	return true
}
