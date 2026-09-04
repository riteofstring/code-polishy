package policy

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const MaximumBehaviorReviewFeatures = 128

func NormalizeFeatureAlias(value string) (string, error) {
	if !utf8.ValidString(value) || len(value) > 256 {
		return "", fmt.Errorf("feature name or alias must be UTF-8 text of at most 256 bytes")
	}
	for _, character := range value {
		if (unicode.IsControl(character) || unicode.In(character, unicode.Cf)) && !unicode.IsSpace(character) {
			return "", fmt.Errorf("feature name or alias contains an unsupported control character")
		}
	}
	identity := strings.Join(strings.Fields(cases.Fold().String(norm.NFKC.String(value))), " ")
	if identity == "" || len(identity) > 256 {
		return "", fmt.Errorf("normalized feature name or alias must contain at most 256 UTF-8 bytes")
	}
	return identity, nil
}

func ResolveBehaviorReviewFeatures(config Config, operands []string) ([]string, error) {
	if len(operands) > MaximumBehaviorReviewFeatures {
		return nil, fmt.Errorf("at most %d behavior review features may be explicitly selected", MaximumBehaviorReviewFeatures)
	}
	features := []BehaviorReviewFeature{}
	if config.Verification.BehaviorReview != nil {
		features = config.Verification.BehaviorReview.Features
	}
	index, err := behaviorReviewFeatureIdentityIndex(features)
	if err != nil {
		return nil, err
	}
	selected := []string{}
	for _, operand := range operands {
		identity, err := NormalizeFeatureAlias(operand)
		if err != nil {
			return nil, fmt.Errorf("behavior review feature %q is malformed: %w", operand, err)
		}
		name, found := index[identity]
		if !found {
			return nil, fmt.Errorf("behavior review feature %q is not configured as an exact name or alias", operand)
		}
		selected = append(selected, name)
	}
	slices.Sort(selected)
	return slices.Compact(selected), nil
}

func behaviorReviewFeatureIdentityIndex(features []BehaviorReviewFeature) (map[string]string, error) {
	if len(features) > MaximumBehaviorReviewFeatures {
		return nil, fmt.Errorf("at most %d behavior review features may be declared", MaximumBehaviorReviewFeatures)
	}
	index := map[string]string{}
	for _, feature := range features {
		values := append([]string{feature.Name}, feature.Aliases...)
		for _, value := range values {
			identity, err := NormalizeFeatureAlias(value)
			if err != nil {
				return nil, fmt.Errorf("behavior review feature %q: %w", feature.Name, err)
			}
			if previous, found := index[identity]; found {
				return nil, fmt.Errorf("behavior review feature %q name or alias %q collides with feature %q after Unicode, case, and whitespace normalization", feature.Name, value, previous)
			}
			index[identity] = feature.Name
		}
	}
	return index, nil
}

func validateBehaviorReviewFeatureDescriptions(features []BehaviorReviewFeature) error {
	for _, feature := range features {
		if strings.TrimSpace(feature.Description) != feature.Description || feature.Description == "" || len(feature.Description) > 512 {
			return fmt.Errorf("behavior review feature %q requires a concise trimmed description of at most 512 UTF-8 bytes", feature.Name)
		}
	}
	_, err := behaviorReviewFeatureIdentityIndex(features)
	return err
}
