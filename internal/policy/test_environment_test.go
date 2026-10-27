package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTestSuiteValidatesCommandAfterEnvironmentAssignments(t *testing.T) {
	for _, test := range []struct {
		name  string
		argv  []string
		valid bool
	}{
		{"cpu unittest", []string{"env", "TEST_DEVICE=cpu", "TEST_DTYPES=float32,float64", "uv", "run", "--frozen", "--no-sync", "python", "-m", "unittest", "discover"}, true},
		{"empty value", []string{"env", "VALUE=", "go", "test", "./..."}, true},
		{"option terminator", []string{"env", "--", "VALUE=cpu", "go", "test", "./..."}, true},
		{"empty", []string{"env"}, false},
		{"assignments only", []string{"env", "VALUE=cpu"}, false},
		{"true", []string{"env", "VALUE=cpu", "true"}, false},
		{"nested no-op", []string{"env", "VALUE=cpu", "env", "OTHER=value", "echo", "passed"}, false},
		{"no tests", []string{"env", "VALUE=cpu", "go", "test", "-run=^$"}, false},
		{"shell", []string{"env", "VALUE=cpu", "sh", "-c", "true"}, false},
		{"split string", []string{"env", "-S", "go test ./..."}, false},
		{"invalid variable", []string{"env", "=cpu", "go", "test"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.argv)
			if err != nil {
				t.Fatal(err)
			}
			config := strings.Replace(minimalConfig(), `"argv":["go","test","./..."]`, `"argv":`+string(encoded), 1)
			_, err = Parse([]byte(config), ConfigFilename)
			if (err == nil) != test.valid {
				t.Fatalf("valid = %t, error = %v", test.valid, err)
			}
		})
	}
}
