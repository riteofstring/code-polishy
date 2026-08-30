package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

type behaviorReviewOptions struct {
	action     string
	base       string
	intentFile string
}

type regressionProofOptions struct {
	base     string
	suite    string
	evidence []string
	id       string
	redExit  int
}

var regressionProofOptionNames = []string{"--base", "--suite", "--evidence", "--id", "--red-exit"}

func handleBehaviorReview(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseBehaviorReviewOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	switch options.action {
	case "capture-intent":
		result, captureErr := policyEngine.CaptureBehaviorReviewIntent(ctx, options.intentFile)
		if captureErr != nil {
			return commandResult{}, captureErr
		}
		return commandResult{quiet: true, messages: []string{behaviorReviewIntentCapturedMessage(result.JournalPath, result.ID)}}, nil
	case "prepare":
		result, prepareErr := policyEngine.PrepareBehaviorReview(ctx, options.base)
		if prepareErr != nil {
			return commandResult{}, prepareErr
		}
		return commandResult{quiet: true, messages: []string{behaviorReviewPreparedMessage(result.PacketPath, result.ReviewID)}}, nil
	case "finalize":
		result, finalizeErr := policyEngine.FinalizeBehaviorReview(ctx, options.base)
		if finalizeErr != nil {
			return commandResult{}, finalizeErr
		}
		return commandResult{quiet: true, messages: []string{behaviorReviewFinalizedMessage(result.ReceiptPath, result.ReviewID)}}, nil
	}
	return commandResult{}, fmt.Errorf("unknown behavior-review action %q", options.action)
}

func handleRegressionProof(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseRegressionProofOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	result, err := policyEngine.ProveRegression(ctx, options.base, options.suite, options.evidence, options.id, options.redExit)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{quiet: true, messages: []string{regressionProofMessage(result.ProofPath, result.ID)}}, nil
}

func behaviorReviewPreparedMessage(packetPath, reviewID string) string {
	return fmt.Sprintf("Behavior review prepared: %s (review %s)", packetPath, reviewID)
}

func behaviorReviewIntentCapturedMessage(journalPath, captureID string) string {
	return fmt.Sprintf("Behavior review intent captured: %s (intent %s)", journalPath, captureID)
}

func behaviorReviewFinalizedMessage(receiptPath, reviewID string) string {
	return fmt.Sprintf("Behavior review finalized: %s (review %s)", receiptPath, reviewID)
}

func regressionProofMessage(proofPath, proofID string) string {
	return fmt.Sprintf("Regression proof recorded: %s (proof %s)", proofPath, proofID)
}

func parseBehaviorReviewOptions(arguments []string) (behaviorReviewOptions, error) {
	if len(arguments) == 0 {
		return behaviorReviewOptions{}, fmt.Errorf("behavior-review requires capture-intent, prepare, or finalize")
	}
	options := behaviorReviewOptions{action: arguments[0]}
	switch options.action {
	case "capture-intent":
		if err := parseBehaviorReviewCaptureIntent(&options, arguments[1:]); err != nil {
			return behaviorReviewOptions{}, err
		}
	case "prepare":
		if err := parseBehaviorReviewPrepare(&options, arguments[1:]); err != nil {
			return behaviorReviewOptions{}, err
		}
	case "finalize":
		if err := parseBehaviorReviewFinalize(&options, arguments[1:]); err != nil {
			return behaviorReviewOptions{}, err
		}
	default:
		return behaviorReviewOptions{}, fmt.Errorf("unknown behavior-review action %q", options.action)
	}
	return options, nil
}

func parseBehaviorReviewCaptureIntent(options *behaviorReviewOptions, arguments []string) error {
	for index := 0; index < len(arguments); {
		value, consumed, matched, err := namedOptionValue(arguments[index:], "--intent-file")
		if !matched {
			return fmt.Errorf("unknown behavior-review capture-intent option %q", arguments[index])
		}
		if err != nil {
			return err
		}
		if options.intentFile != "" {
			return errorsDuplicateOption("behavior-review capture-intent", "--intent-file")
		}
		options.intentFile = value
		index += consumed
	}
	if options.intentFile == "" {
		return fmt.Errorf("behavior-review capture-intent requires exactly one --intent-file PATH")
	}
	return nil
}

func parseBehaviorReviewPrepare(options *behaviorReviewOptions, arguments []string) error {
	for index := 0; index < len(arguments); {
		value, consumed, matched, err := namedOptionValue(arguments[index:], "--base")
		if matched {
			if err != nil {
				return err
			}
			if options.base != "" {
				return errorsDuplicateOption("behavior-review prepare", "--base")
			}
			options.base = value
			index += consumed
			continue
		}
		return fmt.Errorf("unknown behavior-review prepare option %q", arguments[index])
	}
	if options.base == "" {
		return fmt.Errorf("behavior-review prepare requires exactly one --base REF")
	}
	return nil
}

func parseBehaviorReviewFinalize(options *behaviorReviewOptions, arguments []string) error {
	for index := 0; index < len(arguments); {
		value, consumed, matched, err := namedOptionValue(arguments[index:], "--base")
		if !matched {
			return fmt.Errorf("unknown behavior-review finalize option %q", arguments[index])
		}
		if err != nil {
			return err
		}
		if options.base != "" {
			return errorsDuplicateOption("behavior-review finalize", "--base")
		}
		options.base = value
		index += consumed
	}
	if options.base == "" {
		return fmt.Errorf("behavior-review finalize requires exactly one --base REF")
	}
	return nil
}

func parseRegressionProofOptions(arguments []string) (regressionProofOptions, error) {
	options := regressionProofOptions{redExit: 1}
	redExitSelected := false
	for len(arguments) > 0 {
		consumed, err := parseRegressionProofOption(arguments, &options, &redExitSelected)
		if err != nil {
			return regressionProofOptions{}, err
		}
		arguments = arguments[consumed:]
	}
	if err := validateRegressionProofOptions(options); err != nil {
		return regressionProofOptions{}, err
	}
	return options, nil
}

func parseRegressionProofOption(arguments []string, options *regressionProofOptions, redExitSelected *bool) (int, error) {
	option, _, _ := strings.Cut(arguments[0], "=")
	if !slices.Contains(regressionProofOptionNames, option) {
		return 0, fmt.Errorf("unknown regression-proof option %q", arguments[0])
	}
	value, consumed, _, err := namedOptionValue(arguments, option)
	if err != nil {
		return 0, err
	}
	switch option {
	case "--base":
		err = setRegressionProofValue(&options.base, value, option)
	case "--suite":
		err = setRegressionProofValue(&options.suite, value, option)
	case "--evidence":
		err = appendRegressionProofEvidence(options, value)
	case "--id":
		err = setRegressionProofValue(&options.id, value, option)
	case "--red-exit":
		err = setRegressionProofRedExit(options, redExitSelected, value)
	}
	return consumed, err
}

func setRegressionProofValue(target *string, value, option string) error {
	if *target != "" {
		return errorsDuplicateOption("regression-proof", option)
	}
	*target = value
	return nil
}

func appendRegressionProofEvidence(options *regressionProofOptions, value string) error {
	if slices.Contains(options.evidence, value) {
		return errorsDuplicateOption("regression-proof", "--evidence "+value)
	}
	options.evidence = append(options.evidence, value)
	return nil
}

func setRegressionProofRedExit(options *regressionProofOptions, selected *bool, value string) error {
	if *selected {
		return errorsDuplicateOption("regression-proof", "--red-exit")
	}
	status, err := strconv.Atoi(value)
	if err != nil || status < 1 || status > 255 {
		return fmt.Errorf("--red-exit requires an integer from 1 through 255")
	}
	options.redExit = status
	*selected = true
	return nil
}

func validateRegressionProofOptions(options regressionProofOptions) error {
	if options.base == "" || options.suite == "" || len(options.evidence) == 0 || options.id == "" {
		return fmt.Errorf("regression-proof requires exactly one --base REF, --suite NAME, --id ID, and at least one --evidence PATH")
	}
	return nil
}

func namedOptionValue(arguments []string, option string) (string, int, bool, error) {
	argument := arguments[0]
	if argument == option {
		if len(arguments) < 2 || arguments[1] == "" || strings.HasPrefix(arguments[1], "--") {
			return "", 0, true, fmt.Errorf("%s requires a non-empty value", option)
		}
		return arguments[1], 2, true, nil
	}
	prefix := option + "="
	if !strings.HasPrefix(argument, prefix) {
		return "", 0, false, nil
	}
	value := strings.TrimPrefix(argument, prefix)
	if value == "" {
		return "", 0, true, fmt.Errorf("%s requires a non-empty value", option)
	}
	return value, 1, true, nil
}

func errorsDuplicateOption(command, option string) error {
	return fmt.Errorf("%s accepts %s only once", command, option)
}
