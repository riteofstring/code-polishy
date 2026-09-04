package policy

import (
	"strings"
	"testing"
)

func TestScopePythonExternalAttributesAcceptsExactWriteContract(t *testing.T) {
	t.Parallel()
	configText := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonExternalAttributes":[{
  "project":"pyproject.toml","module":"plugin.configuration","callable":"configure",
  "receiver":"settings","attribute":"output_path","line":12,"consumerType":"external.runtime.Settings"
}]},"quality":{}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	attributes := config.Scope.PythonExternalAttributes
	if len(attributes) != 1 || attributes[0].Line != 12 || attributes[0].ConsumerType != "external.runtime.Settings" {
		t.Fatalf("external attributes = %+v", attributes)
	}
}

func TestScopePythonExternalAttributesRejectsBroadOrIncompleteContracts(t *testing.T) {
	t.Parallel()
	valid := `"project":"pyproject.toml","module":"plugin","callable":"configure","receiver":"settings","attribute":"output_path","line":12,"consumerType":"external.Settings"`
	cases := map[string]struct {
		declaration string
		want        string
	}{
		"wildcard module":  {strings.Replace(valid, `"module":"plugin"`, `"module":"plugin.*"`, 1), schemaRejection},
		"receiver chain":   {strings.Replace(valid, `"receiver":"settings"`, `"receiver":"self.settings"`, 1), schemaRejection},
		"unqualified type": {strings.Replace(valid, `"consumerType":"external.Settings"`, `"consumerType":"Settings"`, 1), schemaRejection},
		"zero line":        {strings.Replace(valid, `"line":12`, `"line":0`, 1), schemaRejection},
		"unknown field":    {valid + `,"wildcard":true`, schemaRejection},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			configText := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonExternalAttributes":[{`+testCase.declaration+`}]},"quality":{}`, 1)
			_, err := Load(writeConfig(t, configText), "")
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("Load() error = %v, want %q", err, testCase.want)
			}
		})
	}
}
