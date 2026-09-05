package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/engine"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/supplychain"
)

func TestGitEvidenceRenderingPreservesPolicyFailureAndBoundsReceipts(t *testing.T) {
	expires := time.Date(2026, 9, 4, 18, 0, 0, 0, time.UTC)
	report := engine.Report{
		Protocol: engine.ReportProtocol, Summary: engine.ReportSummary{Status: "failed", Errors: 1},
		Findings: []policy.Finding{{Check: "supplyChain.gitVulnerability", Path: "uv.lock", Subject: "critical", Message: "known exploited vulnerability"}},
	}
	for range 10 {
		report.GitEvidence = append(report.GitEvidence, supplychain.GitEvidenceReceipt{Scope: "uv.lock", Provider: "trusted-ci", ExpiresAt: expires})
	}
	var output, diagnostics bytes.Buffer
	printReportTo(&output, &diagnostics, report)
	if !strings.Contains(diagnostics.String(), "FAILED errors=1") || !strings.Contains(diagnostics.String(), "known exploited vulnerability") {
		t.Fatalf("verified evidence hid admission failure: %s", diagnostics.String())
	}
	if strings.Count(output.String(), "verified from trusted-ci") != 8 || !strings.Contains(output.String(), "2 more verified artifact(s)") || !strings.Contains(output.String(), "expires 2026-09-04T18:00:00Z") {
		t.Fatalf("evidence rendering is incomplete or unbounded: %s", output.String())
	}
}
