package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScopePythonDynamicReferencesRequireConsumerBoundForms(t *testing.T) {
	target, registry := dynamicPolicyReference(), dynamicPolicyReference()
	registry.Kind, registry.Target = "registry", nil
	registry.Registry = &PythonDynamicRegistry{Path: "src/registry.json", JSONPointer: "/plugins"}
	registry.Consumer.Site.Line = 8
	config, err := loadDynamicPolicy(t, []PythonDynamicReference{registry, target})
	if err != nil || len(config.Scope.PythonDynamicReferences) != 2 || config.Scope.PythonDynamicReferences[0].Kind != "target" || config.Scope.PythonDynamicReferences[1].Registry.Path != "src/registry.json" {
		t.Fatalf("consumer forms: %+v, %v", config.Scope.PythonDynamicReferences, err)
	}
}

func TestScopePythonDynamicReferencesRejectUnboundOrCompetingEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*PythonDynamicReference){
		"missing kind":       func(r *PythonDynamicReference) { r.Kind = "" },
		"missing target":     func(r *PythonDynamicReference) { r.Target = nil },
		"mixed forms":        func(r *PythonDynamicReference) { r.Registry = &PythonDynamicRegistry{Path: "registry.json"} },
		"wildcard target":    func(r *PythonDynamicReference) { r.Target.Symbol = "Plugin.*" },
		"missing consumer":   func(r *PythonDynamicReference) { r.Consumer = PythonDynamicConsumer{} },
		"unsupported callee": func(r *PythonDynamicReference) { r.Consumer.Callee = "custom.resolve" },
		"missing digest":     func(r *PythonDynamicReference) { r.Consumer.SourceSHA256 = "" },
		"escaped importer":   func(r *PythonDynamicReference) { r.Consumer.Importer = "../loader.py" },
		"wrong project":      func(r *PythonDynamicReference) { r.Project = "other/pyproject.toml" },
	} {
		t.Run(name, func(t *testing.T) {
			reference := dynamicPolicyReference()
			mutate(&reference)
			if _, err := loadDynamicPolicy(t, []PythonDynamicReference{reference}); err == nil {
				t.Fatal("invalid consumer accepted")
			}
		})
	}
	first, second := dynamicPolicyReference(), dynamicPolicyReference()
	second.Target.Symbol = "AnotherPlugin"
	if _, err := loadDynamicPolicy(t, []PythonDynamicReference{first, second}); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("competing consumers accepted: %v", err)
	}
	legacy := `"scope":{"pythonDynamicReferences":[{"project":"pyproject.toml","module":"app","symbol":"serve"}]},"quality":{}`
	if _, err := Load(writeConfig(t, strings.Replace(minimalConfig(), `"quality":{}`, legacy, 1)), ""); err == nil {
		t.Fatal("unbound legacy symbol accepted")
	}
}

func dynamicPolicyReference() PythonDynamicReference {
	return PythonDynamicReference{Kind: "target", Project: "pyproject.toml", Target: &PythonDynamicTarget{Module: "app", Symbol: "Plugin"}, Consumer: PythonDynamicConsumer{
		Kind: "callsite", Importer: "src/loader.py", Module: "loader", Callable: "load", Site: PythonSourceLocation{Line: 3, Column: 12}, Callee: "pkgutil.resolve_name", Shape: "module-object-call/v1", Argument: "'app:Plugin'", SourceSHA256: strings.Repeat("a", 64),
	}}
}

func loadDynamicPolicy(t *testing.T, references []PythonDynamicReference) (Config, error) {
	t.Helper()
	data, err := json.Marshal(references)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonDynamicReferences":`+string(data)+`},"quality":{}`, 1)
	return Load(writeConfig(t, text), "")
}
