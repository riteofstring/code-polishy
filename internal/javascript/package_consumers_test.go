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
	manifest string
	source   string
	files    map[string]string
	entry    string
	needs    bool
	outcome  string
}

const (
	verifiedImport = "import"
	unsupportedUse = "unsupported"
	cleanArtifact  = "clean"
)

func esmPackage(files map[string]string) runtimeConsumerCase {
	return runtimeConsumerCase{manifest: `{"name":"pkg","exports":"./dist/index.mjs"}`, files: files}
}

func needing(testCase runtimeConsumerCase, outcome string) runtimeConsumerCase {
	testCase.needs = true
	testCase.outcome = outcome
	return testCase
}

func TestPackageImportsAgreeWithNodeConsumers(t *testing.T) {
	cases := map[string]runtimeConsumerCase{
		"call/direct require":           needing(runtimeConsumerCase{source: `module.exports = require("dep");`}, verifiedImport),
		"call/parenthesized require":    needing(runtimeConsumerCase{source: `module.exports = (require)("dep");`}, verifiedImport),
		"call/optional require":         needing(runtimeConsumerCase{source: `module.exports = require?.("dep");`}, verifiedImport),
		"call/member require":           needing(runtimeConsumerCase{source: `module.exports = module.require("dep");`}, verifiedImport),
		"call/parenthesized member":     needing(runtimeConsumerCase{source: `module.exports = (module.require)("dep");`}, verifiedImport),
		"call/quoted member":            needing(runtimeConsumerCase{source: `module.exports = module["require"]("dep");`}, verifiedImport),
		"call/parenthesized quoted key": needing(runtimeConsumerCase{source: `module.exports = module[("require")]("dep");`}, verifiedImport),
		"call/require resolve":          needing(runtimeConsumerCase{source: `module.exports = require.resolve("dep");`}, verifiedImport),
		"call/literal dynamic import":   needing(runtimeConsumerCase{source: `module.exports = import("dep");`}, verifiedImport),

		"unsupported/computed require":         needing(runtimeConsumerCase{source: `const name = ["d", "e", "p"].join(""); module.exports = require(name);`}, unsupportedUse),
		"unsupported/alias":                    needing(runtimeConsumerCase{source: `const load = require; module.exports = load("dep");`}, unsupportedUse),
		"unsupported/call method":              needing(runtimeConsumerCase{source: `module.exports = require.call(null, "dep");`}, unsupportedUse),
		"unsupported/constructed":              needing(runtimeConsumerCase{source: `module.exports = new require("dep");`}, unsupportedUse),
		"unsupported/member alias":             needing(runtimeConsumerCase{source: `const load = module.require; module.exports = load.call(module, "dep");`}, unsupportedUse),
		"unsupported/quoted member alias":      needing(runtimeConsumerCase{source: `const load = module[("require")]; module.exports = load.call(module, "dep");`}, unsupportedUse),
		"unsupported/renamed destructuring":    needing(runtimeConsumerCase{source: `const { require: load } = module; module.exports = load.call(module, "dep");`}, unsupportedUse),
		"unsupported/quoted destructuring":     needing(runtimeConsumerCase{source: `const { "require": load } = module; module.exports = load.call(module, "dep");`}, unsupportedUse),
		"unsupported/computed destructuring":   needing(runtimeConsumerCase{source: "const { [`require`]: load } = module; module.exports = load.call(module, \"dep\");"}, unsupportedUse),
		"unsupported/parenthesized key":        needing(runtimeConsumerCase{source: `const { [("require")]: load } = module; module.exports = load.call(module, "dep");`}, unsupportedUse),
		"unsupported/assignment destructuring": needing(runtimeConsumerCase{source: `let load; ({ require: load } = module); module.exports = load.call(module, "dep");`}, unsupportedUse),
		"unsupported/nested assignment":        needing(runtimeConsumerCase{source: `let load; [{ "require": load }] = [module]; module.exports = load.call(module, "dep");`}, unsupportedUse),
		"unsupported/shorthand value":          needing(runtimeConsumerCase{source: `const loaders = { require }; const pick = (o) => o.require; module.exports = pick(loaders)("dep");`}, unsupportedUse),
		"unsupported/bundler require shim":     needing(runtimeConsumerCase{source: `var __require = ((x) => typeof require !== "undefined" ? require : x)(() => { throw new Error("no require"); }); module.exports = __require("dep");`}, unsupportedUse),
		"unsupported/module builtin":           needing(runtimeConsumerCase{source: `module.exports = require("node:module").createRequire(__filename)("dep");`}, unsupportedUse),
		"unsupported/module getter":            needing(runtimeConsumerCase{source: `module.exports = process.getBuiltinModule("module").createRequire(__filename)("dep");`}, unsupportedUse),
		"unsupported/quoted module getter":     needing(runtimeConsumerCase{source: `module.exports = process["getBuiltinModule"]("module").createRequire(__filename)("dep");`}, unsupportedUse),
		"unsupported/renamed getter":           needing(runtimeConsumerCase{source: `const { getBuiltinModule: get } = process; module.exports = get("module").createRequire(__filename)("dep");`}, unsupportedUse),
		"unsupported/imported module getter": needing(esmPackage(map[string]string{
			"dist/index.mjs": `import { getBuiltinModule } from "node:process"; export default getBuiltinModule("module").createRequire(import.meta.url)("dep");`,
		}), unsupportedUse),
		"unsupported/quoted import alias": needing(esmPackage(map[string]string{
			"dist/index.mjs": `import { "getBuiltinModule" as get } from "node:process"; export default get("module").createRequire(import.meta.url)("dep");`,
		}), unsupportedUse),
		"unsupported/quoted re-export alias": needing(esmPackage(map[string]string{
			"dist/loader.mjs": `export { "getBuiltinModule" as get } from "node:process";`,
			"dist/index.mjs":  `import { get } from "./loader.mjs"; export default get("module").createRequire(import.meta.url)("dep");`,
		}), unsupportedUse),

		"ordinary/loader names as data":      {source: `module.exports = { words: ["require", "getBuiltinModule"], require: 1, "getBuiltinModule": 2, same: "require" === "require" };`},
		"ordinary/wrapped and computed data": {source: `module.exports = { word: ("require"), list: [("getBuiltinModule")], ["require"]: 1 };`},
		"ordinary/require metadata":          {source: `if ((require).main === module) { process.exitCode = 0; } module.exports = [typeof (require), Object.keys(require.cache).length];`},
		"ordinary/local resolve":             {source: `module.exports = require.resolve("./local.cjs");`, files: map[string]string{"dist/local.cjs": `module.exports = {};`}},
		"ordinary/builtin loads":             {source: `module.exports = [require("node:path").sep, process.getBuiltinModule("node:fs").constants];`},
		"ordinary/same-name getter binding":  {source: `const { getBuiltinModule } = process; module.exports = getBuiltinModule("node:fs").constants;`},
		"ordinary/method named require":      {source: `class Store { require(key) { return key; } } module.exports = new Store().require("value");`},
		"ordinary/labels and declarations":   {source: `function load(require) { return typeof require; } require: for (;;) { break require; } module.exports = load;`},
		"ordinary/imported getter": esmPackage(map[string]string{
			"dist/index.mjs": `import { getBuiltinModule } from "node:process"; export default getBuiltinModule("node:fs").constants;`,
		}),

		"conservative/unused alias":       {source: `const keep = require; module.exports = typeof keep;`, outcome: unsupportedUse},
		"conservative/comma callee":       {source: `module.exports = (0, require)("node:path").sep;`, outcome: unsupportedUse},
		"conservative/quoted export name": {manifest: `{"name":"pkg","exports":"./dist/index.mjs"}`, files: map[string]string{"dist/index.mjs": `const value = 1; export { value as "require" };`}, outcome: unsupportedUse},

		"layout/self subpath import": needing(runtimeConsumerCase{source: `module.exports = require("pkg/dist/chunk.cjs");`, files: map[string]string{"dist/chunk.cjs": `module.exports = require("dep");`}}, verifiedImport),
		"layout/self exported subpath": needing(runtimeConsumerCase{
			manifest: `{"name":"pkg","exports":{".":"./dist/index.cjs","./chunk":"./dist/chunk.cjs"}}`,
			source:   `module.exports = require("pkg/chunk");`, files: map[string]string{"dist/chunk.cjs": `module.exports = require("dep");`},
		}, verifiedImport),
		"layout/self harmless subpath": {source: `module.exports = require("pkg/dist/util.cjs");`, files: map[string]string{"dist/util.cjs": `module.exports = {};`}},
		"layout/self manifest read":    {source: `module.exports = require("pkg/package.json").name;`},
		"layout/self bare import":      {source: `module.exports = { load: () => require("pkg") };`},
		"layout/directory package main": needing(runtimeConsumerCase{source: `module.exports = require("./chunk");`, files: map[string]string{
			"dist/chunk/package.json": `{"main":"actual.cjs"}`, "dist/chunk/actual.cjs": `module.exports = require("dep");`, "dist/chunk/index.js": `module.exports = {};`,
		}}, verifiedImport),
		"layout/directory index beside sibling": needing(runtimeConsumerCase{source: `module.exports = require("./chunk");`, files: map[string]string{
			"dist/chunk.cjs": `module.exports = {};`, "dist/chunk/index.js": `module.exports = require("dep");`,
		}}, verifiedImport),
		"layout/json before directory": {source: `module.exports = require("./data");`, files: map[string]string{
			"dist/data.json": `{}`, "dist/data/index.js": `module.exports = require("dep");`,
		}},
		"layout/exact file before extension": {source: `module.exports = require("./tool");`, files: map[string]string{
			"dist/tool": `module.exports = {};`, "dist/tool.js": `module.exports = require("dep");`,
		}},
		"layout/unreferenced artifact": {source: `module.exports = {};`, files: map[string]string{"dist/unused.cjs": `module.exports = require("dep");`}},
		"layout/bin entry": needing(runtimeConsumerCase{
			manifest: `{"name":"pkg","main":"dist/index.cjs","bin":{"tool":"bin/tool"}}`, source: `module.exports = {};`, entry: "bin/tool",
			files: map[string]string{"bin/tool": "#!/usr/bin/env node\nrequire(\"dep\");\n"},
		}, verifiedImport),
		"layout/extensionless main": needing(runtimeConsumerCase{
			manifest: `{"name":"pkg","main":"dist/entry"}`, files: map[string]string{"dist/entry": `module.exports = require("dep");`},
		}, verifiedImport),
		"layout/import condition": needing(runtimeConsumerCase{
			manifest: `{"name":"pkg","exports":{".":{"import":"./dist/index.mjs","require":"./dist/index.cjs"}}}`, source: `module.exports = {};`,
			files: map[string]string{"dist/index.mjs": `export * from "dep";`},
		}, verifiedImport),
		"layout/implicit index": needing(runtimeConsumerCase{
			manifest: `{"name":"pkg"}`, files: map[string]string{"index.js": `module.exports = require("dep");`},
		}, unsupportedUse),

		"build/tsc commonjs":            {source: "\"use strict\";\nvar __importDefault = (this && this.__importDefault) || function (mod) {\n    return (mod && mod.__esModule) ? mod : { \"default\": mod };\n};\nObject.defineProperty(exports, \"__esModule\", { value: true });\nexports.value = void 0;\nconst node_path_1 = __importDefault(require(\"node:path\"));\nexports.value = node_path_1.default.sep;\n"},
		"build/tsc commonjs dependency": needing(runtimeConsumerCase{source: "\"use strict\";\nObject.defineProperty(exports, \"__esModule\", { value: true });\nexports.loaded = void 0;\nconst dep_1 = require(\"dep\");\nexports.loaded = Promise.resolve().then(() => require(\"dep\"));\nexports.value = dep_1;\n"}, verifiedImport),
		"build/esbuild commonjs":        {source: "var __defProp = Object.defineProperty;\nvar __getOwnPropNames = Object.getOwnPropertyNames;\nvar __hasOwnProp = Object.prototype.hasOwnProperty;\nvar __export = (target, all) => {\n  for (var name in all)\n    __defProp(target, name, { get: all[name], enumerable: true });\n};\nvar __copyProps = (to, from) => {\n  for (let key of __getOwnPropNames(from))\n    if (!__hasOwnProp.call(to, key)) __defProp(to, key, { get: () => from[key], enumerable: true });\n  return to;\n};\nvar __toCommonJS = (mod) => __copyProps(__defProp({}, \"__esModule\", { value: true }), mod);\nvar src_exports = {};\n__export(src_exports, { value: () => value });\nmodule.exports = __toCommonJS(src_exports);\nvar import_node_path = require(\"node:path\");\nconst value = import_node_path.sep;\n"},
		"build/esbuild external":        needing(runtimeConsumerCase{source: "var import_dep = require(\"dep\");\nmodule.exports = { value: import_dep };\n"}, verifiedImport),
		"build/rollup commonjs":         needing(runtimeConsumerCase{source: "'use strict';\n\nObject.defineProperty(exports, '__esModule', { value: true });\n\nvar dep = require('dep');\n\nexports.value = dep;\n"}, verifiedImport),
		"build/webpack library":         needing(runtimeConsumerCase{source: "(() => {\n  var __webpack_modules__ = { 1: (module) => { module.exports = require(\"dep\"); } };\n  var __webpack_module_cache__ = {};\n  function __webpack_require__(moduleId) {\n    var cachedModule = __webpack_module_cache__[moduleId];\n    if (cachedModule !== undefined) return cachedModule.exports;\n    var module = (__webpack_module_cache__[moduleId] = { exports: {} });\n    __webpack_modules__[moduleId](module, module.exports, __webpack_require__);\n    return module.exports;\n  }\n  module.exports = __webpack_require__(1);\n})();\n"}, verifiedImport),
		"build/rollup esm":              needing(esmPackage(map[string]string{"dist/index.mjs": "import dep from 'dep';\n\nexport { dep as value };\n"}), verifiedImport),
	}
	bundle := installedPackageImportBundle(t)
	node, _, err := bundle.paths()
	if err != nil {
		t.Fatal(err)
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			pkg, missing := installNodeConsumer(t, node, testCase)
			result, err := bundle.PackageImports(t.Context(), pkg, "package.json")
			if err != nil {
				t.Fatal(err)
			}
			outcome := verifierOutcome(result)
			expected := testCase.outcome
			if expected == "" {
				expected = cleanArtifact
			}
			if outcome != expected || missing && outcome == cleanArtifact {
				t.Fatalf("verifier outcome = %s, want %s (Node consumer needs dep = %t): %+v", outcome, expected, missing, result)
			}
		})
	}
}

func verifierOutcome(result ImportResult) string {
	if len(result.Unsupported) > 0 {
		return unsupportedUse
	}
	if slices.Contains(importedPackages(result), "dep") {
		return verifiedImport
	}
	return cleanArtifact
}

func installNodeConsumer(t *testing.T, node string, testCase runtimeConsumerCase) (string, bool) {
	t.Helper()
	consumer := t.TempDir()
	pkg := filepath.Join(consumer, "node_modules", "pkg")
	manifest := testCase.manifest
	if manifest == "" {
		manifest = `{"name":"pkg","main":"dist/index.cjs"}`
	}
	writeScript(t, filepath.Join(pkg, "package.json"), manifest)
	if testCase.source != "" {
		writeScript(t, filepath.Join(pkg, "dist/index.cjs"), testCase.source)
	}
	for path, source := range testCase.files {
		writeScript(t, filepath.Join(pkg, path), source)
	}
	load := func() (bool, string) {
		command := exec.CommandContext(t.Context(), node, "-e", runtimeConsumerDriver, testCase.entry)
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
	return pkg, missing
}

type typeConsumerCase struct {
	manifest    string
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
		"declaration import selecting TypeScript source": {
			declaration: `export type { Value } from "./other.js";`, identity: "dep",
			files: map[string]string{
				"dist/other.ts":   "import type { Value as Source } from \"dep\";\nexport type Value = Source;\n",
				"dist/other.d.ts": `export type Value = string;`,
			},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"declaration directory with types manifest": {
			declaration: `export type { Value } from "./types";`, identity: "dep",
			files: map[string]string{
				"dist/types/package.json": `{"types":"main.d.ts"}`,
				"dist/types/main.d.ts":    `export type { Value } from "dep";`,
				"dist/types/index.d.ts":   `export type Value = string;`,
			},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"self type import": {
			declaration: `export type { Value } from "pkg/dist/other";`, identity: "dep",
			files:   map[string]string{"dist/other.d.ts": `export type { Value } from "dep";`},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"declaration companion import": {
			declaration: `export type { Value } from "./other.js";`,
			files:       map[string]string{"dist/other.d.ts": `export type Value = string;`},
		},
		"exports types condition": {
			manifest: `{"name":"pkg","exports":{".":{"types":"./dist/types.d.ts","default":"./dist/index.js"}}}`, identity: "dep",
			files:   map[string]string{"dist/types.d.ts": `export type { Value } from "dep";`},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"main declaration companion": {
			manifest: `{"name":"pkg","main":"dist/index.js"}`, declaration: `export type { Value } from "dep";`, identity: "dep",
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"implicit root declaration": {
			manifest: `{"name":"pkg","main":"dist/index.js"}`, identity: "dep",
			files:   map[string]string{"index.d.ts": `export type { Value } from "dep";`},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"exports entry selecting TypeScript source": {
			manifest: `{"name":"pkg","exports":"./dist/index.js"}`, identity: "dep",
			files:   map[string]string{"dist/index.ts": "import type { Value as Source } from \"dep\";\nexport type Value = Source;\n"},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"main entry selecting TypeScript source": {
			manifest: `{"name":"pkg","main":"dist/index.js"}`, declaration: `export type Value = string;`, identity: "dep",
			files:   map[string]string{"dist/index.ts": "import type { Value as Source } from \"dep\";\nexport type Value = Source;\n"},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"extensionless main declaration": {
			manifest: `{"name":"pkg","main":"dist/entry"}`, identity: "dep",
			files:   map[string]string{"dist/entry": `module.exports = {};`, "dist/entry.d.ts": `export type { Value } from "dep";`},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"unreachable private declaration": {
			declaration: `export declare const value: string;`,
			files:       map[string]string{"dist/private.d.ts": `export type { Value } from "dep";`},
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
			manifest := testCase.manifest
			if manifest == "" {
				manifest = `{"name":"pkg","main":"dist/index.js","types":"dist/index.d.ts"}`
			}
			writeScript(t, filepath.Join(pkg, "package.json"), manifest)
			writeScript(t, filepath.Join(pkg, "dist/index.js"), `module.exports = {};`)
			if testCase.declaration != "" {
				writeScript(t, filepath.Join(pkg, "dist/index.d.ts"), testCase.declaration)
			}
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

const runtimeConsumerDriver = `const entry = process.argv[1];
if (entry) {
  require(require("node:path").resolve("node_modules/pkg", entry));
} else {
  require("pkg");
  import("pkg").catch((error) => {
    console.error(String(error));
    process.exitCode = 1;
  });
}
`

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
