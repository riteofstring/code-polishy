package policy

import (
	"strings"
	"testing"
)

func TestReviewSnapshotPreservesHistoricalRequirementsWithoutAdmittingOldCandidates(t *testing.T) {
	data := []byte(`{"version":3,"modules":[{"name":"domain","paths":["src/**"]}],"scope":{"tests":["src/probes/*.py"]},"tests":{"suites":[{"name":"unit","kind":"unit","scope":"module","modules":["domain"],"argv":["go","test","./..."]}]},"verification":{"behaviorReview":{"defaultRequiredAt":"merge","features":[{"name":"domain","modules":["domain"],"suites":["unit"]}]}}}`)
	config, err := ParseReviewSnapshot(data, "base.json")
	if err != nil {
		t.Fatal(err)
	}
	if config.Verification.BehaviorReview.DefaultRequiredAt != BehaviorReviewMerge ||
		config.Verification.BehaviorReview.Features[0].Description != "" ||
		len(config.Tests.Ownership) != 1 || config.Tests.Ownership[0].Module != "domain" ||
		config.Tests.Paths[0] != "src/probes/*.py" || config.Tests.Suites[0].Cwd != "." {
		t.Fatalf("historical review policy lost its requirements: %+v", config)
	}
	if _, err := Parse(data, "candidate.json"); err == nil {
		t.Fatal("historical configuration became an admissible candidate")
	}
	if _, err := ParseReviewSnapshot([]byte(strings.Replace(string(data), `"version":3`, `"version":4`, 1)), "base.json"); err == nil {
		t.Fatal("current configuration without current required fields was accepted")
	}
}

func TestReviewSnapshotRejectsAmbiguousOrInvalidRequirements(t *testing.T) {
	base := `{"version":3,"modules":[{"name":"domain","paths":["src/**"]}],"tests":{"suites":[{"name":"unit","kind":"unit","scope":"module","modules":["domain"],"argv":["go","test","./..."]}]},"verification":{"behaviorReview":{"features":[{"name":"domain","modules":["domain"],"suites":["unit"],"requiredAt":"merge"}]}}}`
	for name, data := range map[string]string{
		"duplicate":           strings.Replace(base, `"version":3`, `"version":3,"version":3`, 1),
		"trailing":            base + `{}`,
		"future":              strings.Replace(base, `"version":3`, `"version":5`, 1),
		"missing suite":       strings.Replace(base, `"suites":["unit"]`, `"suites":["missing"]`, 1),
		"unknown requirement": strings.Replace(base, `"requiredAt":"merge"`, `"requiredAt":"never"`, 1),
		"deep ignored field":  strings.TrimSuffix(base, "}") + `,"ignored":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseReviewSnapshot([]byte(data), "base.json"); err == nil {
				t.Fatal("invalid historical evidence was accepted")
			}
		})
	}
}
