package supplychain

import (
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func osvSeverity(group osvGroup, advisories []osvVulnerability, identifiers []string) (string, string) {
	values := []string{}
	sources := []string{}
	unsupported := false
	add := func(value, source string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		severity := policy.NormalizeVulnerabilitySeverity(value)
		unsupported = unsupported || severity == "unknown"
		values = append(values, severity)
		sources = append(sources, fmt.Sprintf("%s=%q", source, value))
	}
	groupValue := strings.TrimSpace(group.MaxSeverity)
	if strings.EqualFold(groupValue, "unknown") || groupValue == "0" || groupValue == "0.0" {
		groupValue = ""
	}
	add(groupValue, "OSV groups.max_severity")
	for _, advisory := range advisories {
		if !slices.ContainsFunc(append([]string{advisory.ID}, advisory.Aliases...), func(id string) bool { return slices.Contains(identifiers, id) }) {
			continue
		}
		add(advisory.DatabaseSpecific.Severity, advisory.ID+" database_specific.severity")
		if groupValue == "" && len(advisory.Severity) > 0 {
			unsupported = unsupported || strings.TrimSpace(advisory.DatabaseSpecific.Severity) == ""
			sources = append(sources, advisory.ID+" severity contains unnormalized CVSS metadata")
		}
	}
	return resolveOSVSeverity(values, sources, unsupported)
}

func resolveOSVSeverity(values, sources []string, unsupported bool) (string, string) {
	if len(sources) == 0 {
		return "unknown", "OSV group score and advisory severity metadata are unavailable"
	}
	source := strings.Join(sources, "; ")
	if unsupported {
		return "unknown", source + "; unsupported severity metadata requires investigation"
	}
	rank := []string{"low", "moderate", "high", "critical"}
	severity := "low"
	for _, value := range values {
		if slices.Index(rank, value) > slices.Index(rank, severity) {
			severity = value
		}
	}
	if slices.ContainsFunc(values, func(value string) bool { return value != severity }) {
		source += "; sources disagree, retaining the highest severity"
	}
	return severity, source
}
