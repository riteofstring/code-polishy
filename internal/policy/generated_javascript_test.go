package policy

import (
	"strings"
	"testing"
)

func TestScopeSourceContextAcceptsExactContextOwnership(t *testing.T) {
	t.Parallel()
	configText := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"sourceContexts":[{
  "paths":["generated/**/*.ts"],"context":"packages/app/package.json"
}]},"quality":{}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	owners := config.Scope.SourceContexts
	if len(owners) != 1 || owners[0].Context != "packages/app/package.json" || len(owners[0].Paths) != 1 {
		t.Fatalf("generated JavaScript ownership = %+v", owners)
	}
}

func TestScopeSourceContextRejectsBroadOrInexactOwnership(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		declaration string
		want        string
	}{
		"empty paths":      {`{"paths":[],"context":"package.json"}`, schemaRejection},
		"universal paths":  {`{"paths":["**/*"],"context":"package.json"}`, "cannot hide the entire repository"},
		"globbed package":  {`{"paths":["generated/**"],"context":"packages/*/package.json"}`, schemaRejection},
		"escaping context": {`{"paths":["generated/**"],"context":"../project.json"}`, schemaRejection},
		"unknown property": {`{"paths":["generated/**"],"context":"package.json","module":"app"}`, schemaRejection},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			configText := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"sourceContexts":[`+testCase.declaration+`]},"quality":{}`, 1)
			_, err := Load(writeConfig(t, configText), "")
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("Load() error = %v, want %q", err, testCase.want)
			}
		})
	}
}
