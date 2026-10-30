package supplychain

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestOSVFindingsReportPackageFixesWithoutCommitRanges(t *testing.T) {
	t.Parallel()
	var observed osvPackageResult
	payload := `{
	  "package":{"name":"example","version":"1.0.0","ecosystem":"npm"},
	  "groups":[{"ids":["GHSA-one","CVE-2026-1"],"max_severity":"high"},{"ids":["GHSA-two"],"max_severity":"low"}],
	  "vulnerabilities":[
	    {"id":"GHSA-one","affected":[
	      {"package":{"name":"example"},"ranges":[
	        {"type":"SEMVER","events":[{"introduced":"0"},{"fixed":"1.0.1"}]},
	        {"type":"SEMVER","events":[{"introduced":"2.0.0"},{"fixed":"2.0.5"}]},
	        {"type":"GIT","events":[{"introduced":"0"},{"fixed":"0123456789abcdef0123456789abcdef01234567"}]}
	      ]},
	      {"package":{"name":"other"},"ranges":[{"type":"SEMVER","events":[{"fixed":"9.9.9"}]}]}
	    ]},
	    {"id":"CVE-2026-1","affected":[{"package":{"name":"example"},"ranges":[{"type":"ECOSYSTEM","events":[{"fixed":"1.0.1"}]}]}]},
	    {"id":"GHSA-two","affected":[{"package":{"name":"example"},"ranges":[{"type":"SEMVER","events":[{"introduced":"0"}]}]}]}
	  ]}`
	if err := json.Unmarshal([]byte(payload), &observed); err != nil {
		t.Fatal(err)
	}
	findings := osvPackageFindings("package-lock.json", observed)
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	fixes := map[string][]string{}
	for _, finding := range findings {
		fixes[finding.Vulnerability.Advisory] = finding.Vulnerability.FixedVersions
	}
	if !slices.Equal(fixes["GHSA-one"], []string{"1.0.1", "2.0.5"}) || len(fixes["GHSA-two"]) != 0 {
		t.Fatalf("fixed versions = %+v", fixes)
	}
}
