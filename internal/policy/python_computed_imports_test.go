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
