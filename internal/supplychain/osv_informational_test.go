package supplychain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/runner"
)

const osvUnmaintainedReport = `{"results":[{"source":{"path":"Cargo.lock"},"packages":[{
  "package":{"name":"zvariant","version":"5.15.0","ecosystem":"crates.io"},
  "groups":[{"ids":["RUSTSEC-2026-0370"],"max_severity":""}],
  "vulnerabilities":[{"id":"RUSTSEC-2026-0370","affected":[{
    "package":{"name":"zvariant","ecosystem":"crates.io"},
    "database_specific":{"informational":"unmaintained","cvss":null}
  }]}]
}]}]}`

func TestOSVUnmaintainedNoticeRetainsIdentityAndReportBearingExit(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"grouped":   osvUnmaintainedReport,
		"ungrouped": strings.ReplaceAll(osvUnmaintainedReport, `"groups":[{"ids":["RUSTSEC-2026-0370"],"max_severity":""}],`, ""),
		"alias":     strings.ReplaceAll(osvUnmaintainedReport, `"id":"RUSTSEC-2026-0370",`, `"id":"RUSTSEC-2026-0370","aliases":["GHSA-test-maintenance"],`),
	} {
		t.Run(name, func(t *testing.T) {
			repo := supplyRepository(t)
			findings, err := parseOSVReport(repo, ".", []byte(payload))
			if err != nil || len(findings) != 1 {
				t.Fatalf("maintenance report = %+v, %v", findings, err)
			}
			finding := findings[0]
			if finding.Check != "supplyChain.osvUnmaintained" || finding.Severity != policy.FindingWarning || finding.Vulnerability != nil || finding.Path != "Cargo.lock" {
				t.Fatalf("maintenance notice became a vulnerability or disappeared: %+v", finding)
			}
			if finding.Fields["package"] != "zvariant" || finding.Fields["version"] != "5.15.0" || !strings.Contains(finding.Fields["aliases"], "RUSTSEC-2026-0370") {
				t.Fatalf("maintenance identity was lost: %+v", finding)
			}
			result := runner.Result{ExitStatus: 1, FailureCategory: runner.FailureCommandExit}
			if !acceptedOSVReportExit(t.Context(), repo, osvScan{Root: "."}, result, runner.Output{Stdout: []byte(payload)}, errors.New("scanner findings")) {
				t.Fatal("complete maintenance report was treated as scanner failure")
			}
		})
	}
}

func TestOSVUnmaintainedClassificationPreservesUncertainAndSecurityFindings(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*osvPackageResult){
		"missing metadata":         func(p *osvPackageResult) { p.Vulnerabilities[0].Affected = nil },
		"missing advisory":         func(p *osvPackageResult) { p.Vulnerabilities = nil },
		"unsound":                  func(p *osvPackageResult) { p.Vulnerabilities[0].Affected[0].DatabaseSpecific.Informational = "unsound" },
		"other notice":             func(p *osvPackageResult) { p.Vulnerabilities[0].Affected[0].DatabaseSpecific.Informational = "notice" },
		"wrong affected package":   func(p *osvPackageResult) { p.Vulnerabilities[0].Affected[0].Package.Name = "other" },
		"wrong affected ecosystem": func(p *osvPackageResult) { p.Vulnerabilities[0].Affected[0].Package.Ecosystem = "npm" },
		"wrong observed ecosystem": func(p *osvPackageResult) { p.Package.Ecosystem = "npm" },
		"group severity":           func(p *osvPackageResult) { p.Groups[0].MaxSeverity = "high" },
		"advisory severity": func(p *osvPackageResult) {
			p.Vulnerabilities[0].Severity = []json.RawMessage{json.RawMessage(`{"type":"CVSS_V3","score":"CVSS:3.1/AV:N"}`)}
		},
		"affected severity": func(p *osvPackageResult) {
			p.Vulnerabilities[0].Affected[0].Severity = []json.RawMessage{json.RawMessage(`{"type":"CVSS_V3","score":"CVSS:3.1/AV:N"}`)}
		},
		"RustSec CVSS": func(p *osvPackageResult) {
			p.Vulnerabilities[0].Affected[0].DatabaseSpecific.CVSS = json.RawMessage(`"CVSS:3.1/AV:N"`)
		},
		"unrepresented alias": func(p *osvPackageResult) { p.Groups[0].Aliases = []string{"CVE-2026-1234"} },
		"CVE alias":           func(p *osvPackageResult) { p.Vulnerabilities[0].Aliases = []string{"CVE-2026-1234"} },
		"mixed group": func(p *osvPackageResult) {
			p.Groups[0].IDs = append(p.Groups[0].IDs, "RUSTSEC-2026-0001")
			p.Vulnerabilities = append(p.Vulnerabilities, osvVulnerability{ID: "RUSTSEC-2026-0001"})
		},
		"conflicting affected entries": func(p *osvPackageResult) {
			other := p.Vulnerabilities[0].Affected[0]
			other.DatabaseSpecific.Informational = "unsound"
			p.Vulnerabilities[0].Affected = append(p.Vulnerabilities[0].Affected, other)
		},
		"alias with security record": func(p *osvPackageResult) {
			p.Vulnerabilities = append(p.Vulnerabilities, osvVulnerability{ID: "GHSA-security", Aliases: []string{"RUSTSEC-2026-0370"}})
		},
	} {
		t.Run(name, func(t *testing.T) {
			var report osvReport
			if err := json.Unmarshal([]byte(osvUnmaintainedReport), &report); err != nil {
				t.Fatal(err)
			}
			change(&report.Results[0].Packages[0])
			payload, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			findings, err := parseOSVReport(supplyRepository(t), ".", payload)
			if err != nil || len(findings) != 1 || findings[0].Check != "supplyChain.osvVulnerability" || findings[0].Vulnerability == nil {
				t.Fatalf("security finding was downgraded: %+v, %v", findings, err)
			}
		})
	}
}
