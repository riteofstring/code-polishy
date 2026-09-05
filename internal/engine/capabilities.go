package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/release"
)

const MaximumCapabilityOutputBytes = 4 << 20

type CapabilitySource struct {
	Kind    string `json:"kind"`
	Name    string `json:"name,omitempty"`
	Path    string `json:"path"`
	Pointer string `json:"pointer,omitempty"`
	Version string `json:"version,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
}

type CapabilityEntry struct {
	ID string `json:"id"`
	release.CapabilityDefinition
	Source       CapabilitySource `json:"source"`
	Availability string           `json:"availability"`
	Reason       string           `json:"reason,omitempty"`
}

type ReleaseCatalogStatus struct {
	Availability string `json:"availability"`
	Path         string `json:"path"`
	SHA256       string `json:"sha256,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type CapabilityInventory struct {
	Protocol       string                   `json:"protocol"`
	LockedRelease  *release.Lock            `json:"lockedRelease"`
	ReleaseCatalog ReleaseCatalogStatus     `json:"releaseCatalog"`
	UpgradeDelta   *release.CapabilityDelta `json:"upgradeDelta"`
	Activation     string                   `json:"activation"`
	Query          string                   `json:"query,omitempty"`
	Total          int                      `json:"total"`
	Matched        int                      `json:"matched"`
	Truncated      bool                     `json:"truncated"`
	Capabilities   []CapabilityEntry        `json:"capabilities"`
}

func (engine *Engine) Capabilities(query string) (CapabilityInventory, error) {
	normalized, err := normalizedCapabilityQuery(query)
	if err != nil {
		return CapabilityInventory{}, err
	}
	inventory := CapabilityInventory{
		Protocol: "capabilities/v1", Activation: "explicit-only", Query: query, Capabilities: []CapabilityEntry{},
		ReleaseCatalog: ReleaseCatalogStatus{Availability: "unavailable", Path: release.CapabilityCatalogPath},
	}
	if err := engine.addReleaseCapabilities(&inventory); err != nil {
		return CapabilityInventory{}, err
	}
	files, err := engine.Repository.AllFiles()
	if err != nil {
		return CapabilityInventory{}, err
	}
	engine.addConfiguredCapabilities(&inventory, files)
	engine.addPackCapabilities(&inventory, files)
	if len(inventory.Capabilities) > 2048 {
		return CapabilityInventory{}, fmt.Errorf("capability inventory exceeds 2048 entries")
	}
	slices.SortFunc(inventory.Capabilities, func(left, right CapabilityEntry) int { return strings.Compare(left.ID, right.ID) })
	inventory.Total = len(inventory.Capabilities)
	inventory.Capabilities, inventory.Matched, err = selectCapabilityCandidates(inventory.Capabilities, normalized)
	if err != nil {
		return CapabilityInventory{}, err
	}
	inventory.Truncated = inventory.Matched > len(inventory.Capabilities)
	if _, err := CapabilityInventoryJSON(inventory); err != nil {
		return CapabilityInventory{}, err
	}
	return inventory, nil
}

func (engine *Engine) addReleaseCapabilities(inventory *CapabilityInventory) error {
	locked, present, err := release.ReadLock(engine.Repository.Root)
	if err != nil {
		return err
	}
	if !present {
		inventory.ReleaseCatalog.Reason = "The repository has no exact release lock."
		return nil
	}
	inventory.LockedRelease = &locked
	delta := release.ReadCapabilityUpgrade(engine.Repository.Root, locked)
	inventory.UpgradeDelta = &delta
	catalog, err := release.ReadCapabilityCatalog(engine.Repository.PolicyRoot, locked)
	if err != nil {
		inventory.ReleaseCatalog.Reason = err.Error()
		return nil
	}
	inventory.ReleaseCatalog.Availability = "available"
	inventory.ReleaseCatalog.SHA256 = catalog.SHA256
	for _, definition := range catalog.Catalog.Capabilities {
		inventory.Capabilities = append(inventory.Capabilities, CapabilityEntry{
			ID: "release:" + definition.Name, CapabilityDefinition: definition, Availability: "available",
			Source: CapabilitySource{Kind: "locked-release", Path: release.CapabilityCatalogPath, Version: locked.CodePolishyVersion, SHA256: catalog.SHA256},
		})
	}
	return nil
}

func CapabilityInventoryJSON(inventory CapabilityInventory) ([]byte, error) {
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(data) > MaximumCapabilityOutputBytes {
		return nil, fmt.Errorf("capability inventory exceeds %d bytes", MaximumCapabilityOutputBytes)
	}
	return append(data, '\n'), nil
}

func normalizedCapabilityQuery(query string) (string, error) {
	if query == "" {
		return "", nil
	}
	normalized, err := policy.NormalizeCapabilityQuery(query)
	if err != nil {
		return "", fmt.Errorf("capability query: %w", err)
	}
	if len(strings.Fields(normalized)) > 16 {
		return "", fmt.Errorf("capability query exceeds 16 terms")
	}
	return normalized, nil
}
