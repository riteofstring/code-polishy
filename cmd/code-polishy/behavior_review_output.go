package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

type behaviorReviewOutputDocument struct {
	Protocol string                              `json:"protocol"`
	Action   string                              `json:"action"`
	State    string                              `json:"state"`
	Capture  *engine.BehaviorReviewIntentCapture `json:"capture,omitempty"`
	Status   *engine.BehaviorReviewStatus        `json:"status,omitempty"`
}

func behaviorReviewConfirmation(options behaviorReviewOptions, document behaviorReviewOutputDocument, human string) (commandResult, error) {
	if options.format != "json" {
		return commandResult{quiet: true, messages: []string{human}}, nil
	}
	document.Protocol = "behavior-review/v1"
	document.Action = options.action
	document.State = "captured"
	if document.Status != nil {
		document.State = string(document.Status.State)
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return commandResult{}, fmt.Errorf("encode behavior review confirmation: %w", err)
	}
	return commandResult{quiet: true, messages: []string{string(data)}}, nil
}

func behaviorReviewFeatureList(features []string) string {
	if len(features) == 0 {
		return "none"
	}
	return strings.Join(features, ", ")
}

func parseBehaviorReviewFormat(options *behaviorReviewOptions, arguments []string) ([]string, error) {
	if options.action != "capture-intent" && options.action != "status" {
		return arguments, nil
	}
	remaining := []string{}
	seen := false
	for index := 0; index < len(arguments); {
		value, consumed, matched, err := namedOptionValue(arguments[index:], "--format")
		if !matched {
			remaining = append(remaining, arguments[index])
			index++
			continue
		}
		if err != nil {
			return nil, err
		}
		if seen {
			return nil, errorsDuplicateOption("behavior-review "+options.action, "--format")
		}
		if value != "human" && value != "json" {
			return nil, fmt.Errorf("behavior-review %s --format must be human or json", options.action)
		}
		options.format = value
		seen = true
		index += consumed
	}
	return remaining, nil
}
