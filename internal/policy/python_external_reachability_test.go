package policy

import (
	"strings"
	"testing"
)

func TestExternalReachabilitySchemaRequiresTargetContract(t *testing.T) {
	for _, kind := range []string{"base", "protocol", "decorator", "entry-point"} {
		reference := PythonDynamicReference{Kind: "target", Project: "pyproject.toml", Target: &PythonDynamicTarget{Module: "app", Symbol: "Plugin.run"}, Consumer: PythonDynamicConsumer{
			Kind: kind, Importer: "src/app.py", Module: "app", Site: PythonSourceLocation{Line: 2, Column: 1}, SourceSHA256: strings.Repeat("a", 64),
			Distribution: "framework", Qualified: "framework.Contract", Implementation: "Plugin", Member: "run",
		}}
		if _, err := loadDynamicPolicy(t, []PythonDynamicReference{reference}); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		for name, mutate := range map[string]func(*PythonDynamicReference){
			"no distribution":   func(r *PythonDynamicReference) { r.Consumer.Distribution = "" },
			"no implementation": func(r *PythonDynamicReference) { r.Consumer.Implementation = "" },
			"no member":         func(r *PythonDynamicReference) { r.Consumer.Member = "" },
			"mixed callsite":    func(r *PythonDynamicReference) { r.Consumer.Callee = "pkgutil.resolve_name" },
			"registry": func(r *PythonDynamicReference) {
				r.Kind = "registry"
				r.Target = nil
				r.Registry = &PythonDynamicRegistry{Path: "src/plugins.json"}
			},
		} {
			t.Run(kind+"/"+name, func(t *testing.T) {
				changed := reference
				mutate(&changed)
				if _, err := loadDynamicPolicy(t, []PythonDynamicReference{changed}); err == nil {
					t.Fatal("incomplete or mixed contract accepted")
				}
			})
		}
	}
}
