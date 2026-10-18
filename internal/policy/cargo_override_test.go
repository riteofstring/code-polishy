package policy

import (
	"strings"
	"testing"
	"time"
)

func TestCargoOverrideRequiresExactManifestAndSourceTable(t *testing.T) {
	t.Parallel()
	today := time.Now().UTC().Format("2006-01-02")
	expiry := time.Now().UTC().AddDate(0, 0, 7).Format("2006-01-02")
	baseline := `{"id":"cargo-source","ecosystem":"cargo","path":"crate/Cargo.toml","field":"patch.crates-io","contentSha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","reason":"reviewed replacement","owner":"fixture","reviewed":"` + today + `","expires":"` + expiry + `"}`
	for name, test := range map[string]struct {
		declaration string
		valid       bool
	}{
		"governed Cargo replacement": {baseline, true},
		"wrong field":                {strings.Replace(baseline, "patch.crates-io", "overrides", 1), false},
		"wrong ecosystem":            {strings.Replace(baseline, `"cargo"`, `"npm"`, 1), false},
		"wrong manifest":             {strings.Replace(baseline, "crate/Cargo.toml", "package.json", 1), false},
		"other registry":             {strings.Replace(baseline, "patch.crates-io", "patch.private", 1), false},
		"absent digest":              {strings.Replace(baseline, `"contentSha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",`, "", 1), false},
	} {
		t.Run(name, func(t *testing.T) {
			configuration := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{"dependencyOverridePolicies":[`+test.declaration+`]}`, 1)
			_, err := Parse([]byte(configuration), ConfigFilename)
			if (err == nil) != test.valid {
				t.Fatalf("Cargo override validity = %v, want %v: %v", err == nil, test.valid, err)
			}
		})
	}
}
