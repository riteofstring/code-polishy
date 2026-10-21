package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/release"
)

func handleCleanupMeta(invocation invocation) int {
	flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dryRun := flags.Bool("dry-run", false, "report what cleanup would remove without removing it")
	reports := flags.Bool("reports", false, "remove generated repository reports and verification caches")
	artifacts := flags.Bool("artifacts", false, "remove completed owned test artifacts")
	if err := flags.Parse(invocation.arguments); err != nil {
		return commandUsageError("cleanup", err.Error())
	}
	if flags.NArg() != 0 || invocation.configPath != "" {
		return commandUsageError("cleanup", "cleanup accepts only --dry-run, --reports, and --artifacts")
	}
	if *reports || *artifacts {
		options := engine.CleanupOptions{Reports: *reports, Artifacts: *artifacts, DryRun: *dryRun}
		result, err := engine.CleanupRepository(invocation.repoRoot, options)
		writeRepositoryCleanupResult(os.Stdout, result, options, err == nil)
		if err != nil {
			return operationalError(err)
		}
		return 0
	}
	result, err := release.CleanReleases(invocation.repoRoot, invocation.policyRoot, *dryRun)
	writeCleanupResult(os.Stdout, result, *dryRun, err == nil)
	if err != nil {
		return operationalError(err)
	}
	return 0
}

func writeRepositoryCleanupResult(output io.Writer, result engine.RepositoryCleanupResult, options engine.CleanupOptions, complete bool) {
	removal := "REMOVED"
	if options.DryRun {
		removal = "WOULD REMOVE"
	}
	for _, entry := range result.Kept {
		fmt.Fprintf(output, "KEPT %s: %s\n", entry.Path, entry.Reason)
	}
	for _, entry := range result.Removed {
		fmt.Fprintf(output, "%s %s (%s)\n", removal, entry.Path, engine.FormatSizeBytes(entry.Bytes))
	}
	if options.Reports && len(result.Removed) > 0 {
		fmt.Fprintln(output, "NOTE removing reports discards cached gate results and test receipts; verification will rerun as needed.")
	}
	if !complete {
		return
	}
	removed, freed := findingCount(len(result.Removed), "repository output"), engine.FormatSizeBytes(result.ReclaimedBytes())
	if options.DryRun {
		fmt.Fprintf(output, "PASS cleanup dry run: removing %s would free %s\n", removed, freed)
		return
	}
	fmt.Fprintf(output, "PASS cleanup removed %s and freed %s\n", removed, freed)
}

func writeCleanupResult(output io.Writer, result release.CleanupResult, dryRun, complete bool) {
	removal := "REMOVED"
	if dryRun {
		removal = "WOULD REMOVE"
	}
	for _, kept := range result.Kept {
		fmt.Fprintf(output, "KEPT %s for %s\n", kept.Name, strings.Join(kept.Repositories, ", "))
	}
	for _, removed := range result.Removed {
		fmt.Fprintf(output, "%s %s (%s)\n", removal, removed.Name, engine.FormatSizeBytes(removed.Bytes))
	}
	for _, repository := range result.ForgottenRepositories {
		fmt.Fprintf(output, "FORGOT %s: it no longer has %s\n", repository, release.LockFilename)
	}
	if !complete {
		return
	}
	removed, freed := findingCount(len(result.Removed), "release"), engine.FormatSizeBytes(result.ReclaimedBytes())
	if dryRun {
		fmt.Fprintf(output, "PASS cleanup dry run: removing %s would free %s under %s\n", removed, freed, result.Prefix)
		return
	}
	fmt.Fprintf(output, "PASS cleanup removed %s and freed %s under %s\n", removed, freed, result.Prefix)
}
