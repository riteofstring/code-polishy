package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func handleArchitectureReview(ctx context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	action, base, err := parseArchitectureReviewOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	report, err := policyEngine.ArchitectureReview(ctx, base, action)
	return commandResult{report: report}, err
}

func parseArchitectureReviewOptions(arguments []string) (string, string, error) {
	if len(arguments) < 2 {
		return "", "", fmt.Errorf("architecture-review requires status, prepare, or finalize and exactly one --base REF")
	}
	action := arguments[0]
	switch action {
	case "status", "prepare", "finalize":
	default:
		return "", "", fmt.Errorf("unknown architecture-review action %q", action)
	}
	base, consumed, matched, err := namedOptionValue(arguments[1:], "--base")
	if err != nil {
		return "", "", err
	}
	if !matched || consumed != len(arguments)-1 || strings.TrimSpace(base) == "" {
		return "", "", fmt.Errorf("architecture-review %s requires exactly one --base REF", action)
	}
	return action, base, nil
}

func printArchitectureReview(output io.Writer, report engine.Report) {
	if prepared := report.ArchitecturePreparation; prepared != nil {
		fmt.Fprintf(output, "ARCHITECTURE REVIEW: prepared (%s)\nPACKET: %s\nRESULT: %s\n", prepared.ReviewID, prepared.PacketPath, prepared.ResultPath)
	}
	if review := report.ArchitectureReview; review != nil {
		fmt.Fprintf(output, "ARCHITECTURE REVIEW: %s\n", strings.ToUpper(review.State))
		if review.Reason != "" {
			fmt.Fprintln(output, review.Reason)
		}
	}
}
