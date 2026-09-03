package policy

import (
	"slices"
	"strings"
	"testing"
)

func TestScopePythonDynamicReferencesSortsExactReferences(t *testing.T) {
	t.Parallel()
	configText := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonDynamicReferences":[
  {"project":"apps/worker/pyproject.toml","module":"worker.entry","symbol":"run"},
  {"project":"pyproject.toml","module":"app.entry","symbol":"serve"}
]},"quality":{}`, 1)
	config, err := Load(writeConfig(t, configText), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []PythonDynamicReference{
		{Project: "apps/worker/pyproject.toml", Module: "worker.entry", Symbol: "run"},
		{Project: "pyproject.toml", Module: "app.entry", Symbol: "serve"},
	}
	if !slices.Equal(config.Scope.PythonDynamicReferences, want) {
		t.Fatalf("references = %+v", config.Scope.PythonDynamicReferences)
	}
}

func TestScopePythonDynamicReferencesRejectsNonExactReferences(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		references string
		want       string
	}{
		"duplicate": {
			references: `[{"project":"pyproject.toml","module":"app.entry","symbol":"serve"},{"project":"pyproject.toml","module":"app.entry","symbol":"serve"}]`,
			want:       "duplicates Python dynamic reference",
		},
		"wildcard project": {
			references: `[{"project":"apps/*/pyproject.toml","module":"app.entry","symbol":"serve"}]`,
			want:       "concrete repository path",
		},
		"non manifest": {
			references: `[{"project":"apps/api/project.toml","module":"app.entry","symbol":"serve"}]`,
			want:       "exact pyproject.toml",
		},
		"noncanonical project": {
			references: `[{"project":"./pyproject.toml","module":"app.entry","symbol":"serve"}]`,
			want:       "canonical repository-relative path",
		},
		"invalid module": {
			references: `[{"project":"pyproject.toml","module":"app.*","symbol":"serve"}]`,
			want:       "Python identifier chain",
		},
		"invalid symbol": {
			references: `[{"project":"pyproject.toml","module":"app.entry","symbol":"serve()"}]`,
			want:       "Python identifier chain",
		},
		"unknown field": {
			references: `[{"project":"pyproject.toml","module":"app.entry","symbol":"serve","wildcard":true}]`,
			want:       "unknown field",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			configText := strings.Replace(minimalConfig(), `"quality":{}`, `"scope":{"pythonDynamicReferences":`+testCase.references+`},"quality":{}`, 1)
			_, err := Load(writeConfig(t, configText), "")
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("Load() error = %v, want %q", err, testCase.want)
			}
		})
	}
}
