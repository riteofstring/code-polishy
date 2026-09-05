package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScopePythonExternalAttributesAcceptsExactReceiverBindings(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"parameter", "local", "self"} {
		t.Run(kind, func(t *testing.T) {
			attribute := exactExternalAttribute()
			attribute.Receiver.Kind = kind
			if kind == "self" {
				attribute.Receiver.Type = ""
				attribute.Receiver.Consumer = &PythonExternalConsumer{
					Kind: "base", Qualified: "external.runtime.Settings", Site: PythonSourceLocation{Line: 2, Column: 15},
				}
			}
			config, err := Load(writeConfig(t, externalAttributeConfig(t, []PythonExternalAttribute{attribute})), "")
			if err != nil {
				t.Fatal(err)
			}
			attributes := config.Scope.PythonExternalAttributes
			if len(attributes) != 1 || attributes[0].Receiver.Kind != kind || attributes[0].Write != attribute.Write {
				t.Fatalf("external attributes = %+v", attributes)
			}
		})
	}
}

func TestScopePythonExternalAttributesRejectsBroadIncompleteAndFlatContracts(t *testing.T) {
	t.Parallel()
	valid := externalAttributeConfig(t, []PythonExternalAttribute{exactExternalAttribute()})
	cases := map[string]string{
		"wildcard module":       strings.Replace(valid, `"module":"plugin"`, `"module":"plugin.*"`, 1),
		"receiver chain":        strings.Replace(valid, `"name":"settings"`, `"name":"self.settings"`, 1),
		"unqualified type":      strings.Replace(valid, `external.runtime.Settings`, `Settings`, 1),
		"zero write column":     strings.Replace(valid, `"write":{"line":12,"column":5}`, `"write":{"line":12,"column":0}`, 1),
		"missing binding":       strings.Replace(valid, `"binding":{"line":3,"column":15},`, ``, 1),
		"missing kind":          strings.Replace(valid, `"kind":"parameter",`, ``, 1),
		"self without consumer": strings.Replace(valid, `"kind":"parameter"`, `"kind":"self"`, 1),
		"flat receiver":         strings.Replace(valid, `"receiver":{"kind":"parameter","name":"settings","binding":{"line":3,"column":15},"type":"external.runtime.Settings"}`, `"receiver":"settings","line":12,"consumerType":"external.runtime.Settings"`, 1),
	}
	for name, configText := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, configText), "")
			if err == nil || !strings.Contains(err.Error(), schemaRejection) {
				t.Fatalf("Load() error = %v, want schema rejection", err)
			}
		})
	}
}

func TestScopePythonExternalAttributesRejectsCompetingContractsForOneWrite(t *testing.T) {
	t.Parallel()
	first := exactExternalAttribute()
	second := first
	second.Receiver.Type = "another.runtime.Settings"
	_, err := Load(writeConfig(t, externalAttributeConfig(t, []PythonExternalAttribute{first, second})), "")
	if err == nil || !strings.Contains(err.Error(), "duplicates Python external attribute") {
		t.Fatalf("competing contracts: %v", err)
	}
}

func exactExternalAttribute() PythonExternalAttribute {
	return PythonExternalAttribute{
		Project: "pyproject.toml", Module: "plugin", Callable: "configure", Attribute: "output_path",
		Write: PythonSourceLocation{Line: 12, Column: 5},
		Receiver: PythonExternalReceiver{
			Kind: "parameter", Name: "settings", Binding: PythonSourceLocation{Line: 3, Column: 15}, Type: "external.runtime.Settings",
		},
	}
}

func externalAttributeConfig(t *testing.T, attributes []PythonExternalAttribute) string {
	t.Helper()
	data, err := json.Marshal(attributes)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonExternalAttributes":`+string(data)+`},"quality":{}`, 1)
}
