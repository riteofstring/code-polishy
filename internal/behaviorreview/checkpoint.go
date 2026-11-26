package behaviorreview

import "github.com/riteofstring/code-polishy/internal/gaterun"

func validCheckpointBehaviorReview(review gaterun.BehaviorReview) bool {
	return validCheckpointReviewSelection(review) && validCheckpointReviewState(review)
}

func validCheckpointReviewSelection(review gaterun.BehaviorReview) bool {
	if !validSHA256(review.SelectionDigest) || review.SelectedFeatures == nil {
		return false
	}
	if !validCheckpointReviewBoundary(review.RequiredBoundary) {
		return false
	}
	return validCheckpointReviewFeatures(review.SelectedFeatures)
}

func validCheckpointReviewBoundary(boundary gaterun.BehaviorReviewBoundary) bool {
	switch boundary {
	case gaterun.BehaviorReviewOnRequest, gaterun.BehaviorReviewMerge, gaterun.BehaviorReviewCheckpoint:
		return true
	default:
		return false
	}
}

func validCheckpointReviewFeatures(features []gaterun.BehaviorReviewFeatureSelection) bool {
	previousFeature := ""
	for _, feature := range features {
		if !validCheckpointReviewFeature(feature, previousFeature) {
			return false
		}
		previousFeature = feature.Name
	}
	return true
}

func validCheckpointReviewFeature(feature gaterun.BehaviorReviewFeatureSelection, previous string) bool {
	if !validFeatureName(feature.Name) || feature.Name <= previous || len(feature.Reasons) == 0 {
		return false
	}
	return validCheckpointReviewReasons(feature.Reasons)
}

func validCheckpointReviewReasons(reasons []string) bool {
	previous := ""
	for _, reason := range reasons {
		if !validSelectionReason(reason) || reason <= previous {
			return false
		}
		previous = reason
	}
	return true
}

func validCheckpointReviewState(review gaterun.BehaviorReview) bool {
	switch review.State {
	case gaterun.BehaviorReviewNotRun:
		return review.ReviewID == "" && review.ReceiptPath == ""
	case gaterun.BehaviorReviewPassed:
		return validPassedCheckpointReview(review)
	default:
		return false
	}
}

func validPassedCheckpointReview(review gaterun.BehaviorReview) bool {
	return (len(review.SelectedFeatures) > 0 || review.FullCandidate) && validIdentifier(review.ReviewID) && review.ReceiptPath == artifactDisplayPath(receiptFilename)
}
