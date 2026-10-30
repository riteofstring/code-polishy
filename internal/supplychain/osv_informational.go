package supplychain

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

type osvAffectedRecord struct {
	Package struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	} `json:"package"`
	Severity []json.RawMessage `json:"severity"`
	Ranges   []struct {
		Type   string `json:"type"`
		Events []struct {
			Fixed string `json:"fixed"`
		} `json:"events"`
	} `json:"ranges"`
	DatabaseSpecific struct {
		Informational string          `json:"informational"`
		CVSS          json.RawMessage `json:"cvss"`
	} `json:"database_specific"`
}

func osvUnmaintainedGroup(group osvGroup, observed osvPackageResult, identifiers []string) bool {
	if group.MaxSeverity != "" || observed.Package.Ecosystem != "crates.io" || len(identifiers) == 0 {
		return false
	}
	if slices.ContainsFunc(identifiers, func(id string) bool { return strings.HasPrefix(id, "CVE-") }) {
		return false
	}
	covered := map[string]bool{}
	for _, advisory := range observed.Vulnerabilities {
		aliases := append([]string{advisory.ID}, advisory.Aliases...)
		if !slices.ContainsFunc(aliases, func(id string) bool { return slices.Contains(identifiers, id) }) {
			continue
		}
		if !osvUnmaintainedAdvisory(advisory, observed.Package.Name) {
			return false
		}
		for _, id := range aliases {
			covered[id] = true
		}
	}
	return !slices.ContainsFunc(identifiers, func(id string) bool { return !covered[id] })
}

func osvUnmaintainedAdvisory(advisory osvVulnerability, name string) bool {
	if !strings.HasPrefix(advisory.ID, "RUSTSEC-") || len(advisory.Severity) != 0 || advisory.DatabaseSpecific.Severity != "" {
		return false
	}
	matched := false
	for _, affected := range advisory.Affected {
		if affected.Package.Ecosystem != "crates.io" || affected.Package.Name != name {
			continue
		}
		cvss := strings.TrimSpace(string(affected.DatabaseSpecific.CVSS))
		if affected.DatabaseSpecific.Informational != "unmaintained" || len(affected.Severity) != 0 || cvss != "" && cvss != "null" {
			return false
		}
		matched = true
	}
	return matched
}

func osvUnmaintainedFinding(scope string, observed osvPackageResult, identifiers []string) policy.Finding {
	advisory := preferredAdvisory(identifiers)
	return policy.Finding{
		Check: "supplyChain.osvUnmaintained", Path: scope, Severity: policy.FindingWarning,
		Subject: advisory + ":" + observed.Package.Name + "@" + observed.Package.Version,
		Fields: map[string]string{
			"ecosystem": "crates.io", "advisory": advisory, "aliases": strings.Join(identifiers, ","),
			"package": observed.Package.Name, "version": observed.Package.Version, "informational": "unmaintained",
		},
		Message:     fmt.Sprintf("RustSec reports %s: %s@%s is unmaintained; this notice does not identify a security vulnerability", advisory, observed.Package.Name, observed.Package.Version),
		Remediation: policy.FindingRemediation{Summary: "Assess the maintenance risk and plan a reviewed migration; this notice does not establish that the dependency is free of vulnerabilities."},
	}
}
