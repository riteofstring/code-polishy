package release

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/riteofstring/code-polishy/internal/policy"
)

const CapabilityCatalogPath = "docs/capabilities.json"

const MaximumCapabilityCatalogBytes = 1 << 20

type CapabilityDefinition struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	Aliases     []string `json:"aliases"`
	Paths       []string `json:"paths"`
	Modules     []string `json:"modules"`
	Enforcement []string `json:"enforcement"`
	Workflows   []string `json:"workflows"`
}

type CapabilityCatalog struct {
	Protocol     string                 `json:"protocol"`
	Capabilities []CapabilityDefinition `json:"capabilities"`
}

type AuthenticatedCapabilityCatalog struct {
	Release Lock              `json:"release"`
	SHA256  string            `json:"sha256"`
	Catalog CapabilityCatalog `json:"catalog"`
	proof   *capabilityCatalogProof
}

func ParseCapabilityCatalog(data []byte) (CapabilityCatalog, error) {
	if len(data) == 0 || len(data) > MaximumCapabilityCatalogBytes || !utf8.Valid(data) {
		return CapabilityCatalog{}, fmt.Errorf("capability catalog must be UTF-8 JSON of at most %d bytes", MaximumCapabilityCatalogBytes)
	}
	var catalog CapabilityCatalog
	if err := decodeExactly(data, CapabilityCatalogPath, &catalog); err != nil {
		return CapabilityCatalog{}, err
	}
	if catalog.Protocol != "release-capabilities/v1" || len(catalog.Capabilities) == 0 || len(catalog.Capabilities) > 256 {
		return CapabilityCatalog{}, fmt.Errorf("capability catalog requires release-capabilities/v1 and between 1 and 256 entries")
	}
	identities := map[string]string{}
	for _, capability := range catalog.Capabilities {
		if err := validateCapabilityDefinition(capability, identities); err != nil {
			return CapabilityCatalog{}, err
		}
	}
	slices.SortFunc(catalog.Capabilities, func(left, right CapabilityDefinition) int { return strings.Compare(left.Name, right.Name) })
	return catalog, nil
}

func validateCapabilityDefinition(capability CapabilityDefinition, identities map[string]string) error {
	if len(capability.Name) > 128 || !featurePattern.MatchString(capability.Name) || capability.Kind != "command" {
		return fmt.Errorf("capability %q requires a canonical command name of at most 128 bytes", capability.Name)
	}
	if !capabilityDescriptionValid(capability.Description) {
		return fmt.Errorf("capability %q requires a concise description of at most 512 UTF-8 bytes", capability.Name)
	}
	if err := validateCapabilityAliases(capability, identities); err != nil {
		return err
	}
	return validateCapabilityScope(capability)
}

func validateCapabilityAliases(capability CapabilityDefinition, identities map[string]string) error {
	if len(capability.Aliases) > 16 || capability.Aliases == nil {
		return fmt.Errorf("capability %q requires at most 16 declared aliases", capability.Name)
	}
	for _, value := range append([]string{capability.Name}, capability.Aliases...) {
		identity, err := policy.NormalizeFeatureAlias(value)
		if err != nil {
			return fmt.Errorf("capability %q alias: %w", capability.Name, err)
		}
		if previous, exists := identities[identity]; exists {
			return fmt.Errorf("capability %q name or alias collides with %q", capability.Name, previous)
		}
		identities[identity] = capability.Name
	}
	return nil
}

func validateCapabilityScope(capability CapabilityDefinition) error {
	if capability.Paths == nil || capability.Modules == nil || len(capability.Enforcement) == 0 || len(capability.Workflows) == 0 {
		return fmt.Errorf("capability %q requires scope, enforcement, and workflow metadata", capability.Name)
	}
	for label, values := range map[string][]string{"paths": capability.Paths, "modules": capability.Modules, "enforcement": capability.Enforcement, "workflows": capability.Workflows} {
		if err := validateCapabilityValues(label, values); err != nil {
			return fmt.Errorf("capability %q: %w", capability.Name, err)
		}
	}
	return validateCapabilityWorkflowPaths(capability)
}

func validateCapabilityWorkflowPaths(capability CapabilityDefinition) error {
	for _, workflow := range capability.Workflows {
		if !strings.HasPrefix(workflow, "docs/") || !strings.HasSuffix(workflow, ".md") || !capabilityRelativePath(workflow) {
			return fmt.Errorf("capability %q workflow %q must be a contained Markdown document under docs", capability.Name, workflow)
		}
	}
	return nil
}

func capabilityDescriptionValid(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value) && strings.TrimSpace(value) == value &&
		!strings.ContainsFunc(value, func(character rune) bool {
			return unicode.IsControl(character) || character == '\u2028' || character == '\u2029'
		})
}

func validateCapabilityValues(label string, values []string) error {
	if len(values) > 128 {
		return fmt.Errorf("%s exceeds 128 entries", label)
	}
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || len(value) > 4096 || strings.TrimSpace(value) != value || strings.ContainsFunc(value, unicode.IsControl) || seen[value] {
			return fmt.Errorf("%s contains an invalid or repeated value", label)
		}
		seen[value] = true
	}
	return nil
}

func capabilityContentSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
