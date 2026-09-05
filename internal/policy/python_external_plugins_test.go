package policy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPythonExternalPluginSchemaBindsDependencyAndRuntimeContract(t *testing.T) {
	declaration := externalPluginTestDeclaration()
	config, err := loadExternalPluginTestConfig(t, []PythonExternalPluginImport{declaration})
	if err != nil || len(config.Scope.PythonExternalPluginImports) != 1 || config.Scope.PythonExternalPluginImports[0].Check.Protocol != "app.Contract" {
		t.Fatalf("external plug-in contract was not preserved: %+v, %v", config, err)
	}
	for name, mutate := range map[string]func(*PythonExternalPluginImport){
		"foreign importer": func(value *PythonExternalPluginImport) { value.Consumer.Importer = "other/loader.py" },
		"foreign registry": func(value *PythonExternalPluginImport) {
			value.Configuration = &PythonComputedImportInput{Path: "other/registry.json", SHA256: strings.Repeat("b", 64)}
		},
		"moving digest":             func(value *PythonExternalPluginImport) { value.Consumer.SourceSHA256 = "latest" },
		"unsupported callee":        func(value *PythonExternalPluginImport) { value.Consumer.Callee = "eval" },
		"wildcard namespace":        func(value *PythonExternalPluginImport) { value.Namespace = "plug_dist.*" },
		"relative namespace":        func(value *PythonExternalPluginImport) { value.Namespace = ".plug_dist" },
		"noncanonical distribution": func(value *PythonExternalPluginImport) { value.Distribution = "Plug_Dist" },
		"invalid grammar":           func(value *PythonExternalPluginImport) { value.InputGrammar = "python-expression/v1" },
		"annotation only":           func(value *PythonExternalPluginImport) { value.Check.Kind = "annotation" },
		"missing check site":        func(value *PythonExternalPluginImport) { value.Check.Site = PythonSourceLocation{} },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := declaration
			mutate(&candidate)
			if _, err := loadExternalPluginTestConfig(t, []PythonExternalPluginImport{candidate}); err == nil {
				t.Fatal("invalid external contract was admitted")
			}
		})
	}
	duplicate := declaration
	duplicate.Namespace = "other_plugin"
	if _, err := loadExternalPluginTestConfig(t, []PythonExternalPluginImport{declaration, duplicate}); err == nil {
		t.Fatal("duplicate loader was admitted")
	}
}

func TestPythonExternalPluginEvidenceCannotBeSuppressed(t *testing.T) {
	now := time.Now()
	finding := Finding{Check: "policy.pythonExternalPluginImport", Path: "src/loader.py", Subject: "plug-dist"}
	kept, suppressed := ApplyExceptions([]Finding{finding}, []Exception{{Check: finding.Check, Path: finding.Path, Subject: finding.Subject, Expires: Date{Time: now.Add(24 * time.Hour)}}}, now)
	if len(kept) != 1 || len(suppressed) != 0 {
		t.Fatal("external plug-in evidence failure was suppressed")
	}
}

func externalPluginTestDeclaration() PythonExternalPluginImport {
	return PythonExternalPluginImport{Project: "apps/api/pyproject.toml", Distribution: "plug-dist", Namespace: "plug_dist.plugins", InputGrammar: "python-module-object/v1", Consumer: PythonDynamicConsumer{Kind: "callsite", Importer: "apps/api/src/loader.py", Module: "loader", Callable: "load", Site: PythonSourceLocation{Line: 4, Column: 14}, Callee: "pkgutil.resolve_name", Shape: "module-object-call/v1", Argument: "name", SourceSHA256: strings.Repeat("a", 64)}, Check: PythonRuntimeCheck{Kind: "isinstance", Protocol: "app.Contract", Site: PythonSourceLocation{Line: 5, Column: 12}}}
}

func loadExternalPluginTestConfig(t *testing.T, declarations []PythonExternalPluginImport) (Config, error) {
	t.Helper()
	data, err := json.Marshal(declarations)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonExternalPluginImports":`+string(data)+`},"quality":{}`, 1)
	return Load(writeConfig(t, text), "")
}
