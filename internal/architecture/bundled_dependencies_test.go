package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestBundledDevelopmentDependencyRequiresVerifiedArtifacts(t *testing.T) {
	repo := bundledPackageRepository(t)
	declarations := repo.Config.Scope.BundledDependencies
	repo.Config.Scope.BundledDependencies = nil
	findings := Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Check != "architecture.packageDependency" || findings[0].Subject != "@editor/view" {
		t.Fatalf("undeclared bundling = %+v", findings)
	}
	repo.Config.Scope.BundledDependencies = declarations
	if findings := Check(t.Context(), repo, []string{"web/app.ts"}); len(findings) != 0 {
		t.Fatalf("verified bundle = %+v", findings)
	}
	if err := os.Remove(filepath.Join(repo.Root, "dist/index.js")); err != nil {
		t.Fatal(err)
	}
	findings = Check(t.Context(), repo, []string{"web/app.ts"})
	if !bundledFinding(findings, "architecture.importCoverage", "package.json") || !bundledFinding(findings, "architecture.packageDependency", "web/app.ts") {
		t.Fatalf("missing build retained bundle authorization: %+v", findings)
	}
}

func TestBundledDependenciesRejectRuntimeAndPublicTypeLeaks(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"runtime chunk": {"dist/index.js": `export * from "./chunk.js";`, "dist/chunk.js": `export * from "@editor/view";`},
		"public types":  {"dist/types/index.d.ts": `export type { Editor } from "./editor.js";`, "dist/types/editor.d.ts": `export type Editor = import("@editor/view").Editor;`},
	} {
		t.Run(name, func(t *testing.T) {
			repo := bundledPackageRepository(t)
			for path, source := range files {
				writeArchitectureFile(t, repo.Root, path, source)
			}
			findings := Check(t.Context(), repo, []string{"web/app.ts"})
			leak := false
			for _, finding := range findings {
				leak = leak || finding.Check == "architecture.packageDependency" && strings.HasPrefix(finding.Path, "dist/") && finding.Subject == "@editor/view"
			}
			if !leak || !bundledFinding(findings, "architecture.packageDependency", "web/app.ts") {
				t.Fatalf("leaked dependency was admitted: %+v", findings)
			}
		})
	}
}

func TestBundledDependenciesPreserveOtherPackageBoundaries(t *testing.T) {
	repo := bundledPackageRepository(t)
	writeArchitectureFile(t, repo.Root, "web/app.ts", `import "@editor/view"; import "other-dev";`)
	findings := Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Subject != "other-dev" {
		t.Fatalf("bundling weakened other development dependencies: %+v", findings)
	}
	writeArchitectureFile(t, repo.Root, "web/package.json", `{"devDependencies":{"@editor/view":"1.0.0","other-dev":"1.0.0"}}`)
	findings = Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 2 {
		t.Fatalf("outer manifest authorized a nested package: %+v", findings)
	}
}

func TestBundledDependenciesRejectContradictoryAndMissingDeclarations(t *testing.T) {
	for _, field := range []string{"dependencies", "peerDependencies", "optionalDependencies", "absent"} {
		t.Run(field, func(t *testing.T) {
			repo := bundledPackageRepository(t)
			manifest := `{"name":"app","devDependencies":{"@editor/view":"1.0.0"},"` + field + `":{"@editor/view":"1.0.0"}}`
			if field == "absent" {
				manifest = `{"name":"app"}`
			}
			writeArchitectureFile(t, repo.Root, "package.json", manifest)
			findings := Check(t.Context(), repo, []string{"web/app.ts"})
			if !bundledFinding(findings, "architecture.packageDependency", "package.json") {
				t.Fatalf("invalid manifest claim accepted: %+v", findings)
			}
		})
	}
}

func TestProviderImportsUseTheSameVerifiedBundleBoundary(t *testing.T) {
	repo := bundledPackageRepository(t)
	files, err := repo.AllFiles()
	if err != nil {
		t.Fatal(err)
	}
	packages := newNodePackages(t.Context(), repo, files)
	findings := providerImportChecks(repo, packages, map[string]bool{}, pack.ImportFact{Path: "web/app.ts", Package: "@editor/view", Specifier: "@editor/view", Line: 1})
	if len(findings) != 0 || len(packages.coverage) != 0 {
		t.Fatalf("provider bundle boundary = %+v, %+v", findings, packages.coverage)
	}
}

func TestBundledDeclarationsAreCheckedWithoutSourceImports(t *testing.T) {
	repo := bundledPackageRepository(t)
	writeArchitectureFile(t, repo.Root, "web/app.ts", "export {};\n")
	writeArchitectureFile(t, repo.Root, "dist/index.js", `export * from "@editor/view";`)
	findings := Check(t.Context(), repo, []string{"package.json"})
	if !bundledFinding(findings, "architecture.packageDependency", "dist/index.js") {
		t.Fatalf("manifest-only selection omitted artifact verification: %+v", findings)
	}
	repo.Config.Scope.BundledDependencies[0].Manifest = "missing/package.json"
	findings = Check(t.Context(), repo, []string{"web/app.ts"})
	if !bundledFinding(findings, "architecture.importCoverage", "missing/package.json") {
		t.Fatalf("missing manifest declaration was ignored: %+v", findings)
	}
}

func bundledPackageRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := nodePackageRepository(t, `{"name":"app","devDependencies":{"@editor/view":"1.0.0","other-dev":"1.0.0"},"exports":{".":{"import":"./dist/index.js","types":"./dist/types/index.d.ts"}}}`)
	repo.Config.Scope.Exclude = []string{"dist/**"}
	repo.Config.Scope.BundledDependencies = []policy.BundledDependencies{{Manifest: "package.json", Packages: []string{"@editor/view"}}}
	policyRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	repo.PolicyRoot = policyRoot
	writeArchitectureFile(t, repo.Root, "web/app.ts", `import "@editor/view";`)
	writeArchitectureFile(t, repo.Root, "dist/index.js", `export const editor = "inlined";`)
	writeArchitectureFile(t, repo.Root, "dist/types/index.d.ts", `export declare const editor: string;`)
	return repo
}

func bundledFinding(findings []policy.Finding, check, path string) bool {
	for _, finding := range findings {
		if finding.Check == check && finding.Path == path {
			return true
		}
	}
	return false
}
