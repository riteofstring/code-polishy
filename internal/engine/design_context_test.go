package engine

import (
	"slices"
	"testing"
	"time"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDesignContextIgnoresUnrelatedGlobalAssessmentStatus(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeEngineFile(t, root, "docs/design/application.md", "# Application\n", 0o600)
	writeEngineFile(t, root, "src/model.go", "package sample\n", 0o600)
	policyEngine := &Engine{Repository: repository.Repository{Root: root, Config: policy.Config{
		Modules: []policy.Module{{Name: "application", Paths: []string{"src/**"}}},
		Documentation: policy.Documentation{Design: []policy.DesignDocument{{
			Path: "docs/design/application.md", Module: "application",
		}}},
		SupplyChain: policy.SupplyChain{VulnerabilityAssessments: []policy.VulnerabilityAssessment{{
			ID: "expired-unrelated", Expires: policy.Date{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		}}},
	}}}
	global := policyEngine.finish(nil, nil)
	if len(global.Findings) != 1 || global.Findings[0].Check != "policy.vulnerabilityAssessmentExpired" {
		t.Fatalf("global assessment status = %+v", global.Findings)
	}
	report, err := policyEngine.DesignContext(ContextRequest{Modules: []string{"application"}})
	if err != nil || len(report.Findings) != 0 || report.RepositoryContext == nil || len(report.RepositoryContext.DesignDocuments) != 1 {
		t.Fatalf("selected context = %+v, err = %v", report, err)
	}
}

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
	for _, testCase := range []struct {
		request ContextRequest
		paths   []string
	}{
		{request: ContextRequest{Mode: "files", Files: []string{"src/model.go", "src/entry.go"}}, paths: []string{"docs/design/application.md", "docs/design/entry.md"}},
		{request: ContextRequest{Mode: "files", Files: []string{"src/entry.go"}}, paths: []string{"docs/design/application.md", "docs/design/entry.md"}},
		{request: ContextRequest{Modules: []string{"application"}}, paths: []string{"docs/design/application.md", "docs/design/entry.md"}},
	} {
		report, err := policyEngine.DesignContext(testCase.request)
		if err != nil || len(report.Findings) != 0 || report.RepositoryContext == nil {
			t.Fatalf("context = %+v, err = %v", report, err)
		}
		paths := []string{}
		for _, document := range report.RepositoryContext.DesignDocuments {
			paths = append(paths, document.Path)
			if document.Content == "" || len(document.SHA256) != 64 {
				t.Fatalf("context lacks bound document contents: %+v", document)
			}
		}
		if !slices.Equal(paths, testCase.paths) {
			t.Fatalf("context paths = %v, want %v", paths, testCase.paths)
		}
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
	report, err := policyEngine.DesignContext(ContextRequest{Modules: []string{"application"}})
	findings := report.Findings
	if err != nil || len(findings) != 1 || findings[0].Check != repository.DesignDocumentationCheck || findings[0].Subject != "docs/design/missing.md" {
		t.Fatalf("findings = %+v, err = %v", findings, err)
	}
}
