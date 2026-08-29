package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func handleDesignContext(_ context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseDesignContextOptions(arguments)
	if err != nil {
		return commandResult{}, err
	}
	documents, findings, err := policyEngine.DesignContext(options.mode, options.files, options.modules)
	if err != nil {
		return commandResult{}, err
	}
	if len(findings) > 0 {
		return commandResult{report: engine.Report{Findings: findings}}, nil
	}
	for _, document := range documents {
		fmt.Println(document)
	}
	return commandResult{quiet: true}, nil
}

type designContextOptions struct {
	mode              string
	files             []string
	modules           []string
	selectionSelected bool
}

func parseDesignContextOptions(arguments []string) (designContextOptions, error) {
	options := designContextOptions{mode: "changes"}
	for index := 0; index < len(arguments); {
		consumed, err := parseDesignContextOption(&options, arguments[index:])
		if err != nil {
			return options, err
		}
		index += consumed
	}
	return options, nil
}

func parseDesignContextOption(options *designContextOptions, arguments []string) (int, error) {
	if isDesignContextModuleOption(arguments[0]) {
		return parseDesignContextModule(options, arguments)
	}
	return parseDesignContextSelection(options, arguments)
}

func isDesignContextModuleOption(argument string) bool {
	return argument == "--module" || strings.HasPrefix(argument, "--module=")
}

func parseDesignContextModule(options *designContextOptions, arguments []string) (int, error) {
	if options.selectionSelected {
		return 0, errors.New("choose only one of file selection or --module")
	}
	value, consumed, err := designContextModuleValue(arguments)
	if err != nil {
		return 0, err
	}
	options.modules = append(options.modules, value)
	return consumed, nil
}

func designContextModuleValue(arguments []string) (string, int, error) {
	if arguments[0] == "--module" {
		if len(arguments) < 2 || strings.HasPrefix(arguments[1], "--") {
			return "", 0, errors.New("--module requires a non-empty value")
		}
		return arguments[1], 2, nil
	}
	value := strings.TrimPrefix(arguments[0], "--module=")
	if value == "" {
		return "", 0, errors.New("--module requires a non-empty value")
	}
	return value, 1, nil
}

func parseDesignContextSelection(options *designContextOptions, arguments []string) (int, error) {
	mode, known := selectionMode(arguments[0])
	if !known {
		return 0, fmt.Errorf("unknown design-context option %q", arguments[0])
	}
	if options.selectionSelected || len(options.modules) > 0 {
		return 0, errors.New("choose only one of file selection or --module")
	}
	options.mode = mode
	options.selectionSelected = true
	if mode != "files" {
		return 1, nil
	}
	files := designContextFiles(arguments[1:])
	if len(files) == 0 {
		return 0, errors.New("--files needs at least one path")
	}
	options.files = append(options.files, files...)
	return len(files) + 1, nil
}

func designContextFiles(arguments []string) []string {
	files := []string{}
	for _, argument := range arguments {
		if strings.HasPrefix(argument, "--") {
			break
		}
		files = append(files, argument)
	}
	return files
}
