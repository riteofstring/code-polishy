package architecture

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riteofstring/code-polishy/internal/pack"
	"github.com/riteofstring/code-polishy/internal/policy"
	"github.com/riteofstring/code-polishy/internal/repository"
)

func TestBundledDeclarationAuthorizesSourceImportsAndReportsArtifactFailures(t *testing.T) {
	repo := bundledPackageRepository(t)
	declarations := repo.Config.Scope.BundledDependencies
	repo.Config.Scope.BundledDependencies = nil
	findings := checkPacked(t, repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Check != "architecture.packageDependency" || findings[0].Subject != "@editor/view" {
		t.Fatalf("undeclared bundling = %+v", findings)
	}
	repo.Config.Scope.BundledDependencies = declarations
	if findings := checkPacked(t, repo, []string{"web/app.ts"}); len(findings) != 0 {
		t.Fatalf("verified bundle = %+v", findings)
	}
	if err := os.Remove(filepath.Join(repo.Root, "dist/index.js")); err != nil {
		t.Fatal(err)
	}
	findings = checkPacked(t, repo, []string{"web/app.ts"})
	if len(findings) != 1 || !bundledFinding(findings, "architecture.importCoverage", bundledArtifact) {
		t.Fatalf("missing build = %+v", findings)
	}
	if err := os.Remove(filepath.Join(repo.Root, bundledArtifact)); err != nil {
		t.Fatal(err)
	}
	findings = Check(t.Context(), repo, []string{"web/app.ts"})
	if len(findings) != 1 || !bundledFinding(findings, "architecture.importCoverage", bundledArtifact) {
		t.Fatalf("missing packed artifact = %+v", findings)
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
			findings := checkPacked(t, repo, []string{"web/app.ts"})
			leak := false
			for _, finding := range findings {
				leak = leak || finding.Check == "architecture.packageDependency" && finding.Path == bundledArtifact && strings.Contains(finding.Message, "dist/") && finding.Subject == "@editor/view"
			}
			if !leak || bundledFinding(findings, "architecture.packageDependency", "web/app.ts") {
				t.Fatalf("leaked dependency = %+v", findings)
			}
		})
	}
}

func TestBundledArtifactReportsEveryUnverifiablePackedFile(t *testing.T) {
	repo := bundledPackageRepository(t)
	writeArchitectureFile(t, repo.Root, "dist/index.js", `export * from "./missing.js";`)
	writeArchitectureFile(t, repo.Root, "dist/types/index.d.ts", `export * from "./missing.js";`)
	identities := map[string]string{}
	for _, finding := range checkPacked(t, repo, []string{"web/app.ts"}) {
		if finding.Check == "architecture.importCoverage" && finding.Path == bundledArtifact {
			identities[policy.NormalizeFinding(finding).Fingerprint] = finding.Message
		}
	}
	reported := strings.Join(slices.Sorted(maps.Values(identities)), "\n")
	if len(identities) != 2 || !strings.Contains(reported, "dist/index.js: ") || !strings.Contains(reported, "dist/types/index.d.ts: ") {
		t.Fatalf("distinct packed file findings = %v", identities)
	}
}

func TestBundledDependenciesPreserveOtherPackageBoundaries(t *testing.T) {
	repo := bundledPackageRepository(t)
	writeArchitectureFile(t, repo.Root, "web/app.ts", `import "@editor/view"; import "other-dev";`)
	findings := checkPacked(t, repo, []string{"web/app.ts"})
	if len(findings) != 1 || findings[0].Subject != "other-dev" {
		t.Fatalf("bundling weakened other development dependencies: %+v", findings)
	}
	writeArchitectureFile(t, repo.Root, "web/package.json", `{"devDependencies":{"@editor/view":"1.0.0","other-dev":"1.0.0"}}`)
	findings = checkPacked(t, repo, []string{"web/app.ts"})
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
			findings := checkPacked(t, repo, []string{"web/app.ts"})
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
	packBundledArtifact(t, repo)
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
	findings := checkPacked(t, repo, []string{"package.json"})
	if !bundledFinding(findings, "architecture.packageDependency", bundledArtifact) {
		t.Fatalf("manifest-only selection omitted artifact verification: %+v", findings)
	}
	repo.Config.Scope.BundledDependencies[0].Manifest = "missing/package.json"
	findings = checkPacked(t, repo, []string{"web/app.ts"})
	if !bundledFinding(findings, "architecture.importCoverage", "missing/package.json") {
		t.Fatalf("missing manifest declaration was ignored: %+v", findings)
	}
}

func bundledPackageRepository(t *testing.T) repository.Repository {
	t.Helper()
	repo := nodePackageRepository(t, `{"name":"app","devDependencies":{"@editor/view":"1.0.0","other-dev":"1.0.0"},"exports":{".":{"import":"./dist/index.js","types":"./dist/types/index.d.ts"}}}`)
	repo.Config.Scope.Exclude = []string{"dist/**"}
	repo.Config.Scope.BundledDependencies = []policy.BundledDependencies{{Manifest: "package.json", Artifact: bundledArtifact, Packages: []string{"@editor/view"}}}
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

const bundledArtifact = "build/app.tgz"

func checkPacked(t *testing.T, repo repository.Repository, paths []string) []policy.Finding {
	t.Helper()
	packBundledArtifact(t, repo)
	return Check(t.Context(), repo, paths)
}

func packBundledArtifact(t *testing.T, repo repository.Repository) {
	t.Helper()
	target := filepath.Join(repo.Root, bundledArtifact)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	err = filepath.WalkDir(repo.Root, func(path string, entry fs.DirEntry, err error) error {
		relative, _ := filepath.Rel(repo.Root, path)
		relative = filepath.ToSlash(relative)
		if err != nil || !entry.Type().IsRegular() || relative != "package.json" && !strings.HasPrefix(relative, "dist/") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := archive.WriteHeader(&tar.Header{Name: "package/" + relative, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data))}); err != nil {
			return err
		}
		_, err = archive.Write(data)
		return err
	})
	if err = errors.Join(err, archive.Close(), compressed.Close(), file.Close()); err != nil {
		t.Fatal(err)
	}
}

func bundledFinding(findings []policy.Finding, check, path string) bool {
	for _, finding := range findings {
		if finding.Check == check && finding.Path == path {
			return true
		}
	}
	return false
}
