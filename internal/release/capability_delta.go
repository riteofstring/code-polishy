package release

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

const CapabilityUpgradeDirectory = ".code-polishy-reports/capability-upgrades"
const MaximumCapabilityUpgradeBytes = 8 << 20

type CapabilityChange struct {
	Before CapabilityDefinition `json:"before"`
	After  CapabilityDefinition `json:"after"`
}

type CapabilityDelta struct {
	Protocol              string                 `json:"protocol"`
	Availability          string                 `json:"availability"`
	Reason                string                 `json:"reason,omitempty"`
	Outgoing              *Lock                  `json:"outgoing"`
	Incoming              Lock                   `json:"incoming"`
	OutgoingCatalogSHA256 string                 `json:"outgoingCatalogSha256,omitempty"`
	IncomingCatalogSHA256 string                 `json:"incomingCatalogSha256,omitempty"`
	Added                 []CapabilityDefinition `json:"added"`
	Removed               []CapabilityDefinition `json:"removed"`
	Changed               []CapabilityChange     `json:"changed"`
	ArtifactPath          string                 `json:"artifactPath"`
}

type LockUpgradeResult struct {
	Lock    Lock
	Changed bool
	Delta   CapabilityDelta
}

type capabilityUpgradeRecord struct {
	Protocol string                  `json:"protocol"`
	Delta    CapabilityDelta         `json:"delta"`
	Outgoing *capabilityCatalogProof `json:"outgoingEvidence"`
	Incoming *capabilityCatalogProof `json:"incomingEvidence"`
}

func newCapabilityDelta(outgoing *Lock, incoming Lock) CapabilityDelta {
	return CapabilityDelta{
		Protocol: "capability-delta/v1", Availability: "unavailable", Outgoing: outgoing, Incoming: incoming,
		Added: []CapabilityDefinition{}, Removed: []CapabilityDefinition{}, Changed: []CapabilityChange{},
		ArtifactPath: capabilityUpgradePath(incoming),
	}
}

func capabilityUpgradePath(incoming Lock) string {
	return CapabilityUpgradeDirectory + "/" + capabilityUpgradeRelativePath(incoming)
}

func capabilityUpgradeRelativePath(incoming Lock) string {
	return capabilityContentSHA256(RenderLock(incoming)) + "/delta.json"
}

func compareCapabilityCatalogs(before, after AuthenticatedCapabilityCatalog) CapabilityDelta {
	previous := before.Release
	delta := newCapabilityDelta(&previous, after.Release)
	delta.Availability = "available"
	delta.OutgoingCatalogSHA256, delta.IncomingCatalogSHA256 = before.SHA256, after.SHA256
	old, current := canonicalCapabilityDefinitions(before.Catalog), canonicalCapabilityDefinitions(after.Catalog)
	for _, name := range sortedCapabilityNames(old, current) {
		previous, existed := old[name]
		next, exists := current[name]
		switch {
		case !existed:
			delta.Added = append(delta.Added, next)
		case !exists:
			delta.Removed = append(delta.Removed, previous)
		case !sameCapabilityDefinition(previous, next):
			delta.Changed = append(delta.Changed, CapabilityChange{Before: previous, After: next})
		}
	}
	return delta
}

func canonicalCapabilityDefinitions(catalog CapabilityCatalog) map[string]CapabilityDefinition {
	definitions := make(map[string]CapabilityDefinition, len(catalog.Capabilities))
	for _, definition := range catalog.Capabilities {
		definition.Aliases = sortedCapabilityValues(definition.Aliases)
		definition.Paths = sortedCapabilityValues(definition.Paths)
		definition.Modules = sortedCapabilityValues(definition.Modules)
		definition.Enforcement = sortedCapabilityValues(definition.Enforcement)
		definition.Workflows = sortedCapabilityValues(definition.Workflows)
		definitions[definition.Name] = definition
	}
	return definitions
}

func sortedCapabilityValues(values []string) []string {
	ordered := append([]string{}, values...)
	slices.Sort(ordered)
	return ordered
}

func sortedCapabilityNames(before, after map[string]CapabilityDefinition) []string {
	names := make([]string, 0, len(before)+len(after))
	for name := range before {
		names = append(names, name)
	}
	for name := range after {
		names = append(names, name)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func sameCapabilityDefinition(before, after CapabilityDefinition) bool {
	left, _ := json.Marshal(before)
	right, _ := json.Marshal(after)
	return bytes.Equal(left, right)
}

func sameCapabilityLock(left, right Lock) bool {
	return left.LockVersion == right.LockVersion && left.CodePolishyVersion == right.CodePolishyVersion &&
		left.ReleaseDigest == right.ReleaseDigest && slices.Equal(left.Features, right.Features)
}

func capabilityDeltaUnavailable(outgoing *Lock, incoming Lock, reason string) CapabilityDelta {
	delta := newCapabilityDelta(outgoing, incoming)
	delta.Reason = strings.TrimSpace(reason)
	return delta
}
