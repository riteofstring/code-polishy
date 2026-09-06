package policy

import (
	"strings"
	"testing"
)

func TestPythonContractConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, contract string
		valid          bool
	}{
		{"type", `{"kind":"type","target":"vendor.Model","members":["execute"]}`, true},
		{"fields", `{"kind":"type","target":"vendor.Model","annotatedFields":true}`, true},
		{"decorator", `{"kind":"decorator","target":"vendor.register"}`, true},
		{"nested export", `{"kind":"entry-point","target":"plugins:registry.primary","members":["execute"]}`, true},
		{"bare type", `{"kind":"type","target":"vendor.Model"}`, false},
		{"wildcard", `{"kind":"type","target":"vendor.*","members":["execute"]}`, false},
		{"blank export", `{"kind":"entry-point","target":"plugins:"}`, false},
		{"executable config", `{"kind":"code","target":"run()"}`, false},
		{"decorator attributes", `{"kind":"decorator","target":"vendor.register","attributes":["state"]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			contract := strings.Replace(test.contract, "{", `{"project":"pyproject.toml","reason":"The declared runtime protocol consumes this definition.",`, 1)
			input := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonContracts":[`+contract+`]},"quality":{}`, 1)
			_, err := Load(writeConfig(t, input), "")
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v, error=%v", test.valid, err)
			}
		})
	}
}
