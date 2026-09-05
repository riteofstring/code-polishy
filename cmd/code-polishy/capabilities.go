package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/riteofstring/code-polishy/internal/engine"
)

type capabilityOptions struct {
	query  string
	format string
}

func handleCapabilities(_ context.Context, policyEngine *engine.Engine, arguments []string) (commandResult, error) {
	options, err := parseCapabilityOptions(arguments)
	if err != nil {
		return commandResult{}, commandInputError(err)
	}
	inventory, err := policyEngine.Capabilities(options.query)
	if err != nil {
		return commandResult{}, err
	}
	if options.format == "json" {
		data, err := engine.CapabilityInventoryJSON(inventory)
		if err != nil {
			return commandResult{}, err
		}
		return commandResult{quiet: true, messages: []string{strings.TrimSuffix(string(data), "\n")}}, nil
	}
	return commandResult{quiet: true, messages: []string{capabilitiesHuman(inventory)}}, nil
}

func parseCapabilityOptions(arguments []string) (capabilityOptions, error) {
	options := capabilityOptions{format: "human"}
	seen := map[string]bool{}
	for len(arguments) > 0 {
		name, _, _ := strings.Cut(arguments[0], "=")
		if name != "--query" && name != "--format" {
			return capabilityOptions{}, fmt.Errorf("unknown capabilities option %q", arguments[0])
		}
		value, consumed, _, err := namedOptionValue(arguments, name)
		if err != nil {
			return capabilityOptions{}, err
		}
		if seen[name] {
			return capabilityOptions{}, errorsDuplicateOption("capabilities", name)
		}
		seen[name] = true
		if name == "--query" {
			options.query = value
		} else if value == "human" || value == "json" {
			options.format = value
		} else {
			return capabilityOptions{}, fmt.Errorf("capabilities --format must be human or json")
		}
		arguments = arguments[consumed:]
	}
	return options, nil
}

func capabilitiesHuman(inventory engine.CapabilityInventory) string {
	var output strings.Builder
	if inventory.LockedRelease != nil {
		fmt.Fprintf(&output, "LOCKED RELEASE: %s %s\n", inventory.LockedRelease.CodePolishyVersion, inventory.LockedRelease.ReleaseDigest)
	} else {
		fmt.Fprintln(&output, "LOCKED RELEASE: unavailable")
	}
	fmt.Fprintf(&output, "RELEASE CATALOG: %s\n", inventory.ReleaseCatalog.Availability)
	if inventory.ReleaseCatalog.Reason != "" {
		fmt.Fprintln(&output, inventory.ReleaseCatalog.Reason)
	}
	if inventory.UpgradeDelta != nil {
		fmt.Fprintln(&output, capabilityDeltaHuman(*inventory.UpgradeDelta))
	}
	fmt.Fprintln(&output, "Discovery selects no behavior features and executes no checks.")
	for _, entry := range inventory.Capabilities {
		fmt.Fprintf(&output, "\n%s %s (%s): %s\n", strings.ToUpper(entry.Kind), entry.Name, entry.Availability, entry.Description)
		fmt.Fprintf(&output, "  SOURCE: %s %s%s", entry.Source.Kind, entry.Source.Path, entry.Source.Pointer)
		if entry.Source.Name != "" {
			fmt.Fprintf(&output, " %s", entry.Source.Name)
		}
		if entry.Source.Version != "" {
			fmt.Fprintf(&output, " @%s", entry.Source.Version)
		}
		fmt.Fprintln(&output)
		for _, field := range []struct {
			label  string
			values []string
		}{{"ALIASES", entry.Aliases}, {"PATHS", entry.Paths}, {"MODULES", entry.Modules}, {"ENFORCEMENT", entry.Enforcement}, {"WORKFLOWS", entry.Workflows}} {
			if len(field.values) > 0 {
				fmt.Fprintf(&output, "  %s: %s\n", field.label, strings.Join(field.values, ", "))
			}
		}
		if entry.Reason != "" {
			fmt.Fprintln(&output, "  REASON:", entry.Reason)
		}
	}
	if inventory.Query != "" {
		fmt.Fprintf(&output, "\nCANDIDATES: %d of %d matches shown; activation requires an explicit configured name or alias.\n", len(inventory.Capabilities), inventory.Matched)
	}
	return strings.TrimSuffix(output.String(), "\n")
}
