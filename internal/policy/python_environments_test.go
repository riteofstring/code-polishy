package policy

import (
	"strings"
	"testing"
)

func TestPythonTypecheckEnvironmentsRequireExactUnambiguousRuntimeContracts(t *testing.T) {
	declaration := `{"paths":["src/worker.py"],"environmentProject":"typecheck/worker/pyproject.toml","reason":"Executed by the worker runtime."}`
	configuration := func(declarations string) string {
		return strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonTypecheckEnvironments":[`+declarations+`]},"quality":{}`, 1)
	}
	config, err := Parse([]byte(configuration(declaration)), ConfigFilename)
	if err != nil || len(config.Scope.PythonTypecheckEnvironments) != 1 {
		t.Fatalf("runtime contract: %+v, %v", config.Scope, err)
	}
	for name, declarations := range map[string]string{
		"duplicate":             declaration + "," + declaration,
		"glob":                  strings.Replace(declaration, "src/worker.py", "src/*.py", 1),
		"escape":                strings.Replace(declaration, "typecheck/worker/pyproject.toml", "../pyproject.toml", 1),
		"non Python":            strings.Replace(declaration, "src/worker.py", "src/worker.js", 1),
		"environment directory": strings.Replace(declaration, "typecheck/worker/pyproject.toml", "typecheck/worker", 1),
		"no rationale":          strings.Replace(declaration, "Executed by the worker runtime.", " ", 1),
		"overlay":               strings.Replace(declaration, `"paths":`, `"overlay":true,"paths":`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(configuration(declarations)), ConfigFilename); err == nil {
				t.Fatal("invalid runtime contract accepted")
			}
		})
	}
}
