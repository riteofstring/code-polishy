package engine

import (
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDesignContextReturnsBoundedCurrentDocuments(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for path, contents := range map[string]string{
		"docs/design/application.md": "# Application\n",
		"docs/design/entry.md":       "# Entry\n",
		"src/model.go":               "package sample\n",
		"src/entry.go":               "package sample\n",
	} {
		writeEngineFile(t, root, path, contents, 0o600)
	}
	policyEngine := &Engine{Repository: repository.Repository{Root: root, Config: policy.Config{
		Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}},
		Documentation: policy.Documentation{Design: []policy.DesignDocument{
			{Path: "docs/design/application.md", Module: "application"},
			{Path: "docs/design/entry.md", SourcePaths: []string{"src/entry.go"}},
		}},
	}}}
	files, findings, err := policyEngine.DesignContext("files", []string{"src/model.go", "src/entry.go"}, nil)
	if err != nil || len(findings) != 0 || !slices.Equal(files, []string{"docs/design/application.md", "docs/design/entry.md"}) {
		t.Fatalf("file context = %v, findings = %+v, err = %v", files, findings, err)
	}
	direct, findings, err := policyEngine.DesignContext("files", []string{"src/entry.go"}, nil)
	if err != nil || len(findings) != 0 || !slices.Equal(direct, []string{"docs/design/entry.md"}) {
		t.Fatalf("direct context = %v, findings = %+v, err = %v", direct, findings, err)
	}
	modules, findings, err := policyEngine.DesignContext("changes", nil, []string{"application"})
	if err != nil || len(findings) != 0 || !slices.Equal(modules, []string{"docs/design/application.md", "docs/design/entry.md"}) {
		t.Fatalf("module context = %v, findings = %+v, err = %v", modules, findings, err)
	}
}

func TestDesignContextReportsStaleMappings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	policyEngine := &Engine{Repository: repository.Repository{Root: root, Config: policy.Config{
		Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}},
		Documentation: policy.Documentation{Design: []policy.DesignDocument{{
			Path: "docs/design/missing.md", Module: "application",
		}}},
	}}}
	_, findings, err := policyEngine.DesignContext("changes", nil, []string{"application"})
	if err != nil || len(findings) != 1 || findings[0].Check != repository.DesignDocumentationCheck || findings[0].Subject != "docs/design/missing.md" {
		t.Fatalf("findings = %+v, err = %v", findings, err)
	}
}
