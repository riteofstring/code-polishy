package policy

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

func validateVerification(config *Config) error {
	if err := validateTrustedMergeTarget(config.Verification.TrustedMergeTarget); err != nil {
		return err
	}
	if err := validateBehaviorReview(config); err != nil {
		return err
	}
	return validateMergeGate(config)
}

func validateTrustedMergeTarget(target string) error {
	if target == "" {
		return nil
	}
	if strings.TrimSpace(target) != target || strings.HasPrefix(target, "-") || strings.ContainsAny(target, " \t\r\n\x00") {
		return errors.New("verification.trustedMergeTarget must be a non-option Git reference without whitespace")
	}
	return nil
}

func validateMergeGate(config *Config) error {
	mergeGate := config.Verification.MergeGate
	if mergeGate == nil {
		return nil
	}
	for _, module := range mergeGate.RecommendedModules {
		if _, exists := config.ModuleByName[module]; !exists {
			return fmt.Errorf("verification.mergeGate.recommendedModules references unknown module %q", module)
		}
	}
	return nil
}

func validateBehaviorReview(config *Config) error {
	behaviorReview := config.Verification.BehaviorReview
	if behaviorReview == nil {
		return nil
	}
	featureNames := map[string]bool{}
	for index, feature := range behaviorReview.Features {
		label := fmt.Sprintf("verification.behaviorReview.features[%d]", index)
		if err := validateBehaviorReviewFeature(config, *behaviorReview, feature, label, featureNames); err != nil {
			return err
		}
	}
	return nil
}

func validateBehaviorReviewFeature(config *Config, behaviorReview BehaviorReviewPolicy, feature BehaviorReviewFeature, label string, names map[string]bool) error {
	if err := validateBehaviorReviewFeatureScope(config, feature, label, names); err != nil {
		return err
	}
	if err := validateBehaviorReviewFeatureSuites(config, feature, label); err != nil {
		return err
	}
	return validateBehaviorReviewFeatureRequirement(behaviorReview, feature, label)
}

func validateBehaviorReviewFeatureScope(config *Config, feature BehaviorReviewFeature, label string, names map[string]bool) error {
	if names[feature.Name] {
		return fmt.Errorf("duplicate behavior review feature name %q", feature.Name)
	}
	names[feature.Name] = true
	return validateCommandModules(config, feature.Modules, label)
}

func validateBehaviorReviewFeatureSuites(config *Config, feature BehaviorReviewFeature, label string) error {
	for _, suiteName := range feature.Suites {
		suite, err := referencedSuite(config.Tests.Suites, suiteName, label+".suites")
		if err != nil {
			return err
		}
		if !BehaviorReviewSuiteAllowed(suite) {
			return fmt.Errorf("%s.suites references ineligible suite %q; behavior review evidence must be ordinary, non-credentialed, and non-destructive", label, suiteName)
		}
	}
	return nil
}

func validateBehaviorReviewFeatureRequirement(behaviorReview BehaviorReviewPolicy, feature BehaviorReviewFeature, label string) error {
	if behaviorReviewRequirementRank(behaviorReview.EffectiveRequiredAt(feature)) < behaviorReviewRequirementRank(behaviorReview.DefaultRequiredAt) {
		return fmt.Errorf("%s.requiredAt cannot weaken verification.behaviorReview.defaultRequiredAt", label)
	}
	return nil
}

func BehaviorReviewSuiteAllowed(suite TestSuite) bool {
	return !slices.Contains(suite.RunOn, "supplemental") &&
		!supplementalOnlyKind(suite.Kind) &&
		!slices.Contains([]string{"live", "credentialed", "destructive"}, suite.Kind) &&
		len(suite.Environment) == 0
}

func behaviorReviewRequirementRank(requiredAt string) int {
	switch requiredAt {
	case BehaviorReviewOnRequest:
		return 0
	case BehaviorReviewMerge:
		return 1
	case BehaviorReviewCheckpoint:
		return 2
	default:
		return -1
	}
}
