package behaviorreview

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const (
	SelectionReasonTaskRequested     = "task-requested"
	SelectionReasonCandidateRequired = "candidate-required"
	SelectionReasonBaseRequired      = "base-required"
	SelectionReasonDefaultRequired   = "default-required"
)

func ConfiguredFeatureSelection(config policy.Config, name string, reasons []string) (SelectedFeature, error) {
	if !validFeatureName(name) {
		return SelectedFeature{}, fmt.Errorf("%w: behavior review feature %q is malformed", ErrInvalidInput, name)
	}
	behaviorReview := config.Verification.BehaviorReview
	if behaviorReview == nil {
		return SelectedFeature{}, fmt.Errorf("%w: behavior review feature %q is not configured", ErrInvalidInput, name)
	}
	for _, feature := range behaviorReview.Features {
		if feature.Name != name {
			continue
		}
		return normalizeSelectedFeature(SelectedFeature{
			Name:       feature.Name,
			Modules:    slices.Clone(feature.Modules),
			Paths:      slices.Clone(feature.Paths),
			Suites:     slices.Clone(feature.Suites),
			RequiredAt: behaviorReview.EffectiveRequiredAt(feature),
			Reasons:    slices.Clone(reasons),
		})
	}
	return SelectedFeature{}, fmt.Errorf("%w: behavior review feature %q is not configured", ErrInvalidInput, name)
}

func NormalizeReviewSelection(selection ReviewSelection) (ReviewSelection, error) {
	features, err := normalizeSelectionFeatures(selection.Features)
	if err != nil {
		return ReviewSelection{}, err
	}
	reasons, err := normalizeSelectionReasons(selection.FullCandidateReasons)
	if err != nil {
		return ReviewSelection{}, err
	}
	normalized := ReviewSelection{
		Features:             features,
		FullCandidate:        selection.FullCandidate,
		FullCandidateReasons: reasons,
	}
	if err := validateNormalizedReviewSelection(normalized); err != nil {
		return ReviewSelection{}, err
	}
	return normalized, nil
}

func normalizeSelectionFeatures(values []SelectedFeature) ([]SelectedFeature, error) {
	features := make([]SelectedFeature, 0, len(values))
	for _, feature := range values {
		normalized, err := normalizeSelectedFeature(feature)
		if err != nil {
			return nil, err
		}
		features = append(features, normalized)
	}
	sort.Slice(features, func(left, right int) bool { return features[left].Name < features[right].Name })
	if !uniqueSelectedFeatures(features) {
		return nil, fmt.Errorf("%w: selected behavior review features must be unique", ErrInvalidInput)
	}
	return features, nil
}

func uniqueSelectedFeatures(features []SelectedFeature) bool {
	for index := 1; index < len(features); index++ {
		if features[index-1].Name == features[index].Name {
			return false
		}
	}
	return true
}

func validateNormalizedReviewSelection(selection ReviewSelection) error {
	if len(selection.Features) == 0 && !selection.FullCandidate {
		return fmt.Errorf("%w: behavior review selection must contain a feature or the full candidate", ErrInvalidInput)
	}
	if !selection.FullCandidate && len(selection.FullCandidateReasons) != 0 {
		return fmt.Errorf("%w: full-candidate reasons require full-candidate review", ErrInvalidInput)
	}
	if selection.FullCandidate && len(selection.FullCandidateReasons) == 0 {
		return fmt.Errorf("%w: full-candidate review needs a selection reason", ErrInvalidInput)
	}
	return nil
}

func SelectionSHA256(selection ReviewSelection) (string, error) {
	normalized, err := NormalizeReviewSelection(selection)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", operational("encode behavior review selection", err)
	}
	return sha256Hex(data), nil
}

func DecisionBindingSHA256(selection ReviewSelection, requirements TaskRequirementsResult) (string, error) {
	selectionSHA256, err := SelectionSHA256(selection)
	if err != nil {
		return "", err
	}
	requirementSHA256, err := taskRequirementsDigest(requirements.Requirements)
	if err != nil {
		return "", err
	}
	if !validSHA256(requirements.SHA256) || requirementSHA256 != requirements.SHA256 {
		return "", fmt.Errorf("%w: task requirement snapshot digest is invalid", ErrInvalidInput)
	}
	data, err := json.Marshal(struct {
		SelectionSHA256   string `json:"selection_sha256"`
		RequirementSHA256 string `json:"requirement_sha256"`
	}{SelectionSHA256: selectionSHA256, RequirementSHA256: requirementSHA256})
	if err != nil {
		return "", operational("encode behavior review decision binding", err)
	}
	return sha256Hex(data), nil
}

func normalizeSelectedFeature(feature SelectedFeature) (SelectedFeature, error) {
	if !validFeatureName(feature.Name) {
		return SelectedFeature{}, fmt.Errorf("%w: selected behavior review feature name is invalid", ErrInvalidInput)
	}
	modules, err := normalizeFeatureValues(feature.Modules, validIdentifier, "module")
	if err != nil {
		return SelectedFeature{}, err
	}
	paths, err := normalizeFeatureValues(feature.Paths, validFeaturePathPattern, "path")
	if err != nil {
		return SelectedFeature{}, err
	}
	if len(modules) == 0 && len(paths) == 0 {
		return SelectedFeature{}, fmt.Errorf("%w: selected behavior review feature needs a module or path scope", ErrInvalidInput)
	}
	suites, err := normalizeFeatureValues(feature.Suites, validIdentifier, "suite")
	if err != nil {
		return SelectedFeature{}, err
	}
	if len(suites) == 0 {
		return SelectedFeature{}, fmt.Errorf("%w: selected behavior review feature needs an ordinary suite", ErrInvalidInput)
	}
	if !validSelectionRequiredAt(feature.RequiredAt) {
		return SelectedFeature{}, fmt.Errorf("%w: selected behavior review feature requirement is invalid", ErrInvalidInput)
	}
	reasons, err := normalizeSelectionReasons(feature.Reasons)
	if err != nil {
		return SelectedFeature{}, err
	}
	if len(reasons) == 0 {
		return SelectedFeature{}, fmt.Errorf("%w: selected behavior review feature needs a selection reason", ErrInvalidInput)
	}
	return SelectedFeature{
		Name: feature.Name, Modules: modules, Paths: paths, Suites: suites, RequiredAt: feature.RequiredAt, Reasons: reasons,
	}, nil
}

func normalizeFeatureValues(values []string, valid func(string) bool, label string) ([]string, error) {
	normalized := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !valid(value) || seen[value] {
			return nil, fmt.Errorf("%w: selected behavior review feature %s values are invalid", ErrInvalidInput, label)
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func normalizeSelectionReasons(values []string) ([]string, error) {
	normalized := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if !validSelectionReason(value) || seen[value] {
			return nil, fmt.Errorf("%w: behavior review selection reasons are invalid", ErrInvalidInput)
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func validSelectionRequiredAt(value string) bool {
	return slices.Contains([]string{policy.BehaviorReviewOnRequest, policy.BehaviorReviewMerge, policy.BehaviorReviewCheckpoint}, value)
}

func validFeatureName(value string) bool {
	return validIdentifier(value) && value == strings.ToLower(value)
}

func validFeaturePathPattern(value string) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && !filepath.IsAbs(value) &&
		filepath.ToSlash(value) == value && !strings.Contains(value, "\\") && !strings.HasPrefix(value, "../") &&
		!strings.Contains(value, "/../") && !strings.ContainsRune(value, 0)
}

func validSelectionReason(value string) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && len([]byte(value)) <= 256
}

func validateReviewSelection(selection ReviewSelection) error {
	normalized, err := NormalizeReviewSelection(selection)
	if err != nil {
		return fmt.Errorf("%w: behavior review selection is invalid: %v", ErrInvalidReview, err)
	}
	if !sameReviewSelection(selection, normalized) {
		return fmt.Errorf("%w: behavior review selection is not canonical", ErrInvalidReview)
	}
	return nil
}

func sameReviewSelection(left, right ReviewSelection) bool {
	if left.FullCandidate != right.FullCandidate || !slices.Equal(left.FullCandidateReasons, right.FullCandidateReasons) || len(left.Features) != len(right.Features) {
		return false
	}
	for index := range left.Features {
		if !sameSelectedFeature(left.Features[index], right.Features[index]) {
			return false
		}
	}
	return true
}

func sameSelectedFeature(left, right SelectedFeature) bool {
	return left.Name == right.Name && left.RequiredAt == right.RequiredAt &&
		slices.Equal(left.Modules, right.Modules) && slices.Equal(left.Paths, right.Paths) &&
		slices.Equal(left.Suites, right.Suites) && slices.Equal(left.Reasons, right.Reasons)
}

func cloneReviewSelection(selection ReviewSelection) ReviewSelection {
	features := make([]SelectedFeature, 0, len(selection.Features))
	for _, feature := range selection.Features {
		features = append(features, SelectedFeature{
			Name: feature.Name, Modules: slices.Clone(feature.Modules), Paths: slices.Clone(feature.Paths), Suites: slices.Clone(feature.Suites),
			RequiredAt: feature.RequiredAt, Reasons: slices.Clone(feature.Reasons),
		})
	}
	return ReviewSelection{Features: features, FullCandidate: selection.FullCandidate, FullCandidateReasons: slices.Clone(selection.FullCandidateReasons)}
}

func selectedFeatureNames(selection ReviewSelection) []string {
	names := make([]string, 0, len(selection.Features))
	for _, feature := range selection.Features {
		names = append(names, feature.Name)
	}
	return names
}

func selectionHasFeature(selection ReviewSelection, name string) bool {
	for _, feature := range selection.Features {
		if feature.Name == name {
			return true
		}
	}
	return false
}

func selectionFeature(selection ReviewSelection, name string) (SelectedFeature, bool) {
	for _, feature := range selection.Features {
		if feature.Name == name {
			return feature, true
		}
	}
	return SelectedFeature{}, false
}

func validateSelectionIncludesFeatures(selection ReviewSelection, features []string) error {
	for _, feature := range features {
		if !selectionHasFeature(selection, feature) {
			return fmt.Errorf("%w: behavior review selection omits requested feature %q", ErrInvalidInput, feature)
		}
	}
	return nil
}

func validateBehaviorScope(scope BehaviorScope, selection ReviewSelection) error {
	if err := validateBehaviorScopeShape(scope); err != nil {
		return err
	}
	for _, name := range scope.Features {
		if !selectionHasFeature(selection, name) {
			return fmt.Errorf("%w: behavior scope features are invalid", ErrInvalidReview)
		}
	}
	if scope.FullCandidate && !selection.FullCandidate {
		return fmt.Errorf("%w: behavior scope does not match the selected review scope", ErrInvalidReview)
	}
	return nil
}

func validateBehaviorScopeShape(scope BehaviorScope) error {
	if scope.Features == nil {
		return fmt.Errorf("%w: behavior scope must explicitly contain a features array", ErrInvalidReview)
	}
	if scope.FullCandidate == (len(scope.Features) != 0) {
		return fmt.Errorf("%w: behavior scope must name selected features or the full candidate", ErrInvalidReview)
	}
	previous := ""
	for _, name := range scope.Features {
		if !validFeatureName(name) || name <= previous {
			return fmt.Errorf("%w: behavior scope features are invalid", ErrInvalidReview)
		}
		previous = name
	}
	return nil
}

func selectionAllowsSuite(selection ReviewSelection, suite string) bool {
	if selection.FullCandidate {
		return true
	}
	for _, feature := range selection.Features {
		if slices.Contains(feature.Suites, suite) {
			return true
		}
	}
	return false
}

func scopeAllowsSuite(scope BehaviorScope, selection ReviewSelection, suite string) bool {
	if scope.FullCandidate {
		return selection.FullCandidate
	}
	for _, name := range scope.Features {
		feature, ok := selectionFeature(selection, name)
		if ok && slices.Contains(feature.Suites, suite) {
			return true
		}
	}
	return false
}
