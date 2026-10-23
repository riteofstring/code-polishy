package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestSecurityReportBoundsScansAndDisclosesFreshnessAndMissingCoverage(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	security := &policy.SecurityVerification{Status: "failed", CheckedAt: now, RefreshAfter: now, ReportPath: "refresh/report.json"}
	for range 10 {
		security.Scans = append(security.Scans, policy.SecurityScan{Name: "osv-scan-root", Status: "unavailable", ScannerVersion: "2.4.0", LastSucceededAt: now.Add(-25 * time.Hour), ReportPath: "scan.json"})
	}
	var output, diagnostics bytes.Buffer
	printReportTo(&output, &diagnostics, engine.Report{Protocol: engine.ReportProtocol, Security: security, Summary: engine.ReportSummary{Status: "failed", Errors: 1}})
	for _, expected := range []string{"SECURITY: failed", "refresh due 2026-10-22T12:00:00Z", "scanner 2.4.0", "last succeeded 2026-10-21T11:00:00Z", "2 more scan records", "SECURITY REPORT: refresh/report.json"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("missing %q in %s", expected, output.String())
		}
	}
	if strings.Count(output.String(), "SECURITY SCAN:") != 8 || !strings.Contains(diagnostics.String(), "FAILED errors=1") {
		t.Fatalf("scan rendering hid failure or exceeded bound: %s %s", output.String(), diagnostics.String())
	}
	output.Reset()
	printSecurityEvidence(&output, &policy.SecurityVerification{Status: "not-run"})
	if !strings.Contains(output.String(), "online dependency-security checks were not run") {
		t.Fatalf("offline coverage was not disclosed: %s", output.String())
	}
}
