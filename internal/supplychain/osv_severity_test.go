package supplychain

import (
	"fmt"
	"strings"
	"testing"
)

func TestOSVSeverityRetainsUpstreamMetadataAndDisagreement(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, group, metadata, severity, explanation string
	}{
		{"upstream low without CVSS", `""`, `"database_specific":{"severity":"LOW"}`, "low", "database_specific.severity"},
		{"upstream low with CVSS", `""`, `"database_specific":{"severity":"LOW"},"severity":[{"type":"CVSS_V4","score":"CVSS:4.0/AV:N"}]`, "low", "unnormalized CVSS"},
		{"unknown scanner score", `"UNKNOWN"`, `"database_specific":{"severity":"LOW"}`, "low", "database_specific.severity"},
		{"missing numeric score", `"0.0"`, `"database_specific":{"severity":"LOW"}`, "low", "database_specific.severity"},
		{"scanner CVSS", `"9.8"`, `"database_specific":{}`, "critical", "groups.max_severity"},
		{"metadata raises scanner", `"low"`, `"database_specific":{"severity":"HIGH"}`, "high", "sources disagree"},
		{"scanner raises metadata", `"critical"`, `"database_specific":{"severity":"LOW"}`, "critical", "sources disagree"},
		{"missing severity", `""`, `"database_specific":{}`, "unknown", "unavailable"},
		{"unsupported metadata", `""`, `"database_specific":{"severity":"IMPORTANT"}`, "unknown", "unsupported"},
		{"invalid numeric score", `"NaN"`, `"database_specific":{}`, "unknown", "unsupported"},
		{"unnormalized CVSS", `""`, `"severity":[{"type":"CVSS_V4","score":"CVSS:4.0/AV:N"}]`, "unknown", "unnormalized CVSS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := supplyRepository(t)
			payload := fmt.Sprintf(`{"results":[{"source":{"path":"uv.lock"},"packages":[{"package":{"name":"aiohttp","version":"3.14.3","ecosystem":"PyPI"},"groups":[{"ids":["GHSA-494j-54xm-rwp3"],"max_severity":%s}],"vulnerabilities":[{"id":"GHSA-494j-54xm-rwp3",%s},{"id":"GHSA-unrelated","database_specific":{"severity":"CRITICAL"}}]}]}]}`, test.group, test.metadata)
			findings, err := parseOSVScanReport(repo, osvScan{Root: "."}, []byte(payload))
			if err != nil || len(findings) != 1 {
				t.Fatalf("findings=%+v error=%v", findings, err)
			}
			finding := findings[0]
			if finding.Vulnerability.Severity != test.severity || !strings.Contains(finding.Message, test.explanation) || !strings.Contains(finding.Fields["severitySource"], test.explanation) {
				t.Fatalf("severity provenance lost: %+v", finding)
			}
		})
	}
}

func TestOSVUngroupedAdvisoryUsesItsSeverity(t *testing.T) {
	t.Parallel()
	repo := supplyRepository(t)
	payload := `{"results":[{"source":{"path":"uv.lock"},"packages":[{"package":{"name":"aiohttp","version":"3.14.3","ecosystem":"PyPI"},"vulnerabilities":[{"id":"GHSA-494j-54xm-rwp3","database_specific":{"severity":"LOW"}}]}]}]}`
	findings, err := parseOSVScanReport(repo, osvScan{Root: "."}, []byte(payload))
	if err != nil || len(findings) != 1 || findings[0].Vulnerability.Severity != "low" {
		t.Fatalf("ungrouped upstream severity: %+v %v", findings, err)
	}
}
