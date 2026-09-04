package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

type reportOutputOptions struct {
	format  string
	output  string
	display engine.DisplayOptions
}

func parseReportOutputOptions(command string, arguments []string) (reportOutputOptions, []string, error) {
	options := reportOutputOptions{format: "human", display: engine.DisplayOptions{Limit: engine.DefaultFindingDisplayLimit}}
	if !reportOutputCommand(command) {
		return options, arguments, nil
	}
	remaining := []string{}
	seen := map[string]bool{}
	for index := 0; index < len(arguments); index++ {
		name, inline, recognized := reportOption(arguments[index])
		if !recognized {
			remaining = append(remaining, arguments[index])
			continue
		}
		value := inline
		if strings.Contains(arguments[index], "=") && value == "" {
			return options, nil, fmt.Errorf("%s requires a value", name)
		}
		if value == "" {
			if index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "--") {
				return options, nil, fmt.Errorf("%s requires a value", name)
			}
			index++
			value = arguments[index]
		}
		if err := consumeReportOption(&options, seen, name, value); err != nil {
			return options, nil, err
		}
	}
	return options, remaining, validateReportOutputOptions(options)
}

func reportOutputCommand(command string) bool {
	switch command {
	case "change-boundary", "doctor", "gate", "checkpoint-gate", "merge-gate", "check", "architecture", "format", "fix", "test", "test-plan", "test-levels", "verify", "supply-chain", "dependency-review", "artifact-security":
		return true
	default:
		return false
	}
}

func reportOption(argument string) (string, string, bool) {
	for _, name := range []string{"--format", "--output", "--filter-rule", "--filter-module", "--filter-path", "--filter-relation", "--group-by", "--display-limit"} {
		if argument == name {
			return name, "", true
		}
		if strings.HasPrefix(argument, name+"=") {
			return name, strings.TrimPrefix(argument, name+"="), true
		}
	}
	return "", "", false
}

func consumeReportOption(options *reportOutputOptions, seen map[string]bool, name, value string) error {
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%s has an invalid value", name)
	}
	if reportFilterOption(name) {
		return appendReportFilter(options, name, value)
	}
	return consumeSingleReportOption(options, seen, name, value)
}

func reportFilterOption(name string) bool {
	switch name {
	case "--filter-rule", "--filter-module", "--filter-path", "--filter-relation":
		return true
	default:
		return false
	}
}

func consumeSingleReportOption(options *reportOutputOptions, seen map[string]bool, name, value string) error {
	if seen[name] {
		return fmt.Errorf("%s may be specified only once", name)
	}
	seen[name] = true
	switch name {
	case "--format":
		options.format = value
	case "--output":
		options.output = value
	case "--group-by":
		options.display.GroupBy = value
	case "--display-limit":
		limit, err := strconv.Atoi(value)
		if err != nil {
			return errors.New("--display-limit must be an integer")
		}
		options.display.Limit = limit
	}
	return nil
}

func appendReportFilter(options *reportOutputOptions, name, value string) error {
	switch name {
	case "--filter-rule":
		options.display.Filters.Rules = append(options.display.Filters.Rules, value)
	case "--filter-module":
		options.display.Filters.Modules = append(options.display.Filters.Modules, value)
	case "--filter-path":
		options.display.Filters.Paths = append(options.display.Filters.Paths, value)
	case "--filter-relation":
		relation := policy.SelectionRelation(value)
		if relation != policy.SelectionSelected && relation != policy.SelectionRelated && relation != policy.SelectionContext && relation != policy.SelectionGlobal {
			return errors.New("--filter-relation must be selected, related, context, or global")
		}
		options.display.Filters.Relations = append(options.display.Filters.Relations, relation)
	}
	return nil
}

func validateReportOutputOptions(options reportOutputOptions) error {
	if options.format != "human" && options.format != "json" && options.format != "sarif" {
		return errors.New("--format must be human, json, or sarif")
	}
	if options.display.GroupBy != "" && options.display.GroupBy != "rule" && options.display.GroupBy != "module" && options.display.GroupBy != "path" && options.display.GroupBy != "relation" {
		return errors.New("--group-by must be rule, module, path, or relation")
	}
	if options.display.Limit < 1 || options.display.Limit > engine.MaximumFindingDisplayLimit {
		return fmt.Errorf("--display-limit must be between 1 and %d", engine.MaximumFindingDisplayLimit)
	}
	return nil
}

func renderReportOutput(policyEngine *engine.Engine, report engine.Report, verbose bool, options reportOutputOptions) error {
	var data []byte
	var err error
	switch options.format {
	case "human":
		if options.output == "" {
			printReportWithMode(os.Stdout, os.Stderr, report, verbose)
			return nil
		}
		buffer := &bytes.Buffer{}
		printReportWithMode(buffer, buffer, report, verbose)
		data = buffer.Bytes()
	case "json":
		data, err = engine.JSONReport(report)
	case "sarif":
		data, err = engine.SARIF(report)
	}
	if err != nil {
		return fmt.Errorf("render %s report: %w", options.format, err)
	}
	if options.output == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if policyEngine == nil {
		return errors.New("report output requires an open policy engine")
	}
	path, err := policyEngine.WriteReportOutput(options.output, data)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "REPORT OUTPUT:", path)
	return nil
}
