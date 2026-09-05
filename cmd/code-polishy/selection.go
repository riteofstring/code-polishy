package main

import (
	"fmt"
	"strings"
)

type selectionArguments struct {
	mode     string
	operands []string
	selected bool
}

func parseSelection(arguments []string) (string, []string, error) {
	parsed := selectionArguments{mode: "changes"}
	for index := 0; index < len(arguments); index++ {
		next, err := parsed.consume(arguments, index)
		if err != nil {
			return "", nil, err
		}
		index = next
	}
	return parsed.result()
}

func (parsed *selectionArguments) consume(arguments []string, index int) (int, error) {
	option, inline := splitSelectionOption(arguments[index])
	mode, known := selectionMode(option)
	if !known {
		return index, fmt.Errorf("unknown selection option %q", arguments[index])
	}
	if err := parsed.selectMode(mode); err != nil {
		return index, err
	}
	if !selectionHasOperands(mode) {
		if inline != "" {
			return index, fmt.Errorf("%s does not accept a value", option)
		}
		return index, nil
	}
	if inline != "" {
		parsed.operands = append(parsed.operands, inline)
		return index, nil
	}
	next, operands := followingSelectionOperands(arguments, index+1)
	if len(operands) == 0 {
		return index, fmt.Errorf("%s needs at least one %s", option, selectionOperandLabel(mode))
	}
	parsed.operands = append(parsed.operands, operands...)
	return next, nil
}

func (parsed *selectionArguments) selectMode(mode string) error {
	if parsed.selected && parsed.mode != mode {
		return fmt.Errorf("choose only one evaluation selection mode")
	}
	if parsed.selected && !selectionHasOperands(mode) {
		return fmt.Errorf("evaluation selector is repeated: %s", selectionOption(mode))
	}
	parsed.mode = mode
	parsed.selected = true
	return nil
}

func (parsed selectionArguments) result() (string, []string, error) {
	if selectionHasOperands(parsed.mode) && len(parsed.operands) == 0 {
		return "", nil, fmt.Errorf("%s needs at least one %s", selectionOption(parsed.mode), selectionOperandLabel(parsed.mode))
	}
	return parsed.mode, parsed.operands, nil
}

func followingSelectionOperands(arguments []string, start int) (int, []string) {
	index := start
	for index < len(arguments) && !strings.HasPrefix(arguments[index], "--") {
		index++
	}
	return index - 1, arguments[start:index]
}

func splitSelectionOption(argument string) (string, string) {
	for _, option := range []string{"--files", "--module"} {
		if strings.HasPrefix(argument, option+"=") {
			return option, strings.TrimPrefix(argument, option+"=")
		}
	}
	return argument, ""
}

func selectionHasOperands(mode string) bool {
	return mode == "files" || mode == "modules"
}

func selectionOperandLabel(mode string) string {
	if mode == "modules" {
		return "declared module name"
	}
	return "file or directory path"
}

func selectionOption(mode string) string {
	switch mode {
	case "modules":
		return "--module"
	case "files":
		return "--files"
	case "staged":
		return "--staged"
	case "all":
		return "--all"
	default:
		return "--git-changes"
	}
}

func selectionMode(option string) (string, bool) {
	switch option {
	case "--git-changes":
		return "changes", true
	case "--staged":
		return "staged", true
	case "--all":
		return "all", true
	case "--files":
		return "files", true
	case "--module":
		return "modules", true
	default:
		return "", false
	}
}
