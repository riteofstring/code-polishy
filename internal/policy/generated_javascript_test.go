package policy

import (
	"strings"
	"testing"
)

func TestScopeGeneratedJavaScriptAcceptsExactSourcePackageOwnership(t *testing.T) {
	t.Parallel()
	configText := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"generatedJavaScript":[{
  "paths":["generated/**/*.ts"],"sourcePackage":"packages/app/package.json"
}]},"quality":{}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	owners := config.Scope.GeneratedJavaScript
	if len(owners) != 1 || owners[0].SourcePackage != "packages/app/package.json" || len(owners[0].Paths) != 1 {
		t.Fatalf("generated JavaScript ownership = %+v", owners)
	}
}

func TestScopeGeneratedJavaScriptRejectsBroadOrInexactOwnership(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		declaration string
		want        string
	}{
		"empty paths":      {`{"paths":[],"sourcePackage":"package.json"}`, schemaRejection},
		"universal paths":  {`{"paths":["**/*"],"sourcePackage":"package.json"}`, "cannot hide the entire repository"},
		"globbed package":  {`{"paths":["generated/**"],"sourcePackage":"packages/*/package.json"}`, schemaRejection},
		"wrong manifest":   {`{"paths":["generated/**"],"sourcePackage":"packages/app/project.json"}`, schemaRejection},
		"unknown property": {`{"paths":["generated/**"],"sourcePackage":"package.json","module":"app"}`, schemaRejection},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			configText := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"generatedJavaScript":[`+testCase.declaration+`]},"quality":{}`, 1)
			_, err := Load(writeConfig(t, configText), "")
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("Load() error = %v, want %q", err, testCase.want)
			}
		})
	}
}
