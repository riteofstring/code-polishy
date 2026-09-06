package policy

import (
	"strings"
	"testing"
)

func TestPythonRuntimeLoaderConfiguration(t *testing.T) {
	declaration := `{"project":"pyproject.toml","consumer":{"kind":"callsite","importer":"loader.py","module":"loader","callable":"load","site":{"line":10,"column":5},"callee":"importlib.import_module","shape":"call","argument":"module_name","sourceSha256":"` + strings.Repeat("a", 64) + `"},"inputGrammar":"ascii-module-object/v1","check":{"kind":"isinstance","protocol":"loader.Contract","site":{"line":13,"column":12}},"reason":"Operators select adapters at startup."}`
	for name, change := range map[string]string{
		"valid":             declaration,
		"duplicate":         declaration + "," + declaration,
		"escaping importer": strings.Replace(declaration, "loader.py", "../loader.py", 1),
		"unknown grammar":   strings.Replace(declaration, "ascii-module-object/v1", "any-string", 1),
		"wrong loader":      strings.Replace(declaration, "importlib.import_module", "pkgutil.resolve_name", 1),
		"missing reason":    strings.Replace(declaration, `"reason":"Operators select adapters at startup."`, `"reason":""`, 1),
		"wrong check":       strings.Replace(declaration, "isinstance", "issubclass", 1),
	} {
		t.Run(name, func(t *testing.T) {
			input := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonRuntimeLoaders":[`+change+`]},"quality":{}`, 1)
			_, err := Load(writeConfig(t, input), "")
			if (err == nil) != (name == "valid") {
				t.Fatalf("configuration validity: %v", err)
			}
		})
	}
}
