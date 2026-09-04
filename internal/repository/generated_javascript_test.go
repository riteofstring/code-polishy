package repository

import (
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/policy"
)

func TestGeneratedJavaScriptInheritsItsDeclaredSourceModule(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "packages/app/package.json", "{}\n")
	writeFile(t, root, "generated/client.ts", "export {};\n")
	repo := Repository{Root: root, Config: policy.Config{
		Scope: policy.Scope{
			Generated:           []string{"generated/**"},
			GeneratedJavaScript: []policy.GeneratedJavaScript{{Paths: []string{"generated/**"}, SourcePackage: "packages/app/package.json"}},
		},
		Modules: []policy.Module{{Name: "app", Paths: []string{"packages/app/**"}}}, ModuleByName: map[string]int{"app": 0},
	}}
	files := []string{"generated/client.ts", "packages/app/package.json"}
	if findings := repo.GeneratedJavaScriptOwnershipFindings(files); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	owners := repo.ModuleNames("generated/client.ts")
	if len(owners) != 1 || owners[0] != "app" || repo.JavaScriptContextPath("generated/client.ts") != "packages/app/package.json" {
		t.Fatalf("owners = %v, context = %q", owners, repo.JavaScriptContextPath("generated/client.ts"))
	}
}

func TestGeneratedJavaScriptOwnershipRejectsAmbiguousStaleAndCyclicMappings(t *testing.T) {
	t.Parallel()
	repo := Repository{Root: t.TempDir(), Config: policy.Config{Scope: policy.Scope{
		Generated: []string{"generated/**", "packages/source/**"},
		GeneratedJavaScript: []policy.GeneratedJavaScript{
			{Paths: []string{"generated/**"}, SourcePackage: "packages/source/package.json"},
			{Paths: []string{"generated/client.*"}, SourcePackage: "missing/package.json"},
			{Paths: []string{"absent/**"}, SourcePackage: "packages/source/package.json"},
			{Paths: []string{"packages/source/**"}, SourcePackage: "packages/source/package.json"},
		},
	}}}
	writeFile(t, repo.Root, "generated/client.ts", "export {};\n")
	writeFile(t, repo.Root, "packages/source/package.json", "{}\n")
	findings := repo.GeneratedJavaScriptOwnershipFindings([]string{"generated/client.ts", "packages/source/package.json"})
	seen := map[string]bool{}
	for _, finding := range findings {
		seen[finding.Message] = true
	}
	for _, fragment := range []string{"more than one source-package", "does not exist", "do not match any current file", "cannot itself be generated"} {
		found := false
		for message := range seen {
			if strings.Contains(message, fragment) {
				found = true
			}
		}
		if !found {
			t.Fatalf("findings = %+v, want %q", findings, fragment)
		}
	}
}
