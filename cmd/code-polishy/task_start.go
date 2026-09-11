package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func handleTaskStart(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseTaskStartOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	if options.request.IntentPath == "-" {
		options.request.IntentPath = ""
		options.request.Intent = os.Stdin
	}
	data, err := policyEngine.TaskStart(ctx, options.request)
	if err != nil {
		return commandResult{}, err
	}
	message, err := renderTaskStart(data, options.format)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{quiet: true, messages: []string{message}}, nil
}

type taskStartOptions struct {
	request engine.TaskStartRequest
	format  string
}

func parseTaskStartOptions(arguments []string) (taskStartOptions, error) {
	options := taskStartOptions{
		request: engine.TaskStartRequest{Context: engine.ContextRequest{Workflow: "task-start"}},
		format:  "human",
	}
	seen := map[string]bool{}
	for len(arguments) > 0 {
		name, _, _ := strings.Cut(arguments[0], "=")
		value, consumed, _, err := namedOptionValue(arguments, name)
		if err != nil {
			return options, err
		}
		if seen[name] && name != "--feature" && name != "--situation" {
			return options, errorsDuplicateOption("task-start", name)
		}
		seen[name] = true
		if err := applyTaskStartOption(&options, name, value); err != nil {
			return options, err
		}
		arguments = arguments[consumed:]
	}
	if seen["--files"] == seen["--module"] {
		return options, fmt.Errorf("task-start requires exactly one --files PATH or --module NAME")
	}
	return options, nil
}

func applyTaskStartOption(options *taskStartOptions, name, value string) error {
	switch name {
	case "--intent-file":
		options.request.IntentPath = value
	case "--files":
		options.request.Context.Mode, options.request.Context.Files = "files", []string{value}
	case "--module":
		options.request.Context.Mode, options.request.Context.Modules = "modules", []string{value}
	case "--feature":
		options.request.Features = append(options.request.Features, value)
	case "--situation":
		options.request.Context.Situations = append(options.request.Context.Situations, value)
	case "--format":
		if value != "human" && value != "json" {
			return fmt.Errorf("task-start --format must be human or json")
		}
		options.format = value
	default:
		return fmt.Errorf("unknown task-start option %q", name)
	}
	return nil
}
