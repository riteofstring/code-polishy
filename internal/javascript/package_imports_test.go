package javascript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestInstalledPackageImportsFollowRuntimeAndPublicTypes(t *testing.T) {
	root := t.TempDir()
	writeScript(t, filepath.Join(root, "package.json"), `{"exports":{".":{"import":"./dist/index.js","types":"./dist/index.d.ts"},"./style":"./dist/style.css"}}`)
	writeScript(t, filepath.Join(root, "dist/index.js"), `export { value } from "./chunk.js";`)
	writeScript(t, filepath.Join(root, "dist/chunk.js"), `const value = require("@editor/view/subpath"); export { value };`)
	writeScript(t, filepath.Join(root, "dist/index.d.ts"), `export type { Value } from "./types.js";`)
	writeScript(t, filepath.Join(root, "dist/types.d.ts"), `export type Value = import("@editor/state").Value;`)
	writeScript(t, filepath.Join(root, "dist/private.d.ts"), `export type Private = import("unused-private").Value;`)
	writeScript(t, filepath.Join(root, "dist/style.css"), "body {}")
	bundle := installedPackageImportBundle(t)
	result, err := bundle.PackageImports(t.Context(), root, "package.json")
	if err != nil || len(result.Unsupported) != 0 {
		t.Fatalf("PackageImports = %+v, %v", result, err)
	}
	packages := []string{}
	for _, fact := range result.Imports {
		if fact.Package != "" {
			packages = append(packages, fact.Package)
		}
	}
	slices.Sort(packages)
	if !slices.Equal(packages, []string{"@editor/state", "@editor/view", "@types/editor__state"}) || slices.Contains(result.Analyzed, "dist/private.d.ts") {
		t.Fatalf("published imports = %+v", result)
	}
}

func TestInstalledPackageImportsRejectIncompleteArtifacts(t *testing.T) {
	bundle := installedPackageImportBundle(t)
	for name, source := range map[string]string{
		"missing chunk":              `export * from "./missing.js";`,
		"computed import":            `export const load = (name) => import(name);`,
		"computed require":           `export const load = (name) => require(name);`,
		"invalid syntax":             `export const =`,
		"unresolved imports mapping": `export * from "#internal";`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeScript(t, filepath.Join(root, "package.json"), `{"exports":"./dist/index.js"}`)
			writeScript(t, filepath.Join(root, "dist/index.js"), source)
			result, err := bundle.PackageImports(t.Context(), root, "package.json")
			if err != nil || len(result.Unsupported) != 1 || result.Unsupported[0].Path != "dist/index.js" {
				t.Fatalf("PackageImports = %+v, %v", result, err)
			}
		})
	}
}

func TestInstalledPackageImportsCheckImplicitTypesAndReferences(t *testing.T) {
	root := t.TempDir()
	writeScript(t, filepath.Join(root, "package.json"), `{"main":"dist/index.cjs"}`)
	writeScript(t, filepath.Join(root, "dist/index.cjs"), `module.exports = {};`)
	writeScript(t, filepath.Join(root, "dist/index.d.cts"), "/// <reference path=\"./extra.d.ts\" />\nexport {};\n")
	writeScript(t, filepath.Join(root, "dist/extra.d.ts"), "/// <reference types=\"editor\" />\nexport {};\n")
	result, err := installedPackageImportBundle(t).PackageImports(t.Context(), root, "package.json")
	if err != nil || len(result.Unsupported) != 0 || !slices.Equal(importedPackages(result), []string{"@types/editor", "editor"}) {
		t.Fatalf("PackageImports = %+v, %v", result, err)
	}
}

func importedPackages(result ImportResult) []string {
	packages := []string{}
	for _, fact := range result.Imports {
		if fact.Package != "" {
			packages = append(packages, fact.Package)
		}
	}
	slices.Sort(packages)
	return packages
}

func TestInstalledPackageImportsFollowExecutableAndImplicitEntries(t *testing.T) {
	root := t.TempDir()
	writeScript(t, filepath.Join(root, "package.json"), `{"main":"dist/index","bin":{"tool":"bin/tool"}}`)
	writeScript(t, filepath.Join(root, "dist/index"), `module.exports = require("runtime-dep");`)
	writeScript(t, filepath.Join(root, "bin/tool"), "#!/usr/bin/env node\nrequire(\"cli-dep\");\n")
	writeScript(t, filepath.Join(root, "index.d.ts"), `export type { Value } from "type-dep";`)
	result, err := installedPackageImportBundle(t).PackageImports(t.Context(), root, "package.json")
	if err != nil || len(result.Unsupported) != 0 {
		t.Fatalf("PackageImports = %+v, %v", result, err)
	}
	if !slices.Equal(importedPackages(result), []string{"@types/type-dep", "cli-dep", "runtime-dep", "type-dep"}) {
		t.Fatalf("published imports = %+v", result)
	}
}

func TestInstalledPackageImportsResolveExtensionlessRequiresLikeNode(t *testing.T) {
	root := t.TempDir()
	writeScript(t, filepath.Join(root, "package.json"), `{"main":"dist/index.cjs"}`)
	writeScript(t, filepath.Join(root, "dist/index.cjs"), `module.exports = require("./chunk");`)
	writeScript(t, filepath.Join(root, "dist/chunk.cjs"), `module.exports = {};`)
	writeScript(t, filepath.Join(root, "dist/chunk/index.js"), `module.exports = require("dep");`)
	result, err := installedPackageImportBundle(t).PackageImports(t.Context(), root, "package.json")
	if err != nil || len(result.Unsupported) != 0 || !slices.ContainsFunc(result.Imports, func(fact ImportFact) bool { return fact.Package == "dep" }) {
		t.Fatalf("PackageImports = %+v, %v", result, err)
	}
}

func TestInstalledPackageImportsRejectUnmodeledRuntimeTargets(t *testing.T) {
	bundle := installedPackageImportBundle(t)
	for name, files := range map[string]map[string]string{
		"nested package manifest": {
			"dist/index.cjs":          `module.exports = require("./chunk");`,
			"dist/chunk/package.json": `{"main":"actual.cjs"}`,
			"dist/chunk/actual.cjs":   `module.exports = require("dep");`,
			"dist/chunk/index.js":     `module.exports = {};`,
		},
		"unknown artifact type": {
			"dist/index.cjs": `module.exports = require("./data.txt");`,
			"dist/data.txt":  `require("dep");`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeScript(t, filepath.Join(root, "package.json"), `{"main":"dist/index.cjs"}`)
			for path, source := range files {
				writeScript(t, filepath.Join(root, path), source)
			}
			result, err := bundle.PackageImports(t.Context(), root, "package.json")
			if err != nil || len(result.Unsupported) != 1 {
				t.Fatalf("PackageImports = %+v, %v", result, err)
			}
		})
	}
}

func TestInstalledPackageImportsRequireBuiltContainedEntries(t *testing.T) {
	bundle := installedPackageImportBundle(t)
	for name, manifest := range map[string]string{
		"missing build":     `{"exports":"./dist/missing.js"}`,
		"no exports":        `{"name":"app"}`,
		"globbed exports":   `{"exports":{"./*":"./dist/*.js"}}`,
		"escaping exports":  `{"exports":"../outside.js"}`,
		"unmodeled types":   `{"exports":"./dist/index.js","typesVersions":{"*":{}}}`,
		"unmodeled browser": `{"exports":"./dist/index.js","browser":{}}`,
		"unmodeled bin dir": `{"exports":"./dist/index.js","directories":{"bin":"bin"}}`,
		"unreadable bin":    `{"bin":7}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeScript(t, filepath.Join(root, "package.json"), manifest)
			result, err := bundle.PackageImports(t.Context(), root, "package.json")
			if err != nil || len(result.Unsupported) != 1 || result.Unsupported[0].Path != "package.json" {
				t.Fatalf("PackageImports = %+v, %v", result, err)
			}
		})
	}
	root := t.TempDir()
	writeScript(t, filepath.Join(root, "package.json"), `{"exports":"./index.js"}`)
	outside := filepath.Join(t.TempDir(), "index.js")
	writeScript(t, outside, "export {};")
	if err := os.Symlink(outside, filepath.Join(root, "index.js")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := bundle.PackageImports(t.Context(), root, "package.json")
	if err != nil || len(result.Unsupported) == 0 {
		t.Fatalf("escaping artifact accepted: %+v, %v", result, err)
	}
	writeScript(t, filepath.Join(root, "package.json"), `{"exports":"./entry.js"}`)
	writeScript(t, filepath.Join(root, "entry.js"), `export * from "./index.js";`)
	writeScript(t, filepath.Join(root, "index.js.js"), "export {};\n")
	result, err = bundle.PackageImports(t.Context(), root, "package.json")
	if err != nil || len(result.Unsupported) == 0 {
		t.Fatalf("escaping import fell back to another artifact: %+v, %v", result, err)
	}
}

func TestPackageImportsRejectMissingAndEscapingCoverage(t *testing.T) {
	for _, result := range []string{
		`{}`,
		`{"analyzed":["packages/app/package.json"],"imports":[],"unsupported":[]}`,
		`{"analyzed":["packages/app/package.json","elsewhere/index.js"],"imports":[],"unsupported":[]}`,
		`{"analyzed":["packages/app/package.json","packages/app/index.js"],"unsupported":[]}`,
		`{"analyzed":["packages/app/package.json","packages/app/index.js"],"imports":[{"path":"packages/app/index.js","line":1,"column":1,"specifier":"./missing.js","resolved":"packages/app/missing.js","package":"","kind":"runtime"}],"unsupported":[]}`,
	} {
		if _, err := decodePackageImports(json.RawMessage(result), "packages/app/package.json"); err == nil {
			t.Fatalf("accepted incomplete package imports: %s", result)
		}
	}
	result, err := decodePackageImports(json.RawMessage(`{"analyzed":[],"imports":[],"unsupported":[{"path":"package.json","reason":"missing build"}]}`), "package.json")
	if err != nil || !strings.Contains(result.Unsupported[0].Reason, "missing build") {
		t.Fatalf("missing build diagnostic = %+v, %v", result, err)
	}
}

func installedPackageImportBundle(t *testing.T) Bundle {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return Bundle{PolicyRoot: root}
}
