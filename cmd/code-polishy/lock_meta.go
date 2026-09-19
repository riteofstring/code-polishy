package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/riteofstring/code-polishy/internal/release"
)

const maximumCapabilityDeltaLines = 8

type lockOptions struct {
	indexURL    string
	indexSHA256 string
}

func handleLockMeta(invocation invocation) int {
	return handleLockMetaWithWriter(invocation, release.WritePublishedReleaseLock)
}

func handleLockMetaWithWriter(invocation invocation, writer func(context.Context, string, string, string, string) (release.LockUpgradeResult, error)) int {
	options, err := parseLockOptions(invocation.arguments)
	if err != nil {
		return commandUsageError("lock", err.Error())
	}
	result, err := writer(context.Background(), invocation.repoRoot, invocation.policyRoot, options.indexURL, options.indexSHA256)
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("PASS %s requires Code Polishy %s %s\n", release.LockFilename, result.Lock.CodePolishyVersion, result.Lock.ReleaseDigest)
	fmt.Println(capabilityDeltaHuman(result.Delta))
	return 0
}

func parseLockOptions(arguments []string) (lockOptions, error) {
	flags := flag.NewFlagSet("lock", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	indexURL := flags.String("index", "", "release publication index URL")
	indexSHA256 := flags.String("sha256", "", "release publication index SHA-256")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return lockOptions{}, errors.New("lock requires --index URL and --sha256 DIGEST")
	}
	if *indexURL == "" || *indexSHA256 == "" {
		return lockOptions{}, errors.New("lock requires --index URL and --sha256 DIGEST")
	}
	return lockOptions{indexURL: *indexURL, indexSHA256: *indexSHA256}, nil
}

func capabilityDeltaHuman(delta release.CapabilityDelta) string {
	var output strings.Builder
	previous := "none"
	if delta.Outgoing != nil {
		previous = delta.Outgoing.CodePolishyVersion
	}
	fmt.Fprintf(&output, "CAPABILITY DELTA: %s (%s -> %s)\n", delta.Availability, previous, delta.Incoming.CodePolishyVersion)
	if delta.Availability != "available" {
		fmt.Fprintln(&output, delta.Reason)
	} else {
		writeCapabilityChanges(&output, delta)
	}
	if delta.ArtifactPath != "" {
		fmt.Fprintf(&output, "UPGRADE RECORD: %s\n", delta.ArtifactPath)
	}
	return strings.TrimSuffix(output.String(), "\n")
}

func writeCapabilityChanges(output *strings.Builder, delta release.CapabilityDelta) {
	fmt.Fprintf(output, "Added: %d; removed: %d; changed: %d.\n", len(delta.Added), len(delta.Removed), len(delta.Changed))
	shown := 0
	for _, group := range []struct {
		label   string
		version string
		entries []release.CapabilityDefinition
	}{{"ADDED", delta.Incoming.CodePolishyVersion, delta.Added}, {"REMOVED", delta.Outgoing.CodePolishyVersion, delta.Removed}} {
		for _, entry := range group.entries {
			if shown == maximumCapabilityDeltaLines {
				break
			}
			fmt.Fprintf(output, "  %s %s @%s: %s\n", group.label, entry.Name, group.version, capabilityDeltaWorkflows(entry.Workflows))
			shown++
		}
	}
	for _, change := range delta.Changed {
		if shown == maximumCapabilityDeltaLines {
			break
		}
		fmt.Fprintf(output, "  CHANGED %s: @%s %s -> @%s %s\n", change.After.Name,
			delta.Outgoing.CodePolishyVersion, capabilityDeltaWorkflows(change.Before.Workflows),
			delta.Incoming.CodePolishyVersion, capabilityDeltaWorkflows(change.After.Workflows))
		shown++
	}
	if remaining := len(delta.Added) + len(delta.Removed) + len(delta.Changed) - shown; remaining > 0 {
		fmt.Fprintf(output, "  %d more changes in the upgrade record and capabilities --format json.\n", remaining)
	}
}

func capabilityDeltaWorkflows(workflows []string) string {
	values := make([]string, 0, 3)
	for _, workflow := range workflows[:min(2, len(workflows))] {
		characters := []rune(workflow)
		if len(characters) > 96 {
			workflow = string(characters[:93]) + "..."
		}
		values = append(values, workflow)
	}
	if len(workflows) > 2 {
		values = append(values, fmt.Sprintf("%d more documents", len(workflows)-2))
	}
	return strings.Join(values, ", ")
}
