package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"github.com/riteofstring/code-polishy/internal/behaviorreview"
	"github.com/riteofstring/code-polishy/internal/finalstate"
	"github.com/riteofstring/code-polishy/internal/gaterun"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

const behaviorReviewReceiptPath = ".code-polishy-reports/behavior-review/receipt.json"

type BehaviorReviewState = gaterun.BehaviorReviewState

const (
	BehaviorReviewNotRun   = gaterun.BehaviorReviewNotRun
	BehaviorReviewRequired = gaterun.BehaviorReviewRequired
	BehaviorReviewPassed   = gaterun.BehaviorReviewPassed
	BehaviorReviewFailed   = gaterun.BehaviorReviewFailed
)

type BehaviorReviewBoundary = gaterun.BehaviorReviewBoundary

const (
	BehaviorReviewOnRequest  = gaterun.BehaviorReviewOnRequest
	BehaviorReviewMerge      = gaterun.BehaviorReviewMerge
	BehaviorReviewCheckpoint = gaterun.BehaviorReviewCheckpoint
)

type BehaviorReviewFeatureSelection struct {
	Name    string
	Reasons []string
}

type BehaviorReviewStatus struct {
	State              BehaviorReviewState
	FinalStateState    BehaviorReviewState
	RequiredBoundary   BehaviorReviewBoundary
	SelectedFeatures   []BehaviorReviewFeatureSelection
	SelectionDigest    string
	FullCandidate      bool
	ReviewID           string
	ReceiptPath        string
	Affected           []string
	Configured         []string
	TaskRequested      []string
	Required           []string
	Completed          []string
	Missing            []string
	FinalStateFindings []finalstate.Finding
}

type behaviorReviewDecision struct {
	required           bool
	selection          behaviorreview.ReviewSelection
	selectionDigest    string
	requiredSuites     []string
	baseSelectedSuites []policy.TestSuite
	status             BehaviorReviewStatus
}

type behaviorReviewFeatureRequirement struct {
	base              *policy.BehaviorReviewFeature
	candidate         *policy.BehaviorReviewFeature
	task              bool
	baseRule          bool
	candidateRule     bool
	baseAffected      bool
	candidateAffected bool
}

type behaviorReviewDecisionInput struct {
	selection          behaviorreview.ReviewSelection
	requiredBoundary   BehaviorReviewBoundary
	baseSelectedSuites []policy.TestSuite
	configured         []string
	affected           []string
	requested          []string
}

type behaviorReviewBaseConfiguration struct {
	Config  policy.Config
	Path    string
	Present bool
}

func (engine *Engine) behaviorReviewDecision(
	ctx context.Context, selection repository.Selection, boundary BehaviorReviewBoundary,
) (behaviorReviewDecision, error) {
	baseConfiguration, err := engine.behaviorReviewConfigAt(selection.Base)
	if err != nil {
		return behaviorReviewDecision{}, err
	}
	return engine.behaviorReviewDecisionWithBaseConfiguration(ctx, selection, boundary, baseConfiguration.Config)
}

func (engine *Engine) behaviorReviewDecisionWithBaseConfiguration(
	ctx context.Context, selection repository.Selection, boundary BehaviorReviewBoundary, baseConfig policy.Config,
) (behaviorReviewDecision, error) {
	if behaviorReviewConfigurationAbsent(engine.Repository.Config, baseConfig) {
		return emptyBehaviorReviewDecision()
	}
	input, err := engine.buildBehaviorReviewDecisionInput(ctx, selection, boundary, baseConfig)
	if err != nil {
		return behaviorReviewDecision{}, err
	}
	return behaviorReviewDecisionFromInput(input)
}

func behaviorReviewConfigurationAbsent(candidateConfig, baseConfig policy.Config) bool {
	return candidateConfig.Verification.BehaviorReview == nil && baseConfig.Verification.BehaviorReview == nil
}

func (engine *Engine) buildBehaviorReviewDecisionInput(
	ctx context.Context, selection repository.Selection, boundary BehaviorReviewBoundary, baseConfig policy.Config,
) (behaviorReviewDecisionInput, error) {
	documentation := engine.Repository.ClassifyDocumentationCandidate(selection).Ordinary
	requirements, err := behaviorreview.TaskRequirements(ctx, engine.Repository, selection.Base)
	if err != nil {
		return behaviorReviewDecisionInput{}, err
	}
	candidateImpact := engine.Repository.CandidateImpact(selection.Candidate)
	baseRepository := engine.Repository
	baseRepository.Config = baseConfig
	baseImpact := baseRepository.CandidateImpact(selection.Candidate)
	features := behaviorReviewFeatureRequirements(
		engine.Repository.Config, baseConfig, candidateImpact, baseImpact, boundary, requirements.RequestedFeatures,
	)
	selectionValue, requiredBoundary, err := behaviorReviewSelection(engine.Repository.Config, baseConfig, features, boundary, documentation)
	if err != nil {
		return behaviorReviewDecisionInput{}, err
	}
	baseSelectedSuites, err := behaviorReviewBaseSelectedSuites(baseConfig, features)
	if err != nil {
		return behaviorReviewDecisionInput{}, err
	}
	configured, affected := behaviorReviewStatusFeatures(features)
	return behaviorReviewDecisionInput{
		selection:          selectionValue,
		requiredBoundary:   requiredBoundary,
		baseSelectedSuites: baseSelectedSuites,
		configured:         configured,
		affected:           affected,
		requested:          requirements.RequestedFeatures,
	}, nil
}

func behaviorReviewDecisionFromInput(input behaviorReviewDecisionInput) (behaviorReviewDecision, error) {
	if len(input.selection.Features) == 0 && !input.selection.FullCandidate {
		return behaviorReviewNotRunDecision(input.affected, input.configured, input.requested)
	}
	normalized, err := behaviorreview.NormalizeReviewSelection(input.selection)
	if err != nil {
		return behaviorReviewDecision{}, fmt.Errorf("normalize behavior review selection: %w", err)
	}
	digest, err := behaviorreview.SelectionSHA256(normalized)
	if err != nil {
		return behaviorReviewDecision{}, fmt.Errorf("digest behavior review selection: %w", err)
	}
	status := newBehaviorReviewStatus(BehaviorReviewRequired, input.requiredBoundary, normalized, digest, "", "")
	status.Affected = input.affected
	status.Configured = input.configured
	status.TaskRequested = slices.Clone(input.requested)
	status.Required = behaviorReviewSelectionNames(normalized)
	status.Completed = []string{}
	status.Missing = slices.Clone(status.Required)
	return behaviorReviewDecision{
		required: true, selection: normalized, selectionDigest: digest,
		requiredSuites: behaviorReviewSelectionSuites(normalized), baseSelectedSuites: input.baseSelectedSuites, status: status,
	}, nil
}

func emptyBehaviorReviewDecision() (behaviorReviewDecision, error) {
	return behaviorReviewNotRunDecision([]string{}, []string{}, []string{})
}

func behaviorReviewNotRunDecision(affected, configured, requested []string) (behaviorReviewDecision, error) {
	digest, err := emptyBehaviorReviewSelectionDigest()
	if err != nil {
		return behaviorReviewDecision{}, err
	}
	return behaviorReviewDecision{selectionDigest: digest, status: BehaviorReviewStatus{
		State: BehaviorReviewNotRun, FinalStateState: BehaviorReviewNotRun, RequiredBoundary: BehaviorReviewOnRequest,
		SelectedFeatures: []BehaviorReviewFeatureSelection{}, SelectionDigest: digest,
		Affected: affected, Configured: configured, TaskRequested: slices.Clone(requested),
		Required: []string{}, Completed: []string{}, Missing: []string{},
	}}, nil
}

func (engine *Engine) behaviorReviewConfigAt(base string) (behaviorReviewBaseConfiguration, error) {
	configPath := engine.Repository.Config.ConfigPath
	if configPath == "" {
		configPath = policy.ConfigFilename
	}
	if filepath.IsAbs(configPath) {
		if resolved, resolveErr := filepath.EvalSymlinks(configPath); resolveErr == nil {
			configPath = resolved
		}
		relative, err := filepath.Rel(engine.Repository.Root, configPath)
		if err != nil {
			return behaviorReviewBaseConfiguration{}, fmt.Errorf("resolve base configuration: %w", err)
		}
		configPath = relative
	}
	normalized, err := engine.Repository.NormalizePath(configPath)
	if err != nil {
		return behaviorReviewBaseConfiguration{}, fmt.Errorf("resolve base configuration: %w", err)
	}
	data, present, err := engine.Repository.ReadRegularFileAt(base, normalized)
	if err != nil {
		return behaviorReviewBaseConfiguration{}, fmt.Errorf("read base configuration: %w", err)
	}
	if !present {
		return behaviorReviewBaseConfiguration{Config: policy.Config{}, Path: normalized, Present: false}, nil
	}
	config, err := policy.Parse(data, normalized)
	if err != nil {
		return behaviorReviewBaseConfiguration{}, fmt.Errorf("parse base configuration: %w", err)
	}
	return behaviorReviewBaseConfiguration{Config: config, Path: normalized, Present: true}, nil
}

func behaviorReviewFeatureRequirements(
	candidateConfig, baseConfig policy.Config, candidateImpact, baseImpact repository.CandidateImpact,
	boundary BehaviorReviewBoundary, requested []string,
) map[string]behaviorReviewFeatureRequirement {
	result := map[string]behaviorReviewFeatureRequirement{}
	for _, feature := range behaviorReviewFeatures(candidateConfig) {
		entry := result[feature.Name]
		feature := feature
		entry.candidate = &feature
		entry.candidateAffected = behaviorReviewFeatureAffected(feature, candidateImpact)
		entry.candidateRule = behaviorReviewFeatureRequired(candidateConfig, feature, candidateImpact, boundary)
		result[feature.Name] = entry
	}
	for _, feature := range behaviorReviewFeatures(baseConfig) {
		entry := result[feature.Name]
		feature := feature
		entry.base = &feature
		entry.baseAffected = behaviorReviewFeatureAffected(feature, baseImpact)
		entry.baseRule = behaviorReviewFeatureRequired(baseConfig, feature, baseImpact, boundary)
		result[feature.Name] = entry
	}
	for _, name := range requested {
		entry := result[name]
		entry.task = true
		result[name] = entry
	}
	return result
}

func behaviorReviewSelection(
	candidateConfig, baseConfig policy.Config, requirements map[string]behaviorReviewFeatureRequirement,
	boundary BehaviorReviewBoundary, documentation bool,
) (behaviorreview.ReviewSelection, BehaviorReviewBoundary, error) {
	selected := make([]behaviorreview.SelectedFeature, 0, len(requirements))
	selectedBoundary := BehaviorReviewOnRequest
	names := make([]string, 0, len(requirements))
	for name := range requirements {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		feature := requirements[name]
		if !feature.task && !feature.baseRule && !feature.candidateRule {
			continue
		}
		reasons := behaviorReviewFeatureReasons(feature)
		definition, err := behaviorReviewFeatureSelection(candidateConfig, baseConfig, name, feature, reasons)
		if err != nil {
			return behaviorreview.ReviewSelection{}, "", err
		}
		selected = append(selected, definition)
		selectedBoundary = strongerBehaviorReviewBoundary(selectedBoundary, behaviorReviewFeatureBoundary(candidateConfig, baseConfig, feature, boundary))
	}
	fullCandidateReasons := []string{}
	if !documentation {
		if behaviorReviewPersistentRequirement(candidateConfig, boundary, true) {
			fullCandidateReasons = append(fullCandidateReasons, behaviorreview.SelectionReasonDefaultRequired)
			selectedBoundary = strongerBehaviorReviewBoundary(selectedBoundary, behaviorReviewDefaultBoundary(candidateConfig, boundary))
		}
		if behaviorReviewPersistentRequirement(baseConfig, boundary, true) {
			fullCandidateReasons = append(fullCandidateReasons, behaviorreview.SelectionReasonDefaultRequired)
			selectedBoundary = strongerBehaviorReviewBoundary(selectedBoundary, behaviorReviewDefaultBoundary(baseConfig, boundary))
		}
	}
	return behaviorreview.ReviewSelection{
		Features: selected, FullCandidate: len(fullCandidateReasons) != 0, FullCandidateReasons: sortedUniqueStrings(fullCandidateReasons),
	}, selectedBoundary, nil
}

func behaviorReviewFeatures(config policy.Config) []policy.BehaviorReviewFeature {
	if config.Verification.BehaviorReview == nil {
		return nil
	}
	return config.Verification.BehaviorReview.Features
}

func behaviorReviewFeatureRequired(
	config policy.Config, feature policy.BehaviorReviewFeature, impact repository.CandidateImpact, boundary BehaviorReviewBoundary,
) bool {
	policyValue := config.Verification.BehaviorReview
	return policyValue != nil && behaviorReviewRequirementApplies(policyValue.EffectiveRequiredAt(feature), boundary) &&
		behaviorReviewFeatureAffected(feature, impact)
}

func behaviorReviewFeatureAffected(feature policy.BehaviorReviewFeature, impact repository.CandidateImpact) bool {
	for _, module := range feature.Modules {
		if slices.Contains(impact.ImpactedModules, module) {
			return true
		}
	}
	for _, path := range impact.Paths {
		if policy.MatchesAny(path, feature.Paths) {
			return true
		}
	}
	return false
}

func behaviorReviewPersistentRequirement(config policy.Config, boundary BehaviorReviewBoundary, fullCandidate bool) bool {
	policyValue := config.Verification.BehaviorReview
	if policyValue == nil {
		return false
	}
	if fullCandidate {
		return behaviorReviewRequirementApplies(policyValue.DefaultRequiredAt, boundary)
	}
	for _, feature := range policyValue.Features {
		if behaviorReviewRequirementApplies(policyValue.EffectiveRequiredAt(feature), boundary) {
			return true
		}
	}
	return false
}

func behaviorReviewRequirementApplies(requiredAt string, boundary BehaviorReviewBoundary) bool {
	switch requiredAt {
	case policy.BehaviorReviewCheckpoint:
		return boundary == BehaviorReviewCheckpoint || boundary == BehaviorReviewMerge
	case policy.BehaviorReviewMerge:
		return boundary == BehaviorReviewMerge
	default:
		return false
	}
}

func behaviorReviewFeatureReasons(feature behaviorReviewFeatureRequirement) []string {
	reasons := []string{}
	if feature.task {
		reasons = append(reasons, behaviorreview.SelectionReasonTaskRequested)
	}
	if feature.candidateRule {
		reasons = append(reasons, behaviorreview.SelectionReasonCandidateRequired)
	}
	if feature.baseRule {
		reasons = append(reasons, behaviorreview.SelectionReasonBaseRequired)
	}
	return sortedUniqueStrings(reasons)
}

func behaviorReviewFeatureSelection(
	candidateConfig, baseConfig policy.Config, name string, feature behaviorReviewFeatureRequirement, reasons []string,
) (behaviorreview.SelectedFeature, error) {
	var candidateSelection, baseSelection behaviorreview.SelectedFeature
	var candidateFound, baseFound bool
	var err error
	if feature.candidate != nil {
		candidateSelection, err = behaviorreview.ConfiguredFeatureSelection(candidateConfig, name, reasons)
		if err != nil {
			return behaviorreview.SelectedFeature{}, fmt.Errorf("select candidate behavior review feature %q: %w", name, err)
		}
		candidateFound = true
	}
	if feature.base != nil {
		baseSelection, err = behaviorreview.ConfiguredFeatureSelection(baseConfig, name, reasons)
		if err != nil {
			return behaviorreview.SelectedFeature{}, fmt.Errorf("select base behavior review feature %q: %w", name, err)
		}
		baseFound = true
	}
	switch {
	case !candidateFound && !baseFound:
		return behaviorreview.SelectedFeature{}, fmt.Errorf("select behavior review feature %q: no base or candidate definition exists", name)
	case candidateFound && !baseFound:
		return candidateSelection, nil
	case baseFound && !candidateFound:
		return baseSelection, nil
	default:
		return behaviorreview.SelectedFeature{
			Name:       name,
			Modules:    sortedUniqueStrings(append(candidateSelection.Modules, baseSelection.Modules...)),
			Paths:      sortedUniqueStrings(append(candidateSelection.Paths, baseSelection.Paths...)),
			Suites:     sortedUniqueStrings(append(candidateSelection.Suites, baseSelection.Suites...)),
			RequiredAt: strongestBehaviorReviewRequirement(candidateSelection.RequiredAt, baseSelection.RequiredAt),
			Reasons:    sortedUniqueStrings(reasons),
		}, nil
	}
}

func strongestBehaviorReviewRequirement(left, right string) string {
	if behaviorReviewRequirementRank(right) > behaviorReviewRequirementRank(left) {
		return right
	}
	return left
}

func behaviorReviewRequirementRank(requiredAt string) int {
	switch requiredAt {
	case policy.BehaviorReviewCheckpoint:
		return 2
	case policy.BehaviorReviewMerge:
		return 1
	default:
		return 0
	}
}

func behaviorReviewFeatureBoundary(
	candidateConfig, baseConfig policy.Config, feature behaviorReviewFeatureRequirement, current BehaviorReviewBoundary,
) BehaviorReviewBoundary {
	boundary := BehaviorReviewOnRequest
	if feature.candidateRule && feature.candidate != nil {
		boundary = strongerBehaviorReviewBoundary(boundary, behaviorReviewRequirementBoundary(candidateConfig, *feature.candidate, current))
	}
	if feature.baseRule && feature.base != nil {
		boundary = strongerBehaviorReviewBoundary(boundary, behaviorReviewRequirementBoundary(baseConfig, *feature.base, current))
	}
	return boundary
}

func behaviorReviewRequirementBoundary(config policy.Config, feature policy.BehaviorReviewFeature, current BehaviorReviewBoundary) BehaviorReviewBoundary {
	policyValue := config.Verification.BehaviorReview
	if policyValue == nil {
		return BehaviorReviewOnRequest
	}
	switch policyValue.EffectiveRequiredAt(feature) {
	case policy.BehaviorReviewCheckpoint:
		return BehaviorReviewCheckpoint
	case policy.BehaviorReviewMerge:
		if current == BehaviorReviewMerge {
			return BehaviorReviewMerge
		}
	}
	return BehaviorReviewOnRequest
}

func behaviorReviewDefaultBoundary(config policy.Config, current BehaviorReviewBoundary) BehaviorReviewBoundary {
	policyValue := config.Verification.BehaviorReview
	if policyValue == nil {
		return BehaviorReviewOnRequest
	}
	switch policyValue.DefaultRequiredAt {
	case policy.BehaviorReviewCheckpoint:
		return BehaviorReviewCheckpoint
	case policy.BehaviorReviewMerge:
		if current == BehaviorReviewMerge {
			return BehaviorReviewMerge
		}
	}
	return BehaviorReviewOnRequest
}

func strongerBehaviorReviewBoundary(left, right BehaviorReviewBoundary) BehaviorReviewBoundary {
	if behaviorReviewBoundaryRank(right) > behaviorReviewBoundaryRank(left) {
		return right
	}
	return left
}

func behaviorReviewBoundaryRank(boundary BehaviorReviewBoundary) int {
	switch boundary {
	case BehaviorReviewCheckpoint:
		return 2
	case BehaviorReviewMerge:
		return 1
	default:
		return 0
	}
}

func behaviorReviewSelectionSuites(selection behaviorreview.ReviewSelection) []string {
	suites := []string{}
	for _, feature := range selection.Features {
		suites = append(suites, feature.Suites...)
	}
	return sortedUniqueStrings(suites)
}

func behaviorReviewBaseSelectedSuites(
	config policy.Config, requirements map[string]behaviorReviewFeatureRequirement,
) ([]policy.TestSuite, error) {
	configured := make(map[string]policy.TestSuite, len(config.Tests.Suites))
	for _, suite := range config.Tests.Suites {
		configured[suite.Name] = suite
	}
	names := make([]string, 0, len(requirements))
	for _, feature := range requirements {
		if feature.base != nil && (feature.task || feature.baseRule || feature.candidateRule) {
			names = append(names, feature.base.Suites...)
		}
	}
	names = sortedUniqueStrings(names)
	suites := make([]policy.TestSuite, 0, len(names))
	for _, name := range names {
		suite, found := configured[name]
		if !found {
			return nil, fmt.Errorf("base-required behavior review suite %q is unavailable in the base configuration", name)
		}
		suites = append(suites, suite)
	}
	return suites, nil
}

func behaviorReviewSelectionNames(selection behaviorreview.ReviewSelection) []string {
	names := make([]string, 0, len(selection.Features))
	for _, feature := range selection.Features {
		names = append(names, feature.Name)
	}
	return names
}

func behaviorReviewStatusFeatures(requirements map[string]behaviorReviewFeatureRequirement) ([]string, []string) {
	configured := make([]string, 0, len(requirements))
	affected := make([]string, 0, len(requirements))
	for name, feature := range requirements {
		if feature.base != nil || feature.candidate != nil {
			configured = append(configured, name)
		}
		if feature.baseAffected || feature.candidateAffected {
			affected = append(affected, name)
		}
	}
	sort.Strings(configured)
	sort.Strings(affected)
	return configured, affected
}

func newBehaviorReviewStatus(
	state BehaviorReviewState, boundary BehaviorReviewBoundary, selection behaviorreview.ReviewSelection,
	digest, reviewID, receiptPath string,
) BehaviorReviewStatus {
	features := make([]BehaviorReviewFeatureSelection, 0, len(selection.Features))
	for _, feature := range selection.Features {
		features = append(features, BehaviorReviewFeatureSelection{Name: feature.Name, Reasons: slices.Clone(feature.Reasons)})
	}
	return BehaviorReviewStatus{
		State: state, FinalStateState: initialFinalState(state), RequiredBoundary: boundary, SelectedFeatures: features, SelectionDigest: digest,
		FullCandidate: selection.FullCandidate, ReviewID: reviewID, ReceiptPath: receiptPath,
	}
}

func (decision behaviorReviewDecision) withState(state BehaviorReviewState, reviewID, receiptPath string) BehaviorReviewStatus {
	status := cloneBehaviorReviewStatus(decision.status)
	status.State = state
	status.FinalStateState = initialFinalState(state)
	status.ReviewID = reviewID
	status.ReceiptPath = receiptPath
	switch state {
	case BehaviorReviewPassed:
		status.Completed = slices.Clone(status.Required)
		status.Missing = []string{}
	case BehaviorReviewRequired, BehaviorReviewFailed:
		status.Completed = []string{}
		status.Missing = slices.Clone(status.Required)
	default:
		status.Completed = []string{}
		status.Missing = []string{}
	}
	return status
}

func initialFinalState(state BehaviorReviewState) BehaviorReviewState {
	if state == BehaviorReviewPassed || state == BehaviorReviewFailed {
		return state
	}
	return BehaviorReviewNotRun
}

func cloneBehaviorReviewStatus(status BehaviorReviewStatus) BehaviorReviewStatus {
	result := status
	result.SelectedFeatures = make([]BehaviorReviewFeatureSelection, len(status.SelectedFeatures))
	for index, feature := range status.SelectedFeatures {
		result.SelectedFeatures[index] = BehaviorReviewFeatureSelection{Name: feature.Name, Reasons: slices.Clone(feature.Reasons)}
	}
	result.Affected = slices.Clone(status.Affected)
	result.Configured = slices.Clone(status.Configured)
	result.TaskRequested = slices.Clone(status.TaskRequested)
	result.Required = slices.Clone(status.Required)
	result.Completed = slices.Clone(status.Completed)
	result.Missing = slices.Clone(status.Missing)
	result.FinalStateFindings = make([]finalstate.Finding, len(status.FinalStateFindings))
	for index, finding := range status.FinalStateFindings {
		result.FinalStateFindings[index] = finding
		result.FinalStateFindings[index].IntentIDs = slices.Clone(finding.IntentIDs)
	}
	return result
}

func gaterunBehaviorReview(status BehaviorReviewStatus) gaterun.BehaviorReview {
	features := make([]gaterun.BehaviorReviewFeatureSelection, 0, len(status.SelectedFeatures))
	for _, feature := range status.SelectedFeatures {
		features = append(features, gaterun.BehaviorReviewFeatureSelection{Name: feature.Name, Reasons: slices.Clone(feature.Reasons)})
	}
	return gaterun.BehaviorReview{
		State: status.State, RequiredBoundary: status.RequiredBoundary,
		SelectedFeatures: features, SelectionDigest: status.SelectionDigest, FullCandidate: status.FullCandidate,
		ReviewID: status.ReviewID, ReceiptPath: status.ReceiptPath,
	}
}

func withBehaviorReview(report Report, status BehaviorReviewStatus) Report {
	cloned := cloneBehaviorReviewStatus(status)
	report.BehaviorReview = &cloned
	return report
}

func emptyBehaviorReviewSelectionDigest() (string, error) {
	data, err := json.Marshal(behaviorreview.ReviewSelection{Features: []behaviorreview.SelectedFeature{}, FullCandidateReasons: []string{}})
	if err != nil {
		return "", fmt.Errorf("encode empty behavior review selection: %w", err)
	}
	return gaterun.ContentSHA256(data), nil
}
