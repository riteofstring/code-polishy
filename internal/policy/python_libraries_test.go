package policy

import (
	"slices"
	"strings"
	"testing"
)

func TestPythonLibraryScopeRequiresExactManifestPaths(t *testing.T) {
	t.Parallel()
	for _, declaration := range []string{
		`["../pyproject.toml"]`, `["/pyproject.toml"]`, `["*/pyproject.toml"]`,
		`["./pyproject.toml"]`, `["a//pyproject.toml"]`, `["package.json"]`,
		`["pyproject.toml", "pyproject.toml"]`, `["a\\pyproject.toml"]`,
	} {
		config := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{"pythonLibraries":`+declaration+`}`, 1)
		if _, err := Parse([]byte(config), ConfigFilename); err == nil {
			t.Errorf("invalid library paths accepted: %s", declaration)
		}
	}
	config := strings.Replace(minimalConfig(), `"supplyChain":{}`, `"supplyChain":{"pythonLibraries":["pyproject.toml","packages/sdk/pyproject.toml"]}`, 1)
	parsed, err := Parse([]byte(config), ConfigFilename)
	if err != nil || !slices.Equal(parsed.SupplyChain.PythonLibraries, []string{"pyproject.toml", "packages/sdk/pyproject.toml"}) {
		t.Fatalf("library manifest scope = %v, error = %v", parsed.SupplyChain.PythonLibraries, err)
	}
}
