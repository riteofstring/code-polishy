package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
)

func (engine *Engine) addConfiguredCapabilities(inventory *CapabilityInventory, files []string) {
	engine.addDeclaredCapabilities(inventory, files)
	engine.addCheckCapabilities(inventory, files)
	engine.addBehaviorCapabilities(inventory)
}

func (engine *Engine) addDeclaredCapabilities(inventory *CapabilityInventory, files []string) {
	config := engine.Repository.Config
	for index, name := range config.Project.Capabilities {
		entry := configuredCapability("project", name, "Declared project capability: "+name+".", fmt.Sprintf("/project/capabilities/%d", index))
		entry.Enforcement = []string{"declaration"}
		inventory.Capabilities = append(inventory.Capabilities, entry)
	}
	for index, module := range config.Modules {
		for capabilityIndex, name := range module.Capabilities {
			entry := configuredCapability("module", name, "Declared capability of module "+module.Name+": "+name+".", fmt.Sprintf("/modules/%d/capabilities/%d", index, capabilityIndex))
			entry.ID = "module:" + module.Name + ":" + name
			entry.Paths, entry.Modules, entry.Enforcement = reportArray(module.Paths), []string{module.Name}, []string{"declaration"}
			engine.markCapabilityApplicability(&entry, files)
			inventory.Capabilities = append(inventory.Capabilities, entry)
		}
	}
}

func (engine *Engine) addCheckCapabilities(inventory *CapabilityInventory, files []string) {
	config := engine.Repository.Config
	for index, command := range config.Checks {
		if command.Adapter != nil {
			continue
		}
		description := "Configured check " + command.Name + "."
		if len(command.Provides) > 0 {
			description = "Configured check providing " + strings.Join(command.Provides, ", ") + "."
		}
		entry := configuredCapability("check", command.Name, description, fmt.Sprintf("/checks/%d", index))
		entry.Paths, entry.Modules, entry.Enforcement = reportArray(command.Paths), reportArray(command.Modules), reportArray(command.RunOn)
		if command.Managed {
			entry.Source = CapabilitySource{Kind: "locked-policy-module", Path: release.CapabilityCatalogPath, SHA256: inventory.ReleaseCatalog.SHA256}
			if inventory.LockedRelease != nil {
				entry.Source.Version = inventory.LockedRelease.CodePolishyVersion
			}
			if inventory.ReleaseCatalog.Availability != "available" {
				entry.Availability, entry.Reason = "unavailable", "The exact release capability catalog is unavailable."
			}
		}
		engine.markCapabilityApplicability(&entry, files)
		inventory.Capabilities = append(inventory.Capabilities, entry)
	}
}

func (engine *Engine) addBehaviorCapabilities(inventory *CapabilityInventory) {
	config := engine.Repository.Config
	if review := config.Verification.BehaviorReview; review != nil {
		for index, feature := range review.Features {
			entry := configuredCapability("behavior-feature", feature.Name, feature.Description, fmt.Sprintf("/verification/behaviorReview/features/%d", index))
			entry.Aliases, entry.Paths, entry.Modules = reportArray(feature.Aliases), reportArray(feature.Paths), reportArray(feature.Modules)
			entry.Enforcement, entry.Workflows = []string{review.EffectiveRequiredAt(feature)}, []string{"docs/policies/behavior-review.md"}
			inventory.Capabilities = append(inventory.Capabilities, entry)
		}
	}
}

func configuredCapability(kind, name, description, pointer string) CapabilityEntry {
	return CapabilityEntry{
		ID: kind + ":" + name, Availability: "available",
		CapabilityDefinition: release.CapabilityDefinition{
			Name: name, Kind: kind, Description: description, Aliases: []string{}, Paths: []string{}, Modules: []string{},
			Enforcement: []string{}, Workflows: []string{"docs/agent-workflows.md"},
		},
		Source: CapabilitySource{Kind: "configuration", Path: policy.ConfigFilename, Pointer: pointer},
	}
}

func (engine *Engine) markCapabilityApplicability(entry *CapabilityEntry, files []string) {
	if entry.Availability != "available" || len(entry.Paths) == 0 && len(entry.Modules) == 0 {
		return
	}
	for _, file := range files {
		if len(entry.Paths) > 0 {
			if policy.MatchesAny(file, entry.Paths) {
				return
			}
			continue
		}
		for _, owner := range engine.Repository.OwnerModuleNames(file) {
			if slices.Contains(entry.Modules, owner) {
				return
			}
		}
	}
	entry.Availability, entry.Reason = "inapplicable", "No current governed file matches the declared scope."
}

func (engine *Engine) addPackCapabilities(inventory *CapabilityInventory, files []string) {
	resolution := pack.Resolve(engine.Repository.Config.Packs, engine.PackDataRoot)
	for _, selected := range engine.Repository.Config.Packs {
		entry := configuredCapability("language-pack", selected.Name, "Selected language pack "+selected.Name+".", "")
		entry.Source = CapabilitySource{Kind: "installed-pack", Name: selected.Name, Path: pack.ManifestFilename, Version: selected.Version, SHA256: selected.Digest}
		entry.Enforcement = []string{"configured-profiles"}
		for _, finding := range resolution.Findings {
			if finding.Subject == selected.Name {
				entry.Availability, entry.Reason = "unavailable", finding.Message
			}
		}
		inventory.Capabilities = append(inventory.Capabilities, entry)
	}
	for _, command := range resolution.Commands {
		adapter := command.Adapter
		entry := configuredCapability("pack-capability", adapter.Capability, "Language pack "+adapter.PackName+" provides "+adapter.Capability+" through "+command.Name+".", "")
		entry.ID = "pack-capability:" + command.Name
		entry.Source = CapabilitySource{Kind: "installed-pack", Name: adapter.PackName, Path: pack.ManifestFilename, Version: adapter.PackVersion, SHA256: adapter.PackDigest}
		entry.Paths, entry.Enforcement = reportArray(command.Paths), reportArray(command.RunOn)
		engine.markCapabilityApplicability(&entry, files)
		inventory.Capabilities = append(inventory.Capabilities, entry)
	}
}
