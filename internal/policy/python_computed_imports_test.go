package policy

import (
	"strings"
	"testing"
)

func TestScopePythonComputedImportsRequiresExactBoundedCallsites(t *testing.T) {
	declaration := `{
      "project":"apps/api/pyproject.toml",
      "importer":"apps/api/src/api/loader.py",
      "module":"api.loader",
      "moduleScope":true,
      "callee":"importlib.import_module",
      "line":7,
      "column":3,
      "shape":"Call(func=Attribute(value=Name(id='importlib', ctx=Load()), attr='import_module', ctx=Load()), args=[Name(id='module_name', ctx=Load())], keywords=[])",
      "argument":"module_name",
      "sourceSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "namespace":"api.plugins",
      "targets":["api.plugins.first"]
    }`
	configText := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonComputedImports":[`+declaration+`]},"quality":{}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Scope.PythonComputedImports) != 1 || config.Scope.PythonComputedImports[0].Argument != "module_name" {
		t.Fatalf("computed imports = %+v", config.Scope.PythonComputedImports)
	}
	invalid := []string{
		strings.Replace(declaration, `"moduleScope":true,`, "", 1),
		strings.Replace(declaration, `"namespace":"api.plugins"`, `"namespace":"plugins"`, 1),
		strings.Replace(declaration, `"targets":["api.plugins.first"]`, `"targets":["outside.first"]`, 1),
		strings.Replace(declaration, `"sourceSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, `"sourceSha256":"latest"`, 1),
	}
	for _, candidate := range invalid {
		candidateConfig := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonComputedImports":[`+candidate+`]},"quality":{}`, 1)
		if _, err := Load(writeConfig(t, candidateConfig), ""); err == nil {
			t.Fatalf("invalid declaration was accepted: %s", candidate)
		}
	}
}

func TestScopePythonObjectImportsRequireOneRegistryWithoutTargetInventory(t *testing.T) {
	declaration := `{"project":"pyproject.toml","importer":"src/app/loader.py","module":"app.loader","callable":"load","callee":"pkgutil.resolve_name","line":5,"column":12,"shape":"module-object-call/v1","argument":"registry[name]","sourceSha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","namespace":"app.plugins","configuration":[{"path":"registry.json","jsonPointer":"/plugins","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`
	load := func(candidate string) error {
		text := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonComputedImports":[`+candidate+`]},"quality":{}`, 1)
		_, err := Load(writeConfig(t, text), "")
		return err
	}
	if err := load(declaration); err != nil {
		t.Fatal(err)
	}
	rootSelector := strings.Replace(declaration, `"jsonPointer":"/plugins"`, `"jsonPointer":""`, 1)
	if err := load(rootSelector); err != nil {
		t.Fatalf("object registry root selector: %v", err)
	}
	if err := load(strings.Replace(rootSelector, "pkgutil.resolve_name", "importlib.import_module", 1)); err == nil {
		t.Fatal("module-only imports lost their non-root selector boundary")
	}
	for name, invalid := range map[string]string{
		"duplicate inventory": strings.Replace(declaration, `"configuration":`, `"targets":["app.plugins.first"],"configuration":`, 1),
		"unsupported shape":   strings.Replace(declaration, "module-object-call/v1", "unknown", 1),
		"entry-point variant": strings.Replace(declaration, `"namespace":"app.plugins"`, `"entryPointGroup":"plugins"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := load(invalid); err == nil {
				t.Fatal("object import schema admitted an unsupported contract")
			}
		})
	}
}
