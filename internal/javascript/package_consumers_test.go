package javascript

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type runtimeConsumerCase struct {
	source string
	files  map[string]string
	needs  bool
}

func TestPackageImportsAgreeWithNodeConsumers(t *testing.T) {
	cases := map[string]runtimeConsumerCase{
		"direct require":           {source: `module.exports = require("dep");`, needs: true},
		"member require":           {source: `module.exports = module.require("dep");`, needs: true},
		"require resolve":          {source: `module.exports = require.resolve("dep");`, needs: true},
		"literal dynamic import":   {source: `module.exports = import("dep");`, needs: true},
		"computed require":         {source: `const name = ["d", "e", "p"].join(""); module.exports = require(name);`, needs: true},
		"require alias":            {source: `const load = require; module.exports = load("dep");`, needs: true},
		"require call":             {source: `module.exports = require.call(null, "dep");`, needs: true},
		"member require alias":     {source: `const load = module.require; module.exports = load.call(module, "dep");`, needs: true},
		"element require":          {source: `module.exports = module["require"]("dep");`, needs: true},
		"destructured require":     {source: `const { require: load } = module; module.exports = load.call(module, "dep");`, needs: true},
		"quoted destructuring":     {source: `const { "require": load } = module; module.exports = load.call(module, "dep");`, needs: true},
		"computed destructuring":   {source: "const { [`require`]: load } = module; module.exports = load.call(module, \"dep\");", needs: true},
		"assignment destructuring": {source: `let load; ({ require: load } = module); module.exports = load.call(module, "dep");`, needs: true},
		"nested assignment":        {source: `let load; [{ "require": load }] = [module]; module.exports = load.call(module, "dep");`, needs: true},
		"shorthand escape":         {source: `const loaders = { require }; const pick = (o) => o.require; module.exports = pick(loaders)("dep");`, needs: true},
		"bundler require shim":     {source: `var __require = ((x) => typeof require !== "undefined" ? require : x)(() => { throw new Error("no require"); }); module.exports = __require("dep");`, needs: true},
		"module builtin":           {source: `module.exports = require("node:module").createRequire(__filename)("dep");`, needs: true},
		"builtin getter":           {source: `module.exports = process.getBuiltinModule("module").createRequire(__filename)("dep");`, needs: true},
		"destructured getter":      {source: `const { getBuiltinModule: get } = process; module.exports = get("module").createRequire(__filename)("dep");`, needs: true},
		"quoted getter":            {source: `module.exports = process["getBuiltinModule"]("module").createRequire(__filename)("dep");`, needs: true},
		"loader names as data":     {source: `module.exports = { words: ["require", "getBuiltinModule"], require: 1, "getBuiltinModule": 2, same: "require" === "require" };`},
		"main module check":        {source: `if (require.main === module) { process.exitCode = 0; } module.exports = [typeof require, Object.keys(require.cache).length];`},
		"local resolve":            {source: `module.exports = require.resolve("./local.cjs");`, files: map[string]string{"dist/local.cjs": `module.exports = {};`}},
		"builtin requires":         {source: `module.exports = [require("node:path").sep, process.getBuiltinModule("node:fs").constants];`},
		"method named require":     {source: `class Store { require(key) { return key; } } module.exports = new Store().require("value");`},
	}
	bundle := installedPackageImportBundle(t)
	node, _, err := bundle.paths()
	if err != nil {
		t.Fatal(err)
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			consumer := t.TempDir()
			pkg := filepath.Join(consumer, "node_modules", "pkg")
			writeScript(t, filepath.Join(pkg, "package.json"), `{"main":"dist/index.cjs"}`)
			writeScript(t, filepath.Join(pkg, "dist/index.cjs"), testCase.source)
			for path, source := range testCase.files {
				writeScript(t, filepath.Join(pkg, path), source)
			}
			load := func() (bool, string) {
				command := exec.CommandContext(t.Context(), node, "-e", `require(require("node:path").resolve(process.argv[1]))`, pkg)
				command.Dir = consumer
				command.Env = []string{}
				output, err := command.CombinedOutput()
				return err == nil, string(output)
			}
			loaded, output := load()
			missing := !loaded && (strings.Contains(output, "Cannot find module 'dep'") || strings.Contains(output, "Cannot find package 'dep'"))
			if !loaded && !missing {
				t.Fatalf("fixture failed for another reason: %s", output)
			}
			if missing {
				writeScript(t, filepath.Join(consumer, "node_modules/dep/package.json"), `{"main":"index.js"}`)
				writeScript(t, filepath.Join(consumer, "node_modules/dep/index.js"), `module.exports = {};`)
				if ok, output := load(); !ok {
					t.Fatalf("fixture still fails with dep installed: %s", output)
				}
			}
			if missing != testCase.needs {
				t.Fatalf("Node consumer needs dep = %t, case declares %t: %s", missing, testCase.needs, output)
			}
			result, err := bundle.PackageImports(t.Context(), pkg, "package.json")
			if err != nil {
				t.Fatal(err)
			}
			flagged := len(result.Unsupported) > 0 || slices.Contains(importedPackages(result), "dep")
			if flagged != missing {
				t.Fatalf("verifier flagged dep = %t, Node consumer needs it = %t: %+v", flagged, missing, result)
			}
		})
	}
}

type typeConsumerCase struct {
	declaration string
	files       map[string]string
	identity    string
	install     map[string]string
}

func TestPackageImportsAgreeWithTypeScriptConsumers(t *testing.T) {
	cases := map[string]typeConsumerCase{
		"bare import through package": {
			declaration: `export type { Value } from "dep";`, identity: "dep",
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"bare import through types package": {
			declaration: `export type { Value } from "dep";`, identity: "@types/dep",
			install: map[string]string{"@types/dep/index.d.ts": `export type Value = string;`},
		},
		"scoped subpath through types package": {
			declaration: `export type Value = import("@scope/dep/sub").Value;`, identity: "@types/scope__dep",
			install: map[string]string{"@types/scope__dep/sub.d.ts": `export type Value = string;`},
		},
		"type reference": {
			declaration: "/// <reference types=\"dep\" />\nexport declare const value: Marker;\n", identity: "@types/dep",
			install: map[string]string{"@types/dep/index.d.ts": `interface Marker {}`},
		},
		"prefixed builtin": {
			declaration: `import type { EventEmitter } from "node:events"; export declare const emitter: EventEmitter;`, identity: "@types/node",
			install: map[string]string{"@types/node/index.d.ts": `declare module "node:events" { export class EventEmitter {} }`},
		},
		"bare builtin": {
			declaration: `import type { EventEmitter } from "events"; export declare const emitter: EventEmitter;`, identity: "@types/node",
			install: map[string]string{"@types/node/index.d.ts": `declare module "events" { export class EventEmitter {} }`},
		},
		"relative declaration": {
			declaration: `export type { Value } from "./value.js";`,
			files:       map[string]string{"dist/value.d.ts": `export type Value = string;`},
		},
		"library reference": {
			declaration: "/// <reference lib=\"es2015.promise\" />\nexport declare const value: Promise<string>;\n",
		},
	}
	bundle := installedPackageImportBundle(t)
	node, script, err := bundle.paths()
	if err != nil {
		t.Fatal(err)
	}
	compiler := filepath.Join(filepath.Dir(script), "node_modules", "typescript")
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			consumer := t.TempDir()
			pkg := filepath.Join(consumer, "node_modules", "pkg")
			writeScript(t, filepath.Join(pkg, "package.json"), `{"main":"dist/index.js","types":"dist/index.d.ts"}`)
			writeScript(t, filepath.Join(pkg, "dist/index.js"), `module.exports = {};`)
			writeScript(t, filepath.Join(pkg, "dist/index.d.ts"), testCase.declaration)
			for path, source := range testCase.files {
				writeScript(t, filepath.Join(pkg, path), source)
			}
			writeScript(t, filepath.Join(consumer, "index.ts"), "import type * as pkg from \"pkg\";\nexport type Surface = typeof pkg;\n")
			writeScript(t, filepath.Join(consumer, "compile.cjs"), typeScriptConsumerDriver)
			compile := func() []string {
				command := exec.CommandContext(t.Context(), node, filepath.Join(consumer, "compile.cjs"), compiler, consumer)
				command.Dir = consumer
				command.Env = []string{}
				var stderr bytes.Buffer
				command.Stderr = &stderr
				output, err := command.Output()
				if err != nil {
					t.Fatalf("compile consumer: %v: %s", err, stderr.String())
				}
				diagnostics := []string{}
				if err := json.Unmarshal(output, &diagnostics); err != nil {
					t.Fatal(err)
				}
				return diagnostics
			}
			before := compile()
			if testCase.identity == "" {
				if len(before) != 0 {
					t.Fatalf("neighbor fixture does not compile: %v", before)
				}
			} else {
				if len(before) == 0 {
					t.Fatal("fixture compiles without its type dependency")
				}
				for path, source := range testCase.install {
					writeScript(t, filepath.Join(consumer, "node_modules", path), source)
				}
				if after := compile(); len(after) != 0 {
					t.Fatalf("fixture does not compile with %s installed: %v", testCase.identity, after)
				}
			}
			result, err := bundle.PackageImports(t.Context(), pkg, "package.json")
			if err != nil || len(result.Unsupported) != 0 {
				t.Fatalf("PackageImports = %+v, %v", result, err)
			}
			packages := importedPackages(result)
			if testCase.identity == "" && len(packages) != 0 || testCase.identity != "" && !slices.Contains(packages, testCase.identity) {
				t.Fatalf("verifier identities %v do not match the consumer's need for %q", packages, testCase.identity)
			}
		})
	}
}

const typeScriptConsumerDriver = `const [compiler, project] = process.argv.slice(2);
const ts = require(compiler);
const { existsSync } = require("node:fs");
const nodeTypes = existsSync(require("node:path").join(project, "node_modules", "@types", "node"));
const program = ts.createProgram([require("node:path").join(project, "index.ts")], {
  module: ts.ModuleKind.NodeNext,
  moduleResolution: ts.ModuleResolutionKind.NodeNext,
  lib: ["lib.es5.d.ts"],
  types: nodeTypes ? ["node"] : [],
  noEmit: true,
  strict: true,
  skipLibCheck: false,
});
process.stdout.write(JSON.stringify(ts.getPreEmitDiagnostics(program).map((diagnostic) =>
  diagnostic.code + " " + ts.flattenDiagnosticMessageText(diagnostic.messageText, " "))));
`
