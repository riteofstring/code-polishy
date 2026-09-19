package main

import (
	"context"
	"fmt"
	"os"
	"slices"
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
		consumed, err := parseTaskStartOption(&options, seen, arguments)
		if err != nil {
			return options, err
		}
		arguments = arguments[consumed:]
	}
	if seen["--files"] == seen["--module"] {
		return options, fmt.Errorf("task-start requires one or more --files PATH operands or --module NAME options")
	}
	return options, nil
}

func parseTaskStartOption(options *taskStartOptions, seen map[string]bool, arguments []string) (int, error) {
	name, _, _ := strings.Cut(arguments[0], "=")
	if name == "--files" && arguments[0] == "--files" {
		return parseTaskStartFiles(options, seen, arguments)
	}
	value, consumed, _, err := namedOptionValue(arguments, name)
	if err != nil {
		return 0, err
	}
	if seen[name] && !slices.Contains([]string{"--files", "--module", "--feature", "--situation"}, name) {
		return 0, errorsDuplicateOption("task-start", name)
	}
	seen[name] = true
	return consumed, applyTaskStartOption(options, name, value)
}

func parseTaskStartFiles(options *taskStartOptions, seen map[string]bool, arguments []string) (int, error) {
	files := designContextFiles(arguments[1:])
	if len(files) == 0 {
		return 0, fmt.Errorf("--files needs at least one path")
	}
	seen["--files"] = true
	options.request.Context.Mode = "files"
	options.request.Context.Files = append(options.request.Context.Files, files...)
	return len(files) + 1, nil
}

func applyTaskStartOption(options *taskStartOptions, name, value string) error {
	switch name {
	case "--intent-file":
		options.request.IntentPath = value
	case "--files":
		options.request.Context.Mode = "files"
		options.request.Context.Files = append(options.request.Context.Files, value)
	case "--module":
		options.request.Context.Mode = "modules"
		options.request.Context.Modules = append(options.request.Context.Modules, value)
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
