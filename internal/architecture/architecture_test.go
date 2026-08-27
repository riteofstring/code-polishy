package architecture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestGoImportMustFollowDeclaredModuleDependency(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, false)
	findings := Check(t.Context(), repo, []string{"internal/api/api.go"})
	if len(findings) != 1 || findings[0].Subject != "domain" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestDeclaredGoModuleDependencyPasses(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, true)
	if findings := Check(t.Context(), repo, []string{"internal/api/api.go"}); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCoverageFindsPlaceholderModule(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, true)
	repo.Config.Modules = append(repo.Config.Modules, policy.Module{Name: "missing", Paths: []string{"does-not-exist/**"}})
	findings := CoverageFindings(repo, []string{"go.mod", "internal/api/api.go", "internal/domain/domain.go"})
	if len(findings) != 1 || findings[0].Subject != "missing" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGoImportAcrossNestedModulesUsesRepositoryGraph(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeArchitectureFile(t, root, "go.mod", "module example.test/app\n")
	writeArchitectureFile(t, root, "nested/go.mod", "module example.test/shared\n")
	writeArchitectureFile(t, root, "nested/value/value.go", "package value\n")
	writeArchitectureFile(t, root, "internal/api/api.go", "package api\nimport _ \"example.test/shared/value\"\n")
	config := policy.Config{
		Modules: []policy.Module{
			{Name: "shared", Paths: []string{"nested/value/**"}},
			{Name: "api", Paths: []string{"internal/api/**"}},
		},
		ModuleByName: map[string]int{"shared": 0, "api": 1},
	}
	repo := repository.Repository{Root: root, Config: config}
	findings := Check(t.Context(), repo, []string{"internal/api/api.go"})
	if len(findings) != 1 || findings[0].Subject != "shared" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestGeneratedGoImportStillFollowsRepositoryGraph(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, false)
	repo.Config.Scope.Generated = []string{"internal/api/**"}
	findings := Check(t.Context(), repo, []string{"internal/api/api.go"})
	if len(findings) != 1 || findings[0].Subject != "domain" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestCoverageRejectsGoPackageSplitAcrossModules(t *testing.T) {
	t.Parallel()
	repo := architectureRepository(t, true)
	repo.Config.Modules = []policy.Module{
		{Name: "api-a", Paths: []string{"internal/api/a.go"}},
		{Name: "api-b", Paths: []string{"internal/api/b.go"}},
	}
	writeArchitectureFile(t, repo.Root, "internal/api/a.go", "package api\n")
	writeArchitectureFile(t, repo.Root, "internal/api/b.go", "package api\n")
	findings := CoverageFindings(repo, []string{"internal/api/a.go", "internal/api/b.go"})
	if len(findings) != 1 || findings[0].Subject != "go-package" {
		t.Fatalf("findings = %+v", findings)
	}
}

func architectureRepository(t *testing.T, allow bool) repository.Repository {
	t.Helper()
	root := t.TempDir()
	writeArchitectureFile(t, root, "go.mod", "module example.test/app\n\ngo 1.26.5\n")
	writeArchitectureFile(t, root, "internal/domain/domain.go", "package domain\n")
	writeArchitectureFile(t, root, "internal/api/api.go", "package api\nimport _ \"example.test/app/internal/domain\"\n")
	api := policy.Module{Name: "api", Paths: []string{"internal/api/**"}}
	if allow {
		api.DependsOn = []string{"domain"}
	}
	config := policy.Config{
		Modules:      []policy.Module{{Name: "domain", Paths: []string{"internal/domain/**"}}, api},
		ModuleByName: map[string]int{"domain": 0, "api": 1},
	}
	return repository.Repository{Root: root, Config: config}
}

func writeArchitectureFile(t *testing.T, root, path, contents string) {
	t.Helper()
	absolute := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
