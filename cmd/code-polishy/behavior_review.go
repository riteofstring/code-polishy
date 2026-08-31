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
	features   []string
}

type regressionProofOptions struct {
	base     string
	suite    string
	evidence []string
	id       string
	redExit  int
}

var regressionProofOptionNames = []string{"--base", "--suite", "--evidence", "--id", "--red-exit"}

type behaviorReviewAction struct {
	parse  func(*behaviorReviewOptions, []string) error
	handle func(context.Context, *engine.Engine, behaviorReviewOptions) (commandResult, error)
}

var behaviorReviewActions = map[string]behaviorReviewAction{
	"capture-intent": {parse: parseBehaviorReviewCaptureIntent, handle: handleBehaviorReviewCaptureIntent},
	"require":        {parse: parseBehaviorReviewRequire, handle: handleBehaviorReviewRequire},
	"status":         {parse: parseBehaviorReviewStatus, handle: handleBehaviorReviewStatus},
	"prepare":        {parse: parseBehaviorReviewPrepare, handle: handleBehaviorReviewPrepare},
	"finalize":       {parse: parseBehaviorReviewFinalize, handle: handleBehaviorReviewFinalize},
}

func handleBehaviorReview(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseBehaviorReviewOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	return behaviorReviewActions[options.action].handle(ctx, policyEngine, options)
}

func handleBehaviorReviewCaptureIntent(ctx context.Context, policyEngine *engine.Engine, options behaviorReviewOptions) (commandResult, error) {
	result, err := policyEngine.CaptureBehaviorReviewIntent(ctx, options.intentFile, options.features)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{quiet: true, messages: []string{behaviorReviewIntentCapturedMessage(result.JournalPath, result.ID)}}, nil
}

func handleBehaviorReviewRequire(ctx context.Context, policyEngine *engine.Engine, options behaviorReviewOptions) (commandResult, error) {
	result, err := policyEngine.RequireBehaviorReview(ctx, options.base, options.features)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{quiet: true, messages: []string{behaviorReviewRequirementAddedMessage(result.JournalPath, result.ID, result.Features)}}, nil
}

func handleBehaviorReviewStatus(ctx context.Context, policyEngine *engine.Engine, options behaviorReviewOptions) (commandResult, error) {
	status, err := policyEngine.BehaviorReviewStatus(ctx, options.base)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{quiet: true, messages: []string{behaviorReviewStatusMessage(status)}}, nil
}

func handleBehaviorReviewPrepare(ctx context.Context, policyEngine *engine.Engine, options behaviorReviewOptions) (commandResult, error) {
	result, err := policyEngine.PrepareBehaviorReview(ctx, options.base)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{quiet: true, messages: []string{behaviorReviewPreparedMessage(result.PacketPath, result.ReviewID)}}, nil
}

func handleBehaviorReviewFinalize(ctx context.Context, policyEngine *engine.Engine, options behaviorReviewOptions) (commandResult, error) {
	result, err := policyEngine.FinalizeBehaviorReview(ctx, options.base)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{quiet: true, messages: []string{behaviorReviewFinalizedMessage(result.ReceiptPath, result.ReviewID)}}, nil
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

func behaviorReviewRequirementAddedMessage(journalPath, requirementID string, features []string) string {
	return fmt.Sprintf("Behavior review requirement added: %s (requirement %s; features %s)", journalPath, requirementID, strings.Join(features, ", "))
}

func behaviorReviewStatusMessage(status engine.BehaviorReviewStatus) string {
	var output strings.Builder
	printBehaviorReview(&output, &status, false)
	for _, list := range []struct {
		label  string
		values []string
	}{
		{label: "AFFECTED FEATURES", values: status.Affected},
		{label: "CONFIGURED FEATURES", values: status.Configured},
		{label: "TASK-REQUESTED FEATURES", values: status.TaskRequested},
		{label: "REQUIRED FEATURES", values: status.Required},
		{label: "COMPLETED FEATURES", values: status.Completed},
		{label: "MISSING FEATURES", values: status.Missing},
	} {
		values := "none"
		if len(list.values) > 0 {
			values = strings.Join(list.values, ", ")
		}
		fmt.Fprintf(&output, "%s: %s\n", list.label, values)
	}
	return strings.TrimSuffix(output.String(), "\n")
}

func behaviorReviewFinalizedMessage(receiptPath, reviewID string) string {
	return fmt.Sprintf("Behavior review finalized: %s (review %s)", receiptPath, reviewID)
}

func regressionProofMessage(proofPath, proofID string) string {
	return fmt.Sprintf("Regression proof recorded: %s (proof %s)", proofPath, proofID)
}

func parseBehaviorReviewOptions(arguments []string) (behaviorReviewOptions, error) {
	if len(arguments) == 0 {
		return behaviorReviewOptions{}, fmt.Errorf("behavior-review requires capture-intent, require, status, prepare, or finalize")
	}
	options := behaviorReviewOptions{action: arguments[0]}
	action, found := behaviorReviewActions[options.action]
	if !found {
		return behaviorReviewOptions{}, fmt.Errorf("unknown behavior-review action %q", options.action)
	}
	if err := action.parse(&options, arguments[1:]); err != nil {
		return behaviorReviewOptions{}, err
	}
	return options, nil
}

func parseBehaviorReviewStatus(options *behaviorReviewOptions, arguments []string) error {
	return parseBehaviorReviewBase(options, arguments, "status")
}

func parseBehaviorReviewPrepare(options *behaviorReviewOptions, arguments []string) error {
	return parseBehaviorReviewBase(options, arguments, "prepare")
}

func parseBehaviorReviewFinalize(options *behaviorReviewOptions, arguments []string) error {
	return parseBehaviorReviewBase(options, arguments, "finalize")
}

func parseBehaviorReviewCaptureIntent(options *behaviorReviewOptions, arguments []string) error {
	for index := 0; index < len(arguments); {
		value, consumed, matched, err := namedOptionValue(arguments[index:], "--intent-file")
		if matched {
			if err != nil {
				return err
			}
			if options.intentFile != "" {
				return errorsDuplicateOption("behavior-review capture-intent", "--intent-file")
			}
			options.intentFile = value
			index += consumed
			continue
		}
		value, consumed, matched, err = namedOptionValue(arguments[index:], "--feature")
		if matched {
			if err != nil {
				return err
			}
			options.features = append(options.features, value)
			index += consumed
			continue
		}
		return fmt.Errorf("unknown behavior-review capture-intent option %q", arguments[index])
	}
	if options.intentFile == "" {
		return fmt.Errorf("behavior-review capture-intent requires exactly one --intent-file PATH")
	}
	return nil
}

func parseBehaviorReviewRequire(options *behaviorReviewOptions, arguments []string) error {
	for index := 0; index < len(arguments); {
		value, consumed, matched, err := namedOptionValue(arguments[index:], "--base")
		if matched {
			if err != nil {
				return err
			}
			if options.base != "" {
				return errorsDuplicateOption("behavior-review require", "--base")
			}
			options.base = value
			index += consumed
			continue
		}
		value, consumed, matched, err = namedOptionValue(arguments[index:], "--feature")
		if matched {
			if err != nil {
				return err
			}
			options.features = append(options.features, value)
			index += consumed
			continue
		}
		return fmt.Errorf("unknown behavior-review require option %q", arguments[index])
	}
	if options.base == "" {
		return fmt.Errorf("behavior-review require requires exactly one --base REF")
	}
	if len(options.features) == 0 {
		return fmt.Errorf("behavior-review require requires at least one --feature NAME")
	}
	return nil
}

func parseBehaviorReviewBase(options *behaviorReviewOptions, arguments []string, action string) error {
	for index := 0; index < len(arguments); {
		value, consumed, matched, err := namedOptionValue(arguments[index:], "--base")
		if !matched {
			return fmt.Errorf("unknown behavior-review %s option %q", action, arguments[index])
		}
		if err != nil {
			return err
		}
		if options.base != "" {
			return errorsDuplicateOption("behavior-review "+action, "--base")
		}
		options.base = value
		index += consumed
	}
	if options.base == "" {
		return fmt.Errorf("behavior-review %s requires exactly one --base REF", action)
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
