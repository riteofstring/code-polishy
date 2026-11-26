package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func parseSizeOptions(arguments []string) (string, []engine.BudgetProposal, error) {
	baseArguments, proposals := []string{}, []engine.BudgetProposal{}
	rationale := ""
	seen := map[string]bool{}
	for len(arguments) > 0 {
		name, value, consumed, err := parseSizeOption(arguments, seen)
		if err != nil {
			return "", nil, err
		}
		seen[name], arguments = true, arguments[consumed:]
		switch name {
		case "--base":
			baseArguments = append(baseArguments, name, value)
		case "--growth-rationale":
			rationale = value
		case "--propose-budget":
			proposal, err := parseProposedBudget(value, seen)
			if err != nil {
				return "", nil, err
			}
			seen[proposal.Scope] = true
			proposals = append(proposals, proposal)
		}
	}
	if !validBudgetRationale(proposals, rationale) {
		return "", nil, fmt.Errorf("pair --propose-budget with one --growth-rationale of at most 2000 bytes; this only previews a ceiling and never changes policy")
	}
	for index := range proposals {
		proposals[index].Rationale = rationale
	}
	base, err := parseSizeBaseOption(baseArguments)
	return base, proposals, err
}

func parseSizeOption(arguments []string, seen map[string]bool) (string, string, int, error) {
	name, _, _ := strings.Cut(arguments[0], "=")
	if name != "--base" && name != "--propose-budget" && name != "--growth-rationale" {
		return "", "", 0, fmt.Errorf("unknown size option %q", name)
	}
	value, consumed, _, err := namedOptionValue(arguments, name)
	if err != nil || seen[name] && name != "--propose-budget" {
		return "", "", 0, fmt.Errorf("size %s requires one value and cannot repeat", name)
	}
	return name, value, consumed, nil
}

func parseProposedBudget(value string, seen map[string]bool) (engine.BudgetProposal, error) {
	scope, ceiling, found := strings.Cut(value, "=")
	count, err := strconv.Atoi(ceiling)
	if !found || err != nil || count <= 0 || scope != "production" && scope != "test" && scope != "total" || seen[scope] {
		return engine.BudgetProposal{}, fmt.Errorf("--propose-budget needs a unique production=LINES, test=LINES, or total=LINES with a positive ceiling")
	}
	return engine.BudgetProposal{Scope: scope, Ceiling: count}, nil
}

func validBudgetRationale(proposals []engine.BudgetProposal, rationale string) bool {
	return (len(proposals) > 0) == (strings.TrimSpace(rationale) != "") && len(rationale) <= 2000 && !strings.ContainsAny(rationale, "\x00\r\n")
}
