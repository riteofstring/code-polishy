package policy

import (
	"strings"
	"testing"
)

func TestBundledDependenciesRequireExactPackageDeclarations(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name         string
		declarations string
		valid        bool
	}{
		{"exact names", `[{"manifest":"packages/ui/package.json","artifact":"packages/ui/ui.tgz","packages":["@editor/view","plain"]}]`, true},
		{"empty packages", `[{"manifest":"package.json","artifact":"app.tgz","packages":[]}]`, false},
		{"wildcard", `[{"manifest":"package.json","artifact":"app.tgz","packages":["*"]}]`, false},
		{"scope wildcard", `[{"manifest":"package.json","artifact":"app.tgz","packages":["@editor/*"]}]`, false},
		{"subpath", `[{"manifest":"package.json","artifact":"app.tgz","packages":["@editor/view/internal"]}]`, false},
		{"globbed manifest", `[{"manifest":"packages/*/package.json","artifact":"app.tgz","packages":["editor"]}]`, false},
		{"escaping manifest", `[{"manifest":"../package.json","artifact":"app.tgz","packages":["editor"]}]`, false},
		{"wrong manifest", `[{"manifest":"manifest.json","artifact":"app.tgz","packages":["editor"]}]`, false},
		{"duplicate packages", `[{"manifest":"package.json","artifact":"app.tgz","packages":["editor","editor"]}]`, false},
		{"missing artifact", `[{"manifest":"package.json","packages":["editor"]}]`, false},
		{"unpacked artifact", `[{"manifest":"package.json","artifact":"dist/index.js","packages":["editor"]}]`, false},
		{"escaping artifact", `[{"manifest":"package.json","artifact":"../app.tgz","packages":["editor"]}]`, false},
		{"duplicate manifest", `[{"manifest":"package.json","artifact":"app.tgz","packages":["editor"]},{"manifest":"package.json","artifact":"app.tgz","packages":["other"]}]`, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			text := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"bundledDependencies":`+testCase.declarations+`},"quality":{}`, 1)
			config, err := Parse([]byte(text), "")
			if (err == nil) != testCase.valid {
				t.Fatalf("Parse() = %+v, %v", config, err)
			}
			if testCase.valid && config.Scope.BundledDependencies[0].Packages[0] != "@editor/view" {
				t.Fatalf("bundled declarations = %+v", config.Scope.BundledDependencies)
			}
		})
	}
}
