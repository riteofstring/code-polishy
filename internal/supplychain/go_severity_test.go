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
	for _, test := range []struct {
		name, payload, severity string
		failure                 bool
	}{
		{"medium", `[{"cve_id":"CVE-2026-12345","ghsa_id":"GHSA-example","severity":"medium"}]`, "moderate", false},
		{"highest", `[{"cve_id":"CVE-2026-12345","ghsa_id":"GHSA-first","severity":"medium"},{"cve_id":"CVE-2026-12345","ghsa_id":"GHSA-second","severity":"critical"}]`, "critical", false},
		{"missing", `[]`, "unknown", false},
		{"unsupported", `[{"cve_id":"CVE-2026-12345","ghsa_id":"GHSA-example","severity":"unrated"}]`, "unknown", false},
		{"withdrawn", `[{"cve_id":"CVE-2026-12345","ghsa_id":"GHSA-example","severity":"low","withdrawn_at":"2026-11-01T00:00:00Z"}]`, "unknown", false},
		{"foreign", `[{"cve_id":"CVE-2026-54321","ghsa_id":"GHSA-example","severity":"low"}]`, "unknown", true},
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
	if request.URL.Scheme != "https" || request.URL.Host != "api.github.com" || request.URL.Path != "/advisories" || request.URL.Query().Get("cve_id") != "CVE-2026-12345" || request.URL.Query().Get("per_page") != "100" {
		client.t.Fatalf("unexpected advisory request %s", request.URL.Redacted())
	}
	if client.payload == "" {
		return nil, errors.New("advisory service unavailable")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(client.payload)), Header: http.Header{}}, nil
}
