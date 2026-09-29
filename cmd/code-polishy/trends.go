package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func handleTrends(_ context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseTrendsOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	report, err := policyEngine.Trends(options)
	return commandResult{report: report}, err
}

func parseTrendsOptions(arguments []string) (engine.TrendsOptions, error) {
	if err := rejectRepeatedTrendsOptions(arguments); err != nil {
		return engine.TrendsOptions{}, err
	}
	flags := flag.NewFlagSet("trends", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	since := flags.String("since", "", "start at the week containing this YYYY-MM-DD date")
	branch := flags.String("branch", "", "follow this branch or commit instead of HEAD")
	rewriteDays := flags.Int("rewrite-days", 0, "count a new line as rewritten when it changes within this many days")
	if err := flags.Parse(arguments); err != nil {
		return engine.TrendsOptions{}, err
	}
	if flags.NArg() != 0 {
		return engine.TrendsOptions{}, fmt.Errorf("unexpected trends arguments: %s", strings.Join(flags.Args(), " "))
	}
	set := map[string]bool{}
	flags.Visit(func(option *flag.Flag) { set[option.Name] = true })
	if set["branch"] && *branch == "" {
		return engine.TrendsOptions{}, errors.New("trends --branch requires a non-empty Git reference")
	}
	if set["rewrite-days"] && *rewriteDays < 1 {
		return engine.TrendsOptions{}, errors.New("trends --rewrite-days requires a positive number of days")
	}
	start, err := parseTrendsSince(*since, set["since"])
	if err != nil {
		return engine.TrendsOptions{}, err
	}
	return engine.TrendsOptions{Branch: *branch, Since: start, RewriteDays: *rewriteDays}, nil
}

func rejectRepeatedTrendsOptions(arguments []string) error {
	for _, name := range []string{"since", "branch", "rewrite-days"} {
		if optionOccurrences(arguments, name) > 1 {
			return fmt.Errorf("trends accepts --%s at most once", name)
		}
	}
	return nil
}

func parseTrendsSince(value string, set bool) (time.Time, error) {
	if !set {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, errors.New("trends --since requires a date in YYYY-MM-DD form")
	}
	return parsed, nil
}

func optionOccurrences(arguments []string, name string) int {
	count := 0
	for _, argument := range arguments {
		if argument == "--"+name || strings.HasPrefix(argument, "--"+name+"=") {
			count++
		}
	}
	return count
}
