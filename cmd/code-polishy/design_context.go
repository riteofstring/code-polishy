package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func handleDesignContext(_ context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseDesignContextOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	report, err := policyEngine.DesignContext(engine.ContextRequest{
		Mode: options.mode, Files: options.files, Modules: options.modules, Situations: options.situations, Workflow: "design-context",
	})
	return commandResult{report: report}, err
}

type designContextOptions struct {
	mode              string
	files             []string
	modules           []string
	situations        []string
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
	if len(options.situations) > 0 && !options.selectionSelected && len(options.modules) == 0 {
		options.mode = "none"
	}
	return options, policy.ValidateHandoffSituations(options.situations)
}

func parseDesignContextOption(options *designContextOptions, arguments []string) (int, error) {
	if arguments[0] == "--situation" || strings.HasPrefix(arguments[0], "--situation=") {
		value, consumed, err := designContextSituationValue(arguments)
		if err == nil {
			options.situations = append(options.situations, value)
		}
		return consumed, err
	}
	if isDesignContextModuleOption(arguments[0]) {
		return parseDesignContextModule(options, arguments)
	}
	return parseDesignContextSelection(options, arguments)
}

func designContextSituationValue(arguments []string) (string, int, error) {
	if strings.HasPrefix(arguments[0], "--situation=") {
		return strings.TrimPrefix(arguments[0], "--situation="), 1, nil
	}
	if len(arguments) < 2 || strings.HasPrefix(arguments[1], "--") {
		return "", 0, errors.New("--situation requires one exact situation identifier")
	}
	return arguments[1], 2, nil
}

func printRepositoryContext(output io.Writer, context *engine.RepositoryContext) {
	if context == nil {
		return
	}
	for _, document := range context.DesignDocuments {
		fmt.Fprintln(output, "DESIGN DOCUMENT:", document.Path)
	}
	for _, handoff := range context.Handoffs {
		fmt.Fprintf(output, "HANDOFF %s: %s\n  DOCUMENT: %s\n  SHA256: %s\n", handoff.Name, handoff.Description, handoff.Document.Path, handoff.Document.SHA256)
		for _, reason := range handoff.Reasons {
			fmt.Fprintf(output, "  SELECTED BY: %s %s\n", reason.Kind, reason.Value)
		}
	}
	if len(context.DesignDocuments)+len(context.Handoffs) == 0 {
		fmt.Fprintln(output, "CONTEXT: no current documents or operational handoffs match this selection")
	}
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
