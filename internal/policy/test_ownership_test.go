package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTestOwnershipSchemaRequiresOneExactPrimarySuite(t *testing.T) {
	t.Parallel()
	baseline := func(t *testing.T) map[string]any {
		t.Helper()
		var config map[string]any
		if err := json.Unmarshal([]byte(minimalConfig()), &config); err != nil {
			t.Fatal(err)
		}
		tests := config["tests"].(map[string]any)
		tests["ownership"] = []any{map[string]any{"paths": []any{"spec/check.py"}, "module": "content", "focusedSuite": "content-test"}}
		suite := tests["suites"].([]any)[0].(map[string]any)
		suite["paths"] = []any{"spec/**"}
		return config
	}
	cases := []struct {
		name   string
		change func(map[string]any)
		want   string
	}{
		{"valid", func(map[string]any) {}, ""},
		{"missing ownership", func(tests map[string]any) { delete(tests, "ownership") }, "ownership"},
		{"unknown module", func(tests map[string]any) { tests["ownership"].([]any)[0].(map[string]any)["module"] = "missing" }, "unknown production module"},
		{"unknown suite", func(tests map[string]any) { tests["ownership"].([]any)[0].(map[string]any)["focusedSuite"] = "missing" }, "unknown test suite"},
		{"repository suite", func(tests map[string]any) { tests["ownership"].([]any)[0].(map[string]any)["focusedSuite"] = "full" }, "quick module-scoped"},
		{"no execution paths", func(tests map[string]any) { delete(tests["suites"].([]any)[0].(map[string]any), "paths") }, "explicit execution paths"},
		{"missing profile", func(tests map[string]any) {
			tests["suites"].([]any)[0].(map[string]any)["runOn"] = []any{"focused", "full"}
		}, "recommended"},
		{"overlap", func(tests map[string]any) {
			tests["ownership"] = append(tests["ownership"].([]any), map[string]any{"paths": []any{"spec/*.py"}, "module": "content", "focusedSuite": "content-test"})
		}, "overlaps"},
		{"escaping", func(tests map[string]any) {
			tests["ownership"].([]any)[0].(map[string]any)["paths"] = []any{"../spec/*.py"}
		}, "tests/ownership/0/paths"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			config := baseline(t)
			testCase.change(config["tests"].(map[string]any))
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := Parse(data, ConfigFilename)
			if testCase.want == "" {
				if err != nil || len(loaded.Tests.Ownership) != 1 || loaded.Tests.Ownership[0].Module != "content" {
					t.Fatalf("loaded ownership = %+v, error = %v", loaded.Tests.Ownership, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected %q, got %v", testCase.want, err)
			}
		})
	}
}

func TestTestOwnershipCutoverRejectsSupersededForms(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		strings.Replace(minimalConfig(), `"version":4`, `"version":3`, 1),
		strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"tests":["spec/**"]},"quality":{}`, 1),
	} {
		if _, err := Parse([]byte(data), ConfigFilename); err == nil {
			t.Fatal("superseded test ownership configuration was accepted")
		}
	}
}
