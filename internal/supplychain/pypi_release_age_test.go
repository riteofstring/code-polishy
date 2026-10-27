package supplychain

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPyPIReleaseAgeMatchesPEP440Versions(t *testing.T) {
	for _, test := range []struct{ published, locked string }{
		{"13.0.3", "13.0.3.0"},
		{"1.0.0", "1"},
		{"1!2.0rc1", "01!2RC1"},
		{"1.0+LOCAL.1", "1+local.1"},
	} {
		t.Run(test.locked, func(t *testing.T) {
			payload := `{"releases":{"` + test.published + `":[{"upload_time_iso_8601":"2026-04-14T00:50:08Z"}]}}`
			times, err := decodePyPIReleaseTimes(strings.NewReader(payload), []resolvedPackage{{Version: test.locked}})
			if err != nil || times[test.locked].Format(time.RFC3339) != "2026-04-14T00:50:08Z" {
				t.Fatalf("release time = %v, error = %v", times, err)
			}
		})
	}
}

func TestPyPIReleaseAgePreservesLargeReleaseInventories(t *testing.T) {
	var payload strings.Builder
	payload.WriteString(`{"releases":{`)
	for index := 0; index < 4100; index++ {
		if index > 0 {
			payload.WriteByte(',')
		}
		fmt.Fprintf(&payload, `"1.0.%d":[{"upload_time_iso_8601":"2026-04-14T00:50:08Z"}]`, index)
	}
	payload.WriteString(`,"legacy":[],"0.1":[],"0.1.0":[]}}`)
	times, err := decodePyPIReleaseTimes(strings.NewReader(payload.String()), []resolvedPackage{{Version: "1.0.4099.0"}})
	if err != nil || times["1.0.4099.0"].Format(time.RFC3339) != "2026-04-14T00:50:08Z" {
		t.Fatalf("large release inventory = %v, %v", times, err)
	}
}

func TestPyPIReleaseAgePreservesDatedReleaseWithEmptyAlias(t *testing.T) {
	payload := `{"releases":{"13.0.3":[{"upload_time_iso_8601":"2026-04-14T00:50:08Z"}],"13.0.3.0":[]}}`
	times, err := decodePyPIReleaseTimes(strings.NewReader(payload), []resolvedPackage{{Version: "13.0.3"}})
	if err != nil || times["13.0.3"].Format(time.RFC3339) != "2026-04-14T00:50:08Z" {
		t.Fatalf("dated release lost to empty alias: %v, %v", times, err)
	}
}

func TestPyPIReleaseAgeKeepsMissingAndAmbiguousEvidence(t *testing.T) {
	for _, test := range []struct {
		name, releases, version string
		ambiguous               bool
	}{
		{"absent", `"1.0":[{"upload_time_iso_8601":"2026-04-14T00:50:08Z"}]`, "1.1", false},
		{"no files", `"1.0":[]`, "1.0.0", false},
		{"no timestamp", `"1.0":[{}]`, "1.0.0", false},
		{"distinct prerelease", `"1.0rc1":[{"upload_time_iso_8601":"2026-04-14T00:50:08Z"}]`, "1.0", false},
		{"distinct local", `"1.0+cpu":[{"upload_time_iso_8601":"2026-04-14T00:50:08Z"}]`, "1.0+cuda", false},
		{"duplicate identity", `"1.0":[{"upload_time_iso_8601":"2026-04-14T00:50:08Z"}],"1.0.0":[{"upload_time_iso_8601":"2026-04-15T00:50:08Z"}]`, "1.0", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			times, err := decodePyPIReleaseTimes(strings.NewReader(`{"releases":{`+test.releases+`}}`), []resolvedPackage{{Version: test.version}})
			if test.ambiguous {
				if err == nil || !strings.Contains(err.Error(), "ambiguous") {
					t.Fatalf("ambiguous release accepted: %v, %v", times, err)
				}
			} else if err != nil || len(times) != 0 {
				t.Fatalf("missing release evidence changed: %v, %v", times, err)
			}
		})
	}
}
