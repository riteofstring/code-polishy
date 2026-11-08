package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/ownedoutput"
	"github.com/riteofstring/code-polishy/internal/release"
)

func handleCleanupMeta(invocation invocation) int {
	flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dryRun := flags.Bool("dry-run", false, "report what cleanup would remove without removing it")
	reports := flags.Bool("reports", false, "remove generated repository reports and verification caches")
	artifacts := flags.Bool("artifacts", false, "remove completed owned test artifacts")
	buildCache := flags.Bool("build-cache", false, "remove owned build caches; stop unmanaged consumers first")
	if err := flags.Parse(invocation.arguments); err != nil {
		return commandUsageError("cleanup", err.Error())
	}
	if flags.NArg() != 0 || invocation.configPath != "" {
		return commandUsageError("cleanup", "cleanup accepts only --dry-run, --reports, --artifacts, and --build-cache; repository scopes may be combined")
	}
	if *reports || *artifacts || *buildCache {
		options := engine.CleanupOptions{Reports: *reports, Artifacts: *artifacts, BuildCache: *buildCache, DryRun: *dryRun}
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
	cacheRemoved, cacheBytes, busy := writeBuildCacheEntries(output, result)
	if options.BuildCache && len(result.BuildCaches) > 0 {
		fmt.Fprintln(output, "NOTE build caches regenerate on next use. Stop unmanaged consumers and keep mount topology stable; new builds may overlap pending storage. Preview sizes are conditional estimates.")
	}
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
		fmt.Fprintln(output, "NOTE removing reports discards cached gate results, test receipts, and security evidence; verification will rerun as needed.")
	}
	if !complete {
		return
	}
	writeRepositoryCleanupSummary(output, result, options, cacheRemoved, cacheBytes, busy)
}

func writeRepositoryCleanupSummary(output io.Writer, result engine.RepositoryCleanupResult, options engine.CleanupOptions, cacheRemoved int, cacheBytes int64, busy bool) {
	count := len(result.Removed) + cacheRemoved
	if count == 0 && len(result.Kept)+len(result.BuildCaches) > 0 {
		token := "SKIPPED"
		if busy {
			token = "BUSY"
		}
		fmt.Fprintf(output, "%s cleanup retained all selected outputs; no bytes reclaimed\n", token)
		return
	}
	bytes := result.ReclaimedBytes()
	if options.DryRun {
		bytes += cacheBytes
	}
	removed, freed := findingCount(count, "repository output"), engine.FormatSizeBytes(bytes)
	if options.DryRun {
		if slices.ContainsFunc(result.BuildCaches, func(entry ownedoutput.Entry) bool { return entry.Action == "would-remove" && !entry.SizeKnown }) {
			fmt.Fprintf(output, "PASS cleanup dry run: removing %s; total logical size is unknown\n", removed)
			return
		}
		fmt.Fprintf(output, "PASS cleanup dry run: removing %s would free %s\n", removed, freed)
		return
	}
	fmt.Fprintf(output, "PASS cleanup removed %s and freed %s\n", removed, freed)
}

func writeBuildCacheEntries(output io.Writer, result engine.RepositoryCleanupResult) (int, int64, bool) {
	count, bytes, busy := 0, int64(0), false
	for _, entry := range result.BuildCaches {
		token := "SKIPPED"
		switch entry.Action {
		case "removed", "would-remove":
			count++
			bytes += entry.Bytes
			token = "REMOVED"
			if entry.Action == "would-remove" {
				token = "WOULD REMOVE"
			}
		case "busy":
			token, busy = "BUSY", true
		case "error":
			token = "ERROR"
		}
		fmt.Fprintf(output, "%s %s %s %q", token, entry.Key, entry.Reason, entry.Path)
		if entry.SizeKnown {
			fmt.Fprintf(output, " (%s logical bytes)", engine.FormatSizeBytes(entry.Bytes))
		}
		if entry.Detail != "" {
			fmt.Fprintf(output, ": %s", entry.Detail)
		}
		if entry.PendingPath != "" {
			fmt.Fprintf(output, "; pending=%q; retry cleanup --build-cache", entry.PendingPath)
		}
		fmt.Fprintln(output)
	}
	return count, bytes, busy
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
