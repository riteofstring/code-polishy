package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func goAdvisorySeverity(ctx context.Context, client artifactHTTPClient, advisory osvVulnerability) (string, string, error) {
	severity, source := osvSeverity(osvGroup{}, []osvVulnerability{advisory}, []string{advisory.ID})
	if severity != "unknown" || advisory.DatabaseSpecific.Severity != "" || len(advisory.Severity) > 0 {
		return severity, source, nil
	}
	values, sources := []string{}, []string{}
	unsupported := false
	for _, alias := range compactNonempty(advisory.Aliases) {
		if !cveIdentifierPattern.MatchString(alias) {
			continue
		}
		value, origin, err := githubCVESeverity(ctx, client, alias)
		if err != nil {
			return "unknown", err.Error(), err
		}
		unsupported = unsupported || value == "unknown"
		values, sources = append(values, value), append(sources, origin)
	}
	severity, source = resolveOSVSeverity(values, sources, unsupported)
	return severity, source, nil
}

func githubCVESeverity(ctx context.Context, client artifactHTTPClient, cve string) (string, string, error) {
	endpoint := "https://api.github.com/advisories?cve_id=" + url.QueryEscape(cve) + "&per_page=100"
	payload, err := fetchArtifactDocument(ctx, client, endpoint, "application/vnd.github+json")
	if err != nil {
		return "unknown", "", fmt.Errorf("resolve %s severity: %w", cve, err)
	}
	var records []struct {
		CVEID       string `json:"cve_id"`
		GHSAID      string `json:"ghsa_id"`
		Severity    string
		WithdrawnAt *string `json:"withdrawn_at"`
	}
	if err := json.Unmarshal(payload, &records); err != nil || records == nil || len(records) >= 100 {
		return "unknown", "", fmt.Errorf("resolve %s severity: invalid or incomplete GitHub advisory response", cve)
	}
	values, sources := []string{}, []string{}
	unsupported := false
	for _, record := range records {
		if record.CVEID != cve || !strings.HasPrefix(record.GHSAID, "GHSA-") {
			return "unknown", "", fmt.Errorf("resolve %s severity: GitHub advisory identity mismatch", cve)
		}
		if record.WithdrawnAt != nil {
			continue
		}
		value := policy.NormalizeVulnerabilitySeverity(record.Severity)
		unsupported = unsupported || value == "unknown"
		values = append(values, value)
		sources = append(sources, fmt.Sprintf("GitHub %s (%s) severity=%q", record.GHSAID, cve, record.Severity))
	}
	if len(values) == 0 {
		return "unknown", "GitHub has no current severity for " + cve, nil
	}
	severity, source := resolveOSVSeverity(values, sources, unsupported)
	return severity, source, nil
}
