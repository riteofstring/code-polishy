package gaterun

import (
	"fmt"
	"reflect"
	"strings"
)

func validateBehaviorReview(review BehaviorReview) error {
	if !validBehaviorReviewState(review.State) || !validBehaviorReviewBoundary(review.RequiredBoundary) || !validSHA256(review.SelectionDigest) {
		return fmt.Errorf("%w: behavior review header is invalid", ErrInvalidInput)
	}
	if review.SelectedFeatures == nil {
		return fmt.Errorf("%w: behavior review selections are missing", ErrInvalidInput)
	}
	if err := validateBehaviorReviewFeatures(review.SelectedFeatures); err != nil {
		return err
	}
	if review.State != BehaviorReviewNotRun && len(review.SelectedFeatures) == 0 && !review.FullCandidate {
		return fmt.Errorf("%w: required behavior review has no selected scope", ErrInvalidInput)
	}
	if !validBehaviorReviewReceipt(review) {
		return fmt.Errorf("%w: behavior review receipt is invalid", ErrInvalidInput)
	}
	return nil
}

func validBehaviorReviewState(state BehaviorReviewState) bool {
	switch state {
	case BehaviorReviewNotRun, BehaviorReviewRequired, BehaviorReviewPassed, BehaviorReviewFailed:
		return true
	default:
		return false
	}
}

func validBehaviorReviewBoundary(boundary BehaviorReviewBoundary) bool {
	switch boundary {
	case BehaviorReviewOnRequest, BehaviorReviewMerge, BehaviorReviewCheckpoint:
		return true
	default:
		return false
	}
}

func validateBehaviorReviewFeatures(features []BehaviorReviewFeatureSelection) error {
	known := map[string]bool{}
	previous := ""
	for index, feature := range features {
		if !validToken(feature.Name) || feature.Name <= previous || known[feature.Name] || !validBehaviorReviewReasons(feature.Reasons) {
			return fmt.Errorf("%w: behavior review feature %d is invalid", ErrInvalidInput, index)
		}
		known[feature.Name] = true
		previous = feature.Name
	}
	return nil
}

func validBehaviorReviewReasons(reasons []string) bool {
	if len(reasons) == 0 || !validStrings(reasons) {
		return false
	}
	previous := ""
	for _, reason := range reasons {
		if reason <= previous || strings.TrimSpace(reason) != reason {
			return false
		}
		previous = reason
	}
	return true
}

func validBehaviorReviewReceipt(review BehaviorReview) bool {
	hasReviewID := review.ReviewID != ""
	hasReceiptPath := review.ReceiptPath != ""
	if hasReviewID != hasReceiptPath {
		return false
	}
	if hasReviewID && (!validToken(review.ReviewID) || !validArtifactDisplayPath(review.ReceiptPath)) {
		return false
	}
	switch review.State {
	case BehaviorReviewNotRun, BehaviorReviewRequired:
		return !hasReviewID
	case BehaviorReviewPassed:
		return hasReviewID
	case BehaviorReviewFailed:
		return true
	default:
		return false
	}
}

func validateReportBehaviorReview(planned, outcome BehaviorReview) error {
	if err := validateBehaviorReview(outcome); err != nil {
		return fmt.Errorf("%w: gate run behavior review is invalid", ErrInvalidArtifact)
	}
	if !sameBehaviorReviewSelection(planned, outcome) {
		return fmt.Errorf("%w: gate run behavior review does not match its identity", ErrStaleArtifact)
	}
	if !validBehaviorReviewTransition(planned, outcome) {
		return fmt.Errorf("%w: gate run behavior review outcome does not match its identity", ErrStaleArtifact)
	}
	return nil
}

func sameBehaviorReviewSelection(left, right BehaviorReview) bool {
	return left.RequiredBoundary == right.RequiredBoundary && left.SelectionDigest == right.SelectionDigest &&
		left.FullCandidate == right.FullCandidate && reflect.DeepEqual(left.SelectedFeatures, right.SelectedFeatures)
}

func validBehaviorReviewTransition(planned, outcome BehaviorReview) bool {
	switch planned.State {
	case BehaviorReviewNotRun:
		return outcome.State == BehaviorReviewNotRun
	case BehaviorReviewRequired:
		return outcome.State == BehaviorReviewRequired
	case BehaviorReviewPassed:
		if outcome.State == BehaviorReviewPassed {
			return sameBehaviorReviewReceipt(planned, outcome)
		}
		return outcome.State == BehaviorReviewFailed && (!hasBehaviorReviewReceipt(outcome) || sameBehaviorReviewReceipt(planned, outcome))
	case BehaviorReviewFailed:
		return outcome.State == BehaviorReviewFailed && sameBehaviorReviewReceipt(planned, outcome)
	default:
		return false
	}
}

func hasBehaviorReviewReceipt(review BehaviorReview) bool {
	return review.ReviewID != ""
}

func sameBehaviorReviewReceipt(left, right BehaviorReview) bool {
	return left.ReviewID == right.ReviewID && left.ReceiptPath == right.ReceiptPath
}

func cloneBehaviorReview(review BehaviorReview) BehaviorReview {
	result := review
	result.SelectedFeatures = make([]BehaviorReviewFeatureSelection, len(review.SelectedFeatures))
	for index, feature := range review.SelectedFeatures {
		result.SelectedFeatures[index] = BehaviorReviewFeatureSelection{Name: feature.Name, Reasons: cloneStrings(feature.Reasons)}
	}
	return result
}
