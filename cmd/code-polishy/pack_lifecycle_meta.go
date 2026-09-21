package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
)

type packCatalogOptions struct {
	path   string
	sha256 string
	format string
}

type packListDocument struct {
	Protocol string        `json:"protocol"`
	Packs    []pack.Status `json:"packs"`
}

type packCatalogDocument struct {
	Protocol      string       `json:"protocol"`
	CatalogSHA256 string       `json:"catalogSha256"`
	Catalog       pack.Catalog `json:"catalog"`
}

func showPackCatalog(arguments []string) int {
	options, err := parsePackCatalogOptions("pack catalog", arguments, true)
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	catalog, err := pack.LoadCatalog(options.path, options.sha256)
	if err != nil {
		return operationalError(err)
	}
	if options.format == "json" {
		return encodePackJSON(packCatalogDocument{Protocol: "code-polishy-pack-catalog-view/v1", CatalogSHA256: catalog.SHA256, Catalog: catalog.Catalog})
	}
	var output strings.Builder
	fmt.Fprintf(&output, "CATALOG: %s\nSHA256: %s\n", catalog.Catalog.Protocol, catalog.SHA256)
	for _, entry := range catalog.Catalog.Entries {
		fmt.Fprintf(&output, "PACK %s@%s %s\n", entry.Name, entry.Version, entry.Digest)
		fmt.Fprintf(&output, "  ENGINE: %s\n", entry.EngineVersion)
		fmt.Fprintf(&output, "  PLATFORMS: %s\n", strings.Join(entry.Platforms, ", "))
		fmt.Fprintf(&output, "  CAPABILITIES: %s\n", strings.Join(entry.Capabilities, ", "))
		fmt.Fprintf(&output, "  EXECUTION: %s; discovery %s\n", strings.Join(entry.ExecutionTypes, ", "), strings.Join(entry.DiscoveryModes, ", "))
		fmt.Fprintf(&output, "  TOOLS: %s\n", strings.Join(entry.Tools, ", "))
		fmt.Fprintf(&output, "  DEPENDENCIES: %s\n", packValuesOrNone(entry.Dependencies))
		fmt.Fprintf(&output, "  LICENSES: %s\n", strings.Join(entry.Licenses, ", "))
		fmt.Fprintf(&output, "  PROVENANCE: %s %s\n", entry.Provenance.Builder, entry.Provenance.SourceRevision)
	}
	fmt.Print(strings.TrimSuffix(output.String(), "\n") + "\n")
	return 0
}

func installPack(invocation invocation, arguments []string) int {
	values, positional, err := parsePackOptions("pack install", arguments, "--source", "--official", "--catalog", "--sha256")
	if err != nil || len(positional) != 0 {
		return packLifecycleUsage("pack install", err, "requires either --source PATH or --official NAME@VERSION and no positional arguments")
	}
	source, official := values["--source"], values["--official"]
	if (source == "") == (official == "") {
		return commandUsageError("pack", "pack install requires exactly one of --source PATH or --official NAME@VERSION")
	}
	dataRoot, err := pack.UserDataRoot()
	if err != nil {
		return operationalError(err)
	}
	if source != "" {
		if values["--catalog"] != "" || values["--sha256"] != "" {
			return commandUsageError("pack", "pack install --source does not accept --catalog or --sha256")
		}
		engineVersion, err := readPolicyVersion(invocation.policyRoot)
		if err != nil {
			return operationalError(err)
		}
		identity, _, err := pack.Install(source, dataRoot, engineVersion)
		return installedPackResult(identity, err, "installed pack")
	}
	if values["--catalog"] == "" || values["--sha256"] == "" {
		return commandUsageError("pack", "pack install --official requires --catalog PATH and --sha256 DIGEST")
	}
	name, version, err := pack.ParsePackReference(official)
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	catalog, engineVersion, err := loadPackCatalogForInvocation(invocation, values["--catalog"], values["--sha256"])
	if err != nil {
		return operationalError(err)
	}
	identity, _, err := pack.InstallOfficial(catalog, name, version, engineVersion, dataRoot)
	return installedPackResult(identity, err, "installed official pack")
}

func updatePack(invocation invocation, arguments []string) int {
	values, positional, err := parsePackOptions("pack update", arguments, "--to", "--catalog", "--sha256")
	if err != nil || len(positional) != 1 || values["--to"] == "" || values["--catalog"] == "" || values["--sha256"] == "" {
		return packLifecycleUsage("pack update", err, "requires NAME --to VERSION --catalog PATH --sha256 DIGEST")
	}
	name, version, err := pack.ParsePackReference(positional[0] + "@" + values["--to"])
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	catalog, engineVersion, err := loadPackCatalogForInvocation(invocation, values["--catalog"], values["--sha256"])
	if err != nil {
		return operationalError(err)
	}
	entry, err := catalog.Entry(name, version)
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("UPDATE CANDIDATE %s@%s %s\n", entry.Name, entry.Version, entry.Digest)
	fmt.Printf("CAPABILITIES: %s\n", strings.Join(entry.Capabilities, ", "))
	fmt.Printf("EXECUTION: %s; discovery %s\n", strings.Join(entry.ExecutionTypes, ", "), strings.Join(entry.DiscoveryModes, ", "))
	fmt.Printf("TOOLS: %s\nDEPENDENCIES: %s\nLICENSES: %s\n", strings.Join(entry.Tools, ", "), packValuesOrNone(entry.Dependencies), strings.Join(entry.Licenses, ", "))
	fmt.Printf("PROVENANCE: %s %s\n", entry.Provenance.Builder, entry.Provenance.SourceRevision)
	dataRoot, err := pack.UserDataRoot()
	if err != nil {
		return operationalError(err)
	}
	identity, _, err := pack.InstallOfficial(catalog, name, version, engineVersion, dataRoot)
	if status := installedPackResult(identity, err, "installed update candidate"); status != 0 {
		return status
	}
	fmt.Println("REPOSITORY SELECTION: unchanged")
	return 0
}

func removePack(arguments []string) int {
	values, positional, err := parsePackOptions("pack remove", arguments, "--digest")
	if err != nil || len(positional) != 1 {
		return packLifecycleUsage("pack remove", err, "requires NAME@VERSION [--digest DIGEST]")
	}
	name, version, err := pack.ParsePackReference(positional[0])
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	dataRoot, err := pack.UserDataRoot()
	if err != nil {
		return operationalError(err)
	}
	identity, err := pack.Remove(dataRoot, name, version, values["--digest"])
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("PASS removed pack %s %s %s; repository selections are unchanged\n", identity.Name, identity.Version, identity.Digest)
	return 0
}

func listPacks(invocation invocation, arguments []string) int {
	values, positional, err := parsePackOptions("pack list", arguments, "--format")
	if err != nil || len(positional) != 0 {
		return packLifecycleUsage("pack list", err, "accepts only --format human|json")
	}
	format := values["--format"]
	if format == "" {
		format = "human"
	}
	if format != "human" && format != "json" {
		return commandUsageError("pack", "pack list --format must be human or json")
	}
	selected, err := selectedPacks(invocation)
	if err != nil {
		return operationalError(err)
	}
	dataRoot, err := pack.UserDataRoot()
	if err != nil {
		return operationalError(err)
	}
	engineVersion, err := readPolicyVersion(invocation.policyRoot)
	if err != nil {
		return operationalError(err)
	}
	statuses, err := pack.List(dataRoot, selected, engineVersion)
	if err != nil {
		return operationalError(err)
	}
	if format == "json" {
		return encodePackJSON(packListDocument{Protocol: "code-polishy-pack-status/v1", Packs: statuses})
	}
	if len(statuses) == 0 {
		fmt.Println("No installed or selected packs.")
		return 0
	}
	for _, status := range statuses {
		fmt.Printf("%s %s@%s %s", strings.ToUpper(status.State), status.Name, status.Version, status.Digest)
		if status.Selected {
			fmt.Print(" selected")
		}
		if status.Reason != "" {
			fmt.Print(": ", status.Reason)
		}
		fmt.Println()
	}
	return 0
}

func parsePackCatalogOptions(action string, arguments []string, allowFormat bool) (packCatalogOptions, error) {
	allowed := []string{"--catalog", "--sha256"}
	if allowFormat {
		allowed = append(allowed, "--format")
	}
	values, positional, err := parsePackOptions(action, arguments, allowed...)
	if err != nil {
		return packCatalogOptions{}, err
	}
	if len(positional) != 0 || values["--catalog"] == "" || values["--sha256"] == "" {
		return packCatalogOptions{}, fmt.Errorf("%s requires --catalog PATH --sha256 DIGEST and no positional arguments", action)
	}
	format := values["--format"]
	if format == "" {
		format = "human"
	}
	if format != "human" && format != "json" {
		return packCatalogOptions{}, fmt.Errorf("%s --format must be human or json", action)
	}
	return packCatalogOptions{path: values["--catalog"], sha256: values["--sha256"], format: format}, nil
}

func parsePackOptions(action string, arguments []string, allowed ...string) (map[string]string, []string, error) {
	values := map[string]string{}
	positionals := []string{}
	for len(arguments) > 0 {
		name, _, _ := strings.Cut(arguments[0], "=")
		if !strings.HasPrefix(name, "--") {
			positionals = append(positionals, arguments[0])
			arguments = arguments[1:]
			continue
		}
		allowedOption := false
		for _, candidate := range allowed {
			allowedOption = allowedOption || candidate == name
		}
		if !allowedOption {
			return nil, nil, fmt.Errorf("unknown %s option %q", action, arguments[0])
		}
		value, consumed, _, err := namedOptionValue(arguments, name)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := values[name]; exists {
			return nil, nil, errorsDuplicateOption(action, name)
		}
		values[name] = value
		arguments = arguments[consumed:]
	}
	return values, positionals, nil
}

func loadPackCatalogForInvocation(invocation invocation, path, digest string) (pack.LoadedCatalog, string, error) {
	catalog, err := pack.LoadCatalog(path, digest)
	if err != nil {
		return pack.LoadedCatalog{}, "", err
	}
	version, err := readPolicyVersion(invocation.policyRoot)
	if err != nil {
		return pack.LoadedCatalog{}, "", err
	}
	return catalog, version, nil
}

func readPolicyVersion(policyRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(policyRoot, "VERSION"))
	if err != nil {
		return "", fmt.Errorf("read policy version: %w", err)
	}
	version := strings.TrimSpace(string(data))
	if !pack.ValidEngineVersion(version) {
		return "", errors.New("policy VERSION must contain one exact version")
	}
	return version, nil
}

func selectedPacks(invocation invocation) ([]policy.PackSelection, error) {
	config, err := policy.Load(invocation.repoRoot, invocation.configPath)
	if errors.Is(err, os.ErrNotExist) {
		return []policy.PackSelection{}, nil
	}
	if err != nil {
		return nil, err
	}
	return config.Packs, nil
}

func installedPackResult(identity pack.Identity, err error, action string) int {
	if err != nil {
		return operationalError(err)
	}
	fmt.Printf("PASS %s %s %s %s\n", action, identity.Name, identity.Version, identity.Digest)
	return 0
}

func packLifecycleUsage(action string, err error, fallback string) int {
	if err != nil {
		return commandUsageError("pack", err.Error())
	}
	return commandUsageError("pack", action+" "+fallback)
}

func packValuesOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func encodePackJSON(value any) int {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return operationalError(err)
	}
	return 0
}
