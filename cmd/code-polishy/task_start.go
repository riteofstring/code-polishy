package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func handleTaskStart(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	request, err := parseTaskStartOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	data, err := policyEngine.TaskStart(ctx, request)
	if err != nil {
		return commandResult{}, err
	}
	return commandResult{quiet: true, messages: []string{strings.TrimSuffix(string(data), "\n")}}, nil
}

func parseTaskStartOptions(arguments []string) (engine.TaskStartRequest, error) {
	request := engine.TaskStartRequest{Context: engine.ContextRequest{Workflow: "task-start"}}
	seen := map[string]bool{}
	for len(arguments) > 0 {
		name, _, _ := strings.Cut(arguments[0], "=")
		value, consumed, _, err := namedOptionValue(arguments, name)
		if err != nil {
			return request, err
		}
		if seen[name] && name != "--feature" && name != "--situation" {
			return request, errorsDuplicateOption("task-start", name)
		}
		seen[name] = true
		if err := applyTaskStartOption(&request, name, value); err != nil {
			return request, err
		}
		arguments = arguments[consumed:]
	}
	if request.IntentPath == "" || seen["--files"] == seen["--module"] {
		return request, fmt.Errorf("task-start requires --intent-file PATH and exactly one --files PATH or --module NAME")
	}
	return request, nil
}

func applyTaskStartOption(request *engine.TaskStartRequest, name, value string) error {
	switch name {
	case "--intent-file":
		request.IntentPath = value
	case "--files":
		request.Context.Mode, request.Context.Files = "files", []string{value}
	case "--module":
		request.Context.Mode, request.Context.Modules = "modules", []string{value}
	case "--feature":
		request.Features = append(request.Features, value)
	case "--situation":
		request.Context.Situations = append(request.Context.Situations, value)
	case "--format":
		if value != "json" {
			return fmt.Errorf("task-start emits one JSON packet; --format must be json")
		}
	default:
		return fmt.Errorf("unknown task-start option %q", name)
	}
	return nil
}
