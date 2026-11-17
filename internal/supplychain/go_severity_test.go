package supplychain

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGoAdvisorySeverityUsesExactCurrentCVERecords(t *testing.T) {
	t.Parallel()
	record := func(containers string) string {
		return `{"dataType":"CVE_RECORD","dataVersion":"5.2","cveMetadata":{"cveId":"CVE-2026-12345","state":"PUBLISHED"},"containers":` + containers + `}`
	}
	medium := record(`{"cna":{"metrics":[{"cvssV3_1":{"version":"3.1","baseSeverity":"MEDIUM","baseScore":5.9}}]}}`)
	for _, test := range []struct {
		name, payload, severity string
		failure                 bool
	}{
		{"medium", medium, "moderate", false},
		{"adp", record(`{"cna":{},"adp":[{"metrics":[{"cvssV3_1":{"version":"3.1","baseSeverity":"MEDIUM","baseScore":5.9}},{"other":{"type":"ssvc"}}]}]}`), "moderate", false},
		{"highest", record(`{"cna":{"metrics":[{"cvssV3_0":{"version":"3.0","baseSeverity":"LOW"}}]},"adp":[{"metrics":[{"cvssV4_0":{"version":"4.0","baseSeverity":"CRITICAL"}}]}]}`), "critical", false},
		{"score", record(`{"cna":{"metrics":[{"cvssV2_0":{"version":"2.0","baseScore":7.8}}]}}`), "high", false},
		{"conflicting score", strings.Replace(medium, `"baseScore":5.9`, `"baseScore":9.8`, 1), "critical", false},
		{"missing", record(`{}`), "unknown", false},
		{"missing score", record(`{"cna":{"metrics":[{"cvssV3_1":{"version":"3.1"}}]}}`), "unknown", false},
		{"unsupported severity", strings.Replace(medium, `"MEDIUM"`, `"unrated"`, 1), "unknown", false},
		{"invalid score", strings.Replace(medium, `"baseScore":5.9`, `"baseScore":11`, 1), "unknown", false},
		{"unsupported metric", record(`{"cna":{"metrics":[{"cvssV9_0":{"version":"9.0","baseSeverity":"LOW"}}]}}`), "unknown", false},
		{"mismatched metric", strings.Replace(medium, `"version":"3.1"`, `"version":"3.0"`, 1), "unknown", false},
		{"rejected", strings.Replace(medium, `"PUBLISHED"`, `"REJECTED"`, 1), "unknown", false},
		{"unpublished", strings.Replace(medium, `"PUBLISHED"`, `"RESERVED"`, 1), "unknown", true},
		{"foreign", strings.Replace(medium, "CVE-2026-12345", "CVE-2026-54321", 1), "unknown", true},
		{"unsupported record", strings.Replace(medium, `"dataVersion":"5.2"`, `"dataVersion":"6.0"`, 1), "unknown", true},
		{"malformed", `[`, "unknown", true},
		{"null", `null`, "unknown", true},
		{"unavailable", "", "unknown", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &goSeverityClient{t: t, payload: test.payload}
			advisory := osvVulnerability{ID: "GO-2026-0001", Aliases: []string{"CVE-2026-12345", "CVE-2026-12345", "GHSA-other"}}
			severity, source, err := goAdvisorySeverity(t.Context(), client, advisory)
			if severity != test.severity || (err != nil) != test.failure || client.calls != 1 || source == "" {
				t.Fatalf("severity=%s source=%s err=%v requests=%d", severity, source, err, client.calls)
			}
		})
	}
}

func TestGoAdvisoryMetadataIsNotOverriddenByLookup(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ metadata, expected string }{
		{`"database_specific":{"severity":"HIGH"}`, "high"},
		{`"database_specific":{"severity":"unrated"}`, "unknown"},
		{`"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]`, "unknown"},
	} {
		t.Run(test.expected+test.metadata, func(t *testing.T) {
			var advisory osvVulnerability
			if err := json.Unmarshal([]byte(`{"id":"GO-2026-0001","aliases":["CVE-2026-12345"],`+test.metadata+`}`), &advisory); err != nil {
				t.Fatal(err)
			}
			client := &goSeverityClient{t: t}
			severity, _, err := goAdvisorySeverity(t.Context(), client, advisory)
			if err != nil || client.calls != 0 || severity != test.expected {
				t.Fatalf("severity=%s err=%v requests=%d", severity, err, client.calls)
			}
		})
	}
}

func TestGoSeverityFailureKeepsAdvisoryAndCoverageError(t *testing.T) {
	t.Parallel()
	payload := strings.Replace(cleanGoVulnerabilityReport+goTestAdvisory+goTestCall+goTestCall, `,"database_specific":{"severity":"MODERATE"}`, "", 1)
	report, err := decodeGoVulnerabilityReport([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	client := &goSeverityClient{t: t}
	findings := goVulnerabilityFindings(t.Context(), "go.mod", report, client)
	if len(findings) != 2 || findings[0].Check != "policy.securityScanner" || findings[1].Vulnerability == nil || findings[1].Vulnerability.Severity != "unknown" || client.calls != 1 {
		t.Fatalf("findings=%+v requests=%d", findings, client.calls)
	}
}

type goSeverityClient struct {
	t       *testing.T
	payload string
	calls   int
}

func (client *goSeverityClient) Do(request *http.Request) (*http.Response, error) {
	client.t.Helper()
	client.calls++
	if request.URL.Scheme != "https" || request.URL.Host != "cveawg.mitre.org" || request.URL.Path != "/api/cve/CVE-2026-12345" || request.URL.RawQuery != "" || request.Header.Get("Authorization") != "" {
		client.t.Fatalf("unexpected advisory request %s", request.URL.Redacted())
	}
	if client.payload == "" {
		return nil, errors.New("advisory service unavailable")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(client.payload)), Header: http.Header{}}, nil
}
