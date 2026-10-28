package pythonfacts

import (
	"encoding/json"
	"testing"
)

func TestVersionMembershipUsesPEP440(t *testing.T) {
	cases := []struct {
		specifier string
		version   string
		matches   bool
		invalid   bool
	}{
		{">=2,<3", "2.5", true, false},
		{">=2,<3", "3.0", false, false},
		{">=2,!=2.5", "2.5", false, false},
		{"~=2.4", "2.9", true, false},
		{"==2.4.*", "2.4.7", true, false},
		{">=2,<3", "2.5rc1", true, false},
		{">=2.5", "2.5rc1", false, false},
		{">=2.5rc1,<3", "2.5rc2", true, false},
		{"==2.14.0", "2.14.0+cu130", true, false},
		{">=2.14.0", "2.14.0+cu130", true, false},
		{"==2.14.0+cu130", "2.14.0+cpu", false, false},
		{">=1!2.0", "1!2.1", true, false},
		{"==2.0", "2.0.0", true, false},
		{"", "2.0", true, false},
		{">=banana", "2.0", false, true},
		{">=2", "banana", false, true},
	}
	checks := []VersionCheck{}
	for _, test := range cases {
		checks = append(checks, VersionCheck{test.specifier, test.version})
	}
	results, err := MatchVersions(typeTestInterpreter(t), checks)
	if err != nil {
		t.Fatal(err)
	}
	for index, test := range cases {
		result := results[index]
		if result.Matches != test.matches || (result.Error != "") != test.invalid {
			t.Errorf("%s contains %s: %+v", test.specifier, test.version, result)
		}
	}
}

func TestVersionMembershipRejectsIncompleteOrReorderedEvidence(t *testing.T) {
	checks := []VersionCheck{{">=2", "2.0"}, {"<2", "1.0"}}
	for _, matches := range [][]VersionMatch{
		{{Specifier: ">=2", Version: "2.0", Matches: true}},
		{{Specifier: "<2", Version: "1.0", Matches: true}, {Specifier: ">=2", Version: "2.0", Matches: true}},
	} {
		data, err := json.Marshal(map[string]any{
			"protocol": Protocol, "pythonVersion": "3.12.13", "packagingVersion": PackagingVersion, "matches": matches,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeVersionMatches(data, checks); err == nil {
			t.Fatal("incomplete or reordered membership evidence was accepted")
		}
	}
}
