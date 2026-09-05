package engine

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestDesignContextExplainsSharedMatchesAndPartialCoverage(t *testing.T) {
	t.Parallel()
	policyEngine := designCoverageEngine(t)
	request := ContextRequest{Mode: "files", Files: []string{"app/detail.go", "worker/main.go", "worker/extra.go", "unowned.go"}}
	report, err := policyEngine.DesignContext(request)
	if err != nil || HasFindings(report) || report.RepositoryContext == nil {
		t.Fatalf("context = %+v, %v", report, err)
	}
	resolution := report.RepositoryContext.DesignResolution
	expected := []repository.DesignMatch{
		{Path: "docs/design/application.md", Reasons: []repository.DesignMatchReason{{Kind: "module", Value: "app"}}},
		{Path: "docs/design/shared.md", Reasons: []repository.DesignMatchReason{{Kind: "source", Value: "app/detail.go"}, {Kind: "source", Value: "worker/main.go"}}},
	}
	if !reflect.DeepEqual(resolution.Matches, expected) || !slices.Equal(resolution.UnmappedModules, []string{"worker"}) || !slices.Equal(resolution.UnmappedPaths, []string{"unowned.go", "worker/extra.go"}) {
		t.Fatalf("incorrect matches or coverage: %+v", resolution)
	}
	report.Command = "design-context"
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCanonicalReport(data); err != nil {
		t.Fatal(err)
	}
	if findings := policyEngine.Repository.DesignDocumentFindings(); len(findings) != 1 || findings[0].Subject != "docs/design/unused.md" {
		t.Fatalf("full inventory lost stale mapping diagnostics: %+v", findings)
	}
}

func TestDesignContextRefreshesMappingAndDocumentEvidence(t *testing.T) {
	t.Parallel()
	policyEngine := designCoverageEngine(t)
	request := ContextRequest{Mode: "files", Files: []string{"worker/extra.go"}}
	before, err := policyEngine.DesignContext(request)
	if err != nil || before.RepositoryContext == nil || len(before.RepositoryContext.DesignResolution.UnmappedPaths) != 1 {
		t.Fatalf("before: %+v, %v", before, err)
	}
	path := "docs/design/worker.md"
	writeEngineFile(t, policyEngine.Repository.Root, path, "# Worker\nOwns retry scheduling.\n", 0o600)
	policyEngine.Repository.Config.Documentation.Design = append(policyEngine.Repository.Config.Documentation.Design, policy.DesignDocument{Path: path, Module: "worker"})
	mapped, err := policyEngine.DesignContext(request)
	if err != nil || mapped.RepositoryContext == nil || len(mapped.RepositoryContext.DesignResolution.UnmappedPaths) != 0 || len(mapped.RepositoryContext.DesignDocuments) != 1 {
		t.Fatalf("mapped: %+v, %v", mapped, err)
	}
	writeEngineFile(t, policyEngine.Repository.Root, path, "# Worker\nOwns bounded retry scheduling; callers own policy.\n", 0o600)
	changed, err := policyEngine.DesignContext(request)
	if err != nil || changed.RepositoryContext == nil || changed.RepositoryContext.DesignDocuments[0].SHA256 == mapped.RepositoryContext.DesignDocuments[0].SHA256 {
		t.Fatalf("changed: %+v, %v", changed, err)
	}
}

func TestDesignContextDistinguishesEmptyScopeFromUnmappedEmptyModule(t *testing.T) {
	t.Parallel()
	policyEngine := designCoverageEngine(t)
	empty, err := policyEngine.DesignContext(ContextRequest{Mode: "none"})
	if err != nil || empty.RepositoryContext == nil || len(empty.RepositoryContext.DesignResolution.SelectedModules) != 0 || empty.RepositoryContext.DesignResolution.SelectedPathCount != 0 {
		t.Fatalf("empty: %+v, %v", empty, err)
	}
	policyEngine.Repository.Config.Modules = append(policyEngine.Repository.Config.Modules, policy.Module{Name: "future", Paths: []string{"future/**"}})
	missing, err := policyEngine.DesignContext(ContextRequest{Modules: []string{"future"}})
	if err != nil || missing.RepositoryContext == nil || !slices.Equal(missing.RepositoryContext.DesignResolution.UnmappedModules, []string{"future"}) {
		t.Fatalf("missing: %+v, %v", missing, err)
	}
	if _, err := policyEngine.Select("modules", []string{"future"}); err == nil {
		t.Fatal("ordinary checks accepted an empty module execution selection")
	}
}

func designCoverageEngine(t *testing.T) *Engine {
	t.Helper()
	root := t.TempDir()
	for path, data := range map[string]string{
		"app/detail.go": "package app\n", "worker/main.go": "package worker\n", "worker/extra.go": "package worker\n", "unowned.go": "package unowned\n",
		"docs/design/application.md": "# Application\nOwns user-facing decisions.\n",
		"docs/design/shared.md":      "# Boundary\nWorkers consume application requests through immutable values.\n",
	} {
		writeEngineFile(t, root, path, data, 0o600)
	}
	return &Engine{Repository: repository.Repository{Root: root, Config: policy.Config{
		Modules: []policy.Module{{Name: "app", Paths: []string{"app/**"}}, {Name: "worker", Paths: []string{"worker/**"}}, {Name: "unused", Paths: []string{"unused/**"}}},
		Documentation: policy.Documentation{Design: []policy.DesignDocument{
			{Path: "docs/design/application.md", Module: "app"},
			{Path: "docs/design/shared.md", SourcePaths: []string{"app/detail.go", "worker/main.go"}},
			{Path: "docs/design/unused.md", Module: "unused"},
		}},
	}}}
}
