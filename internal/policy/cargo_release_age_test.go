package policy

import (
	"strings"
	"testing"
	"time"
)

func TestCargoReleaseAgeAssessmentUsesExactRegistryVersions(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	supply := `"supplyChain":{"releaseAgeAssessments":[{
    "id":"cargo-fix","ecosystem":"crates.io","package":"example","version":"1.2.3","scope":"Cargo.lock",
    "category":"security-fix","evidence":"https://example.test/advisory","reason":"reachable security fix","owner":"security",
    "reviewed":"` + now.Format("2006-01-02") + `","expires":"` + now.AddDate(0, 0, 5).Format("2006-01-02") + `"
  }]}`
	configured := strings.Replace(minimalConfig(), `"supplyChain":{}`, supply, 1)
	for _, version := range []string{"1.2.3", "1.2.3+native", "1.2.3-beta.1"} {
		candidate := strings.Replace(configured, `"version":"1.2.3"`, `"version":"`+version+`"`, 1)
		if _, err := Load(writeConfig(t, candidate), ""); err != nil {
			t.Fatalf("exact Cargo version %s rejected: %v", version, err)
		}
	}
	for _, version := range []string{"=1.2.3", "^1.2.3", "1.2", "1.2.3-01"} {
		candidate := strings.Replace(configured, `"version":"1.2.3"`, `"version":"`+version+`"`, 1)
		if _, err := Load(writeConfig(t, candidate), ""); err == nil {
			t.Fatalf("inexact Cargo assessment version %s accepted", version)
		}
	}
}
