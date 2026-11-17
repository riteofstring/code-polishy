package supplychain

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
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
		value, origin, err := cveProgramSeverity(ctx, client, alias)
		if err != nil {
			return "unknown", err.Error(), err
		}
		unsupported = unsupported || value == "unknown"
		values, sources = append(values, value), append(sources, origin)
	}
	severity, source = resolveOSVSeverity(values, sources, unsupported)
	return severity, source, nil
}

type cveSeverityContainer struct {
	Metrics []map[string]json.RawMessage `json:"metrics"`
}

func cveProgramSeverity(ctx context.Context, client artifactHTTPClient, cve string) (string, string, error) {
	endpoint := "https://cveawg.mitre.org/api/cve/" + url.PathEscape(cve)
	payload, err := fetchArtifactDocument(ctx, client, endpoint, "application/json")
	if err != nil {
		return "unknown", "", fmt.Errorf("resolve %s severity: %w", cve, err)
	}
	var record struct {
		DataType    string `json:"dataType"`
		DataVersion string `json:"dataVersion"`
		CVEMetadata struct {
			CVEID string `json:"cveId"`
			State string `json:"state"`
		} `json:"cveMetadata"`
		Containers struct {
			CNA cveSeverityContainer   `json:"cna"`
			ADP []cveSeverityContainer `json:"adp"`
		} `json:"containers"`
	}
	if err := json.Unmarshal(payload, &record); err != nil || record.DataType != "CVE_RECORD" || !strings.HasPrefix(record.DataVersion, "5.") || record.CVEMetadata.CVEID != cve {
		return "unknown", "", fmt.Errorf("resolve %s severity: invalid or mismatched CVE Program record", cve)
	}
	if record.CVEMetadata.State == "REJECTED" {
		return "unknown", "CVE Program record is rejected: " + cve, nil
	}
	if record.CVEMetadata.State != "PUBLISHED" {
		return "unknown", "", fmt.Errorf("resolve %s severity: CVE Program record is not published", cve)
	}
	metrics := record.Containers.CNA.Metrics
	for _, container := range record.Containers.ADP {
		metrics = append(metrics, container.Metrics...)
	}
	severity, source := cveMetricsSeverity(cve, metrics)
	return severity, source, nil
}

func cveMetricsSeverity(cve string, metrics []map[string]json.RawMessage) (string, string) {
	values, sources := []string{}, []string{}
	for _, metric := range metrics {
		for _, kind := range slices.Sorted(maps.Keys(metric)) {
			if !strings.HasPrefix(kind, "cvss") {
				continue
			}
			value, source := cveCVSSSeverity(cve, kind, metric[kind])
			values, sources = append(values, value), append(sources, source)
		}
	}
	if len(values) == 0 {
		return "unknown", "CVE Program has no current severity for " + cve
	}
	return resolveOSVSeverity(values, sources, slices.Contains(values, "unknown"))
}

func cveCVSSSeverity(cve, kind string, payload json.RawMessage) (string, string) {
	origin := "CVE Program " + cve + " " + kind
	expected := map[string]string{"cvssV2_0": "2.0", "cvssV3_0": "3.0", "cvssV3_1": "3.1", "cvssV4_0": "4.0"}[kind]
	var metric struct {
		Version      string      `json:"version"`
		BaseSeverity string      `json:"baseSeverity"`
		BaseScore    json.Number `json:"baseScore"`
	}
	if expected == "" || json.Unmarshal(payload, &metric) != nil || metric.Version != expected {
		return "unknown", origin + " contains unsupported or invalid CVSS metadata"
	}
	values, sources := []string{}, []string{}
	for _, field := range []struct{ name, value string }{{"baseSeverity", metric.BaseSeverity}, {"baseScore", metric.BaseScore.String()}} {
		if field.value == "" {
			continue
		}
		values = append(values, policy.NormalizeVulnerabilitySeverity(field.value))
		sources = append(sources, fmt.Sprintf("%s %s=%q", origin, field.name, field.value))
	}
	if len(values) == 0 {
		return "unknown", origin + " has no severity or base score"
	}
	return resolveOSVSeverity(values, sources, slices.Contains(values, "unknown"))
}
