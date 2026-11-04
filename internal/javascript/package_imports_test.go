package javascript

import (
	"archive/tar"
	"encoding/json"
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
	result, err := packedDirectoryImports(t, bundle, root)
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
			result, err := packedDirectoryImports(t, bundle, root)
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
	result, err := packedDirectoryImports(t, installedPackageImportBundle(t), root)
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
	result, err := packedDirectoryImports(t, installedPackageImportBundle(t), root)
	if err != nil || len(result.Unsupported) != 0 {
		t.Fatalf("PackageImports = %+v, %v", result, err)
	}
	if !slices.Equal(importedPackages(result), []string{"@types/type-dep", "cli-dep", "runtime-dep", "type-dep"}) {
		t.Fatalf("published imports = %+v", result)
	}
}

func TestInstalledPackageImportsRejectUnexportedSelfImports(t *testing.T) {
	root := t.TempDir()
	writeScript(t, filepath.Join(root, "package.json"), `{"name":"pkg","exports":"./dist/index.cjs"}`)
	writeScript(t, filepath.Join(root, "dist/index.cjs"), `module.exports = require("pkg/dist/chunk.cjs");`)
	writeScript(t, filepath.Join(root, "dist/chunk.cjs"), `module.exports = require("dep");`)
	result, err := packedDirectoryImports(t, installedPackageImportBundle(t), root)
	if err != nil || len(result.Unsupported) != 1 || result.Unsupported[0].Path != "dist/index.cjs" {
		t.Fatalf("PackageImports = %+v, %v", result, err)
	}
}

func TestInstalledPackageImportsRejectUnreadableRuntimeTargets(t *testing.T) {
	bundle := installedPackageImportBundle(t)
	for name, files := range map[string]map[string]string{
		"unknown artifact type": {
			"dist/index.cjs": `module.exports = require("./data.txt");`,
			"dist/data.txt":  `require("dep");`,
		},
		"encoded separator import": {
			"dist/index.cjs":     `module.exports = import("./sub%2fchunk.cjs");`,
			"dist/sub/chunk.cjs": `module.exports = require("dep");`,
		},
		"unreadable package scope": {
			"dist/index.cjs":        `module.exports = require("./sub/inner.cjs");`,
			"dist/sub/package.json": `{`,
			"dist/sub/inner.cjs":    `module.exports = require("pkg");`,
		},
		"directory import": {
			"dist/index.cjs":     `module.exports = import("./sub");`,
			"dist/sub/index.js":  `module.exports = require("dep");`,
			"dist/sub/chunk.cjs": `module.exports = {};`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeScript(t, filepath.Join(root, "package.json"), `{"main":"dist/index.cjs"}`)
			for path, source := range files {
				writeScript(t, filepath.Join(root, path), source)
			}
			result, err := packedDirectoryImports(t, bundle, root)
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
			result, err := packedDirectoryImports(t, bundle, root)
			if err != nil || len(result.Unsupported) != 1 || result.Unsupported[0].Path != "package.json" {
				t.Fatalf("PackageImports = %+v, %v", result, err)
			}
		})
	}
	root := t.TempDir()
	writeScript(t, filepath.Join(root, "package.json"), `{"exports":"./entry.js"}`)
	writeScript(t, filepath.Join(root, "entry.js"), `export * from "./index.js";`)
	writeScript(t, filepath.Join(root, "index.js.js"), "export {};\n")
	result, err := packedDirectoryImports(t, bundle, root)
	if err != nil || len(result.Unsupported) == 0 {
		t.Fatalf("escaping import fell back to another artifact: %+v, %v", result, err)
	}
}

func TestPackedImportsRejectUnpublishableArchives(t *testing.T) {
	bundle := installedPackageImportBundle(t)
	manifest := archiveEntry{name: "package/package.json", typeflag: tar.TypeReg, content: `{"name":"pkg","main":"index.js"}`}
	entry := archiveEntry{name: "package/index.js", typeflag: tar.TypeReg, content: `module.exports = {};`}
	for name, entries := range map[string][]archiveEntry{
		"symbolic link":   {manifest, {name: "package/index.js", typeflag: tar.TypeSymlink, link: "/etc/passwd"}},
		"hard link":       {manifest, {name: "package/index.js", typeflag: tar.TypeLink, link: "package/package.json"}},
		"escaping entry":  {manifest, entry, {name: "package/../outside.js", typeflag: tar.TypeReg, content: "x"}},
		"unprefixed":      {manifest, {name: "index.js", typeflag: tar.TypeReg, content: "x"}},
		"duplicate entry": {manifest, entry, entry},
		"other package":   {{name: "package/package.json", typeflag: tar.TypeReg, content: `{"name":"other","main":"index.js"}`}, entry},
		"no manifest":     {entry},
	} {
		t.Run(name, func(t *testing.T) {
			if result, err := bundle.PackedImports(t.Context(), writeArchive(t, entries), "pkg"); err == nil {
				t.Fatalf("PackedImports accepted the archive: %+v", result)
			}
		})
	}
	if result, err := bundle.PackedImports(t.Context(), writeArchive(t, []archiveEntry{{name: "package/", typeflag: tar.TypeDir}, manifest, entry}), "pkg"); err != nil || len(result.Unsupported) != 0 {
		t.Fatalf("PackedImports = %+v, %v", result, err)
	}
}

func TestPackageImportsRejectMissingAndEscapingCoverage(t *testing.T) {
	for _, result := range []string{
		`{}`,
		`{"analyzed":["package.json"],"imports":[],"unsupported":[]}`,
		`{"analyzed":["package.json","../elsewhere/index.js"],"imports":[],"unsupported":[]}`,
		`{"analyzed":["package.json","index.js"],"unsupported":[]}`,
		`{"analyzed":["package.json","index.js"],"imports":[{"path":"index.js","line":1,"column":1,"specifier":"./missing.js","resolved":"missing.js","package":"","kind":"runtime"}],"unsupported":[]}`,
	} {
		if _, err := decodePackageImports(json.RawMessage(result)); err == nil {
			t.Fatalf("accepted incomplete package imports: %s", result)
		}
	}
	result, err := decodePackageImports(json.RawMessage(`{"analyzed":[],"imports":[],"unsupported":[{"path":"package.json","reason":"missing build"}]}`))
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
