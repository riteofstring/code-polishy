package javascript

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type runtimeConsumerCase struct {
	manifest    string
	source      string
	files       map[string]string
	links       map[string]string
	entry       string
	needs       bool
	unpublished bool
	outcome     string
}

const (
	verifiedImport = "import"
	unsupportedUse = "unsupported"
	cleanArtifact  = "clean"
)

func esmPackage(files map[string]string) runtimeConsumerCase {
	return runtimeConsumerCase{manifest: `{"name":"pkg","exports":"./dist/index.mjs"}`, files: files}
}

func unpublished(testCase runtimeConsumerCase) runtimeConsumerCase {
	testCase.unpublished = true
	testCase.outcome = unsupportedUse
	return testCase
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

		"unsupported/computed require": needing(runtimeConsumerCase{source: `const name = ["d", "e", "p"].join(""); module.exports = require(name);`}, unsupportedUse),
		"unsupported/require resolve options": {source: `module.exports = require.resolve("./target.cjs", { paths: [require("node:path").join(__dirname, "..")] });`, files: map[string]string{
			"target.cjs": `module.exports = require("dep");`, "dist/target.cjs": `module.exports = {};`,
		}, outcome: unsupportedUse},
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
		"unsupported/destructuring default":    needing(runtimeConsumerCase{source: `const { load = require } = {}; module.exports = load("dep");`}, unsupportedUse),
		"unsupported/array default":            needing(runtimeConsumerCase{source: `const [load = require] = []; module.exports = load("dep");`}, unsupportedUse),
		"unsupported/assignment default":       needing(runtimeConsumerCase{source: `let load; ({ load = require } = {}); module.exports = load("dep");`}, unsupportedUse),
		"unsupported/array assignment default": needing(runtimeConsumerCase{source: `let load; [load = require] = []; module.exports = load("dep");`}, unsupportedUse),
		"unsupported/parameter default":        needing(runtimeConsumerCase{source: `module.exports = ((load = require) => load("dep"))();`}, unsupportedUse),
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
		"ordinary/defaults with loader data": {source: `const { word = "require" } = {}; const [name = ("getBuiltinModule")] = []; module.exports = [word, name];`},
		"ordinary/labels and declarations":   {source: `function load(require) { return typeof require; } require: for (;;) { break require; } module.exports = load;`},
		"ordinary/imported getter": esmPackage(map[string]string{
			"dist/index.mjs": `import { getBuiltinModule } from "node:process"; export default getBuiltinModule("node:fs").constants;`,
		}),

		"conservative/method named require": {source: `class Store { require(key) { return key; } } module.exports = new Store().require("value");`, outcome: unsupportedUse},
		"conservative/unused alias":         {source: `const keep = require; module.exports = typeof keep;`, outcome: unsupportedUse},
		"conservative/comma callee":         {source: `module.exports = (0, require)("node:path").sep;`, outcome: unsupportedUse},
		"conservative/quoted export name":   {manifest: `{"name":"pkg","exports":"./dist/index.mjs"}`, files: map[string]string{"dist/index.mjs": `const value = 1; export { value as "require" };`}, outcome: unsupportedUse},

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
		"layout/percent encoded export":      needing(runtimeConsumerCase{manifest: `{"name":"pkg","exports":"./dist/%69ndex.cjs"}`, source: `module.exports = require("dep");`, files: map[string]string{"dist/%69ndex.cjs": `module.exports = {};`}}, unsupportedUse),
		"packed/inlined dependency":          {manifest: `{"name":"pkg","main":"dist/index.cjs","devDependencies":{"dep":"1.0.0"}}`, source: `module.exports = { value: "inlined" };`},
		"packed/surviving dependency import": needing(runtimeConsumerCase{manifest: `{"name":"pkg","main":"dist/index.cjs","devDependencies":{"dep":"1.0.0"}}`, source: `module.exports = require("dep");`}, verifiedImport),
		"packed/published manifest":          needing(runtimeConsumerCase{manifest: `{"name":"pkg","main":"dist/index.cjs","publishConfig":{"main":"dist/published.cjs"}}`, source: `module.exports = {};`, files: map[string]string{"dist/published.cjs": `module.exports = require("dep");`}}, verifiedImport),
		"packed/checkout node_modules":       unpublished(runtimeConsumerCase{source: `module.exports = require("../node_modules/dep/index.js");`, files: map[string]string{"node_modules/dep/index.js": `module.exports = {};`}}),
		"packed/omitted symlink":             unpublished(runtimeConsumerCase{source: `module.exports = require("./link.cjs");`, files: map[string]string{"dist/real.cjs": `module.exports = {};`}, links: map[string]string{"dist/link.cjs": "real.cjs"}}),
		"packed/files exclusion":             unpublished(runtimeConsumerCase{manifest: `{"name":"pkg","main":"dist/index.cjs","files":["dist/index.cjs"]}`, source: `module.exports = require("./chunk.cjs");`, files: map[string]string{"dist/chunk.cjs": `module.exports = {};`}}),
		"packed/ignore file exclusion":       unpublished(runtimeConsumerCase{source: `module.exports = require("./chunk.cjs");`, files: map[string]string{"dist/chunk.cjs": `module.exports = {};`, ".npmignore": "dist/chunk.cjs\n"}}),
		"layout/nested scope self import": needing(runtimeConsumerCase{source: `module.exports = require("./sub/inner.cjs");`, files: map[string]string{
			"dist/sub/package.json": `{"name":"pkg","exports":"./other.cjs"}`, "dist/sub/inner.cjs": `module.exports = require("pkg");`, "dist/sub/other.cjs": `module.exports = require("dep");`,
		}}, verifiedImport),
		"layout/nested scope other name": needing(runtimeConsumerCase{source: `module.exports = require("./sub/inner.cjs");`, files: map[string]string{
			"dist/sub/package.json": `{"name":"local-name","exports":{".":{"require":"./other.cjs","types":"./other.d.ts"}}}`, "dist/sub/inner.cjs": `module.exports = require("local-name");`, "dist/sub/other.cjs": `module.exports = require("dep");`,
		}}, verifiedImport),
		"layout/nested scope esm self import": needing(esmPackage(map[string]string{
			"dist/index.mjs": `import "./sub/inner.mjs";`, "dist/sub/inner.mjs": `import "pkg/other";`,
			"dist/sub/package.json": `{"name":"pkg","exports":{"./other":"./other.cjs"}}`, "dist/sub/other.cjs": `module.exports = require("dep");`,
		}), verifiedImport),
		"layout/nested scope without exports": {source: `module.exports = require("./sub/inner.cjs");`, files: map[string]string{
			"dist/sub/package.json": `{"name":"pkg","main":"other.cjs"}`, "dist/sub/inner.cjs": `module.exports = require("pkg");`, "dist/sub/other.cjs": `module.exports = require("dep");`,
		}},
		"layout/nested scope clean neighbour": {source: `module.exports = require("./sub/inner.cjs");`, files: map[string]string{
			"dist/sub/package.json": `{"name":"pkg","exports":"./other.cjs"}`, "dist/sub/inner.cjs": `module.exports = require("pkg");`, "dist/sub/other.cjs": `module.exports = {};`,
			"dist/sub/unused.cjs": `module.exports = require("dep");`,
		}},
		"layout/nested scope esm without exports": esmPackage(map[string]string{
			"dist/index.mjs": `import "./sub/inner.mjs";`, "dist/sub/inner.mjs": `import "pkg";`,
			"dist/sub/package.json": `{"name":"pkg","main":"other.cjs"}`, "dist/sub/other.cjs": `module.exports = require("dep");`,
		}),
		"layout/esm self subpath without exports": needing(runtimeConsumerCase{manifest: `{"name":"pkg","main":"dist/index.mjs"}`, files: map[string]string{
			"dist/index.mjs": `import "pkg/dist/chunk.cjs";`, "dist/chunk.cjs": `module.exports = require("dep");`,
		}}, verifiedImport),
		"layout/self import through require condition": needing(runtimeConsumerCase{
			manifest: `{"name":"pkg","exports":{".":{"import":"./dist/index.mjs","require":"./dist/index.cjs"},"./tool":{"import":"./dist/tool.mjs","require":"./dist/tool.cjs"}}}`,
			source:   `module.exports = require("pkg/tool");`,
			files:    map[string]string{"dist/index.mjs": `export {};`, "dist/tool.mjs": `export {};`, "dist/tool.cjs": `module.exports = require("dep");`},
		}, verifiedImport),
		"layout/main extension search": needing(runtimeConsumerCase{manifest: `{"name":"pkg","main":"dist/entry"}`, files: map[string]string{
			"dist/entry.js": `module.exports = require("dep");`,
		}}, verifiedImport),
		"layout/clean main extension search": {manifest: `{"name":"pkg","main":"dist/entry"}`, files: map[string]string{"dist/entry.js": `module.exports = {};`}},
		"layout/directory main": needing(runtimeConsumerCase{manifest: `{"name":"pkg","main":"lib"}`, files: map[string]string{
			"lib/index.js": `module.exports = require("./chunk.js");`, "lib/chunk.js": `module.exports = require("dep");`,
		}}, verifiedImport),
		"layout/clean directory main": {manifest: `{"name":"pkg","main":"lib"}`, files: map[string]string{
			"lib/index.js": `module.exports = {};`, "lib/package.json": `{"main":"entry.js"}`, "lib/entry.js": `module.exports = require("dep");`,
		}},
		"layout/broken nested self import": unpublished(runtimeConsumerCase{source: `module.exports = require("./sub/inner.cjs");`, files: map[string]string{
			"dist/sub/package.json": `{"name":"local-name","exports":"./missing.cjs"}`, "dist/sub/inner.cjs": `module.exports = require("local-name");`,
		}}),
		"layout/broken nested esm self import": unpublished(esmPackage(map[string]string{
			"dist/index.mjs": `import "./sub/inner.mjs";`, "dist/sub/inner.mjs": `import "local-name";`,
			"dist/sub/package.json": `{"name":"local-name","exports":"./missing.cjs"}`,
		})),
		"layout/missing self subpath":  unpublished(runtimeConsumerCase{source: `module.exports = require("pkg/dist/missing.cjs");`}),
		"layout/missing esm target":    unpublished(esmPackage(map[string]string{"dist/index.mjs": `import "./missing.mjs";`})),
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
	for name, testCase := range referenceMatrix() {
		cases[name] = testCase
	}
	bundle := installedPackageImportBundle(t)
	node, _, err := bundle.paths()
	if err != nil {
		t.Fatal(err)
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			archive, missing := installNodeConsumer(t, node, testCase)
			result, err := bundle.PackedImports(t.Context(), archive, "pkg")
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

type referenceForm struct {
	esm      bool
	executes bool
	urls     bool
	source   func(string) string
}

type specifierCase struct {
	specifier string
	local     bool
	external  bool
	urlOnly   bool
	decoy     bool
	outcome   string
}

func referenceMatrix() map[string]runtimeConsumerCase {
	forms := map[string]referenceForm{
		"static import":   {esm: true, executes: true, urls: true, source: func(s string) string { return "import " + strconv.Quote(s) + ";" }},
		"re-export":       {esm: true, executes: true, urls: true, source: func(s string) string { return "export * from " + strconv.Quote(s) + ";" }},
		"dynamic import":  {executes: true, urls: true, source: func(s string) string { return "module.exports = import(" + strconv.Quote(s) + ");" }},
		"require":         {executes: true, source: func(s string) string { return "module.exports = require(" + strconv.Quote(s) + ");" }},
		"module require":  {executes: true, source: func(s string) string { return "module.exports = module.require(" + strconv.Quote(s) + ");" }},
		"require resolve": {source: func(s string) string { return "module.exports = require.resolve(" + strconv.Quote(s) + ");" }},
	}
	specifiers := map[string]specifierCase{
		"relative needing dep": {specifier: "./target.cjs", local: true, outcome: verifiedImport},
		"relative clean":       {specifier: "./clean.cjs", outcome: cleanArtifact},
		"self needing dep":     {specifier: "pkg/dist/target.cjs", local: true, outcome: verifiedImport},
		"external":             {specifier: "dep", external: true, outcome: verifiedImport},
		"builtin":              {specifier: "node:path", outcome: cleanArtifact},
		"absolute path":        {specifier: "{{PKG}}/dist/target.cjs", local: true, outcome: unsupportedUse},
		"file url":             {specifier: "file://{{PKG}}/dist/target.cjs", local: true, urlOnly: true, outcome: unsupportedUse},
		"percent encoded":      {specifier: "./%74arget.cjs", local: true, decoy: true, outcome: verifiedImport},
		"query":                {specifier: "./target.cjs?v=1", local: true, urlOnly: true, outcome: verifiedImport},
		"fragment":             {specifier: "./target.cjs#v", local: true, urlOnly: true, outcome: verifiedImport},
		"backslash":            {specifier: `./sub\..\target.cjs`, local: true, urlOnly: true, outcome: verifiedImport},
	}
	cases := map[string]runtimeConsumerCase{}
	for formName, form := range forms {
		for specifierName, specifier := range specifiers {
			if specifier.urlOnly && !form.urls {
				continue
			}
			testCase := runtimeConsumerCase{
				manifest: `{"name":"pkg","main":"dist/index.cjs"}`,
				files: map[string]string{
					"dist/target.cjs":   `module.exports = require("dep");`,
					"dist/clean.cjs":    `module.exports = {};`,
					"dist/%74arget.cjs": `module.exports = {};`,
				},
				outcome: specifier.outcome,
				needs:   specifier.external || specifier.local && form.executes,
			}
			if specifier.decoy && !form.urls {
				testCase.outcome, testCase.needs = cleanArtifact, false
			}
			if form.esm {
				testCase.manifest = `{"name":"pkg","main":"dist/index.mjs"}`
				testCase.files["dist/index.mjs"] = form.source(specifier.specifier)
			} else {
				testCase.source = form.source(specifier.specifier)
			}
			cases["matrix/"+formName+"/"+specifierName] = testCase
		}
	}
	cases["origin/nested relative require"] = needing(runtimeConsumerCase{source: `module.exports = require("./sub/inner.cjs");`, files: map[string]string{
		"dist/sub/inner.cjs": `module.exports = require("../target.cjs");`, "dist/target.cjs": `module.exports = require("dep");`, "target.cjs": `module.exports = {};`,
	}}, verifiedImport)
	nested := func(inner string) runtimeConsumerCase {
		return needing(runtimeConsumerCase{source: `module.exports = require("./sub/inner.cjs");`, files: map[string]string{
			"dist/sub/inner.cjs": inner, "dist/chunk.cjs": `module.exports = require("dep");`, "dist/sub/chunk.cjs": `module.exports = {};`,
		}}, unsupportedUse)
	}
	cases["origin/shadowed module receiver"] = nested(`module.exports = ((module) => module.require("./chunk.cjs"))(module.parent);`)
	cases["origin/reassigned module"] = nested(`const original = module; module = module.parent; original.exports = module.require("./chunk.cjs");`)
	cases["origin/with scope"] = nested(`with (module.parent) { module.exports = require("./chunk.cjs"); }`)
	cases["origin/locally bound require"] = runtimeConsumerCase{source: `{ const { require } = module; module.exports = require("node:path").sep; }`, outcome: unsupportedUse}
	cases["origin/parent receiver"] = needing(runtimeConsumerCase{source: `module.exports = require("./sub/inner.cjs");`, files: map[string]string{
		"dist/sub/inner.cjs": `module.exports = module.parent.require("./chunk.cjs");`, "dist/chunk.cjs": `module.exports = require("dep");`, "dist/sub/chunk.cjs": `module.exports = {};`,
	}}, unsupportedUse)
	return cases
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
	files := map[string]string{"package.json": testCase.manifest}
	if testCase.manifest == "" {
		files["package.json"] = `{"name":"pkg","main":"dist/index.cjs"}`
	}
	for path, source := range testCase.files {
		files[path] = strings.ReplaceAll(source, "{{PKG}}", pkg)
	}
	if testCase.source != "" {
		files["dist/index.cjs"] = strings.ReplaceAll(testCase.source, "{{PKG}}", pkg)
	}
	archive := packWithPackageManager(t, node, packedFixture{files: files, links: testCase.links})
	installPacked(t, archive, pkg)
	load := func() (bool, string) {
		command := exec.CommandContext(t.Context(), node, "-e", runtimeConsumerDriver, testCase.entry)
		command.Dir = consumer
		command.Env = []string{}
		output, err := command.CombinedOutput()
		return err == nil, string(output)
	}
	loaded, output := load()
	missing := !loaded && (strings.Contains(output, "Cannot find module 'dep'") || strings.Contains(output, "Cannot find package 'dep'"))
	if testCase.unpublished {
		if loaded || missing || !strings.Contains(output, "Cannot find module") {
			t.Fatalf("Node consumer of the packed package did not miss an unpublished file: %s", output)
		}
		return archive, false
	}
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
	return archive, missing
}

type typeConsumerCase struct {
	unsupported  bool
	manifest     string
	declaration  string
	files        map[string]string
	identity     string
	install      map[string]string
	incompatible []string
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
			incompatible: []string{"node10"},
			manifest:     `{"name":"pkg","exports":{".":{"types":"./dist/types.d.ts","default":"./dist/index.js"}}}`, identity: "dep",
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
			incompatible: []string{"node10"},
			manifest:     `{"name":"pkg","exports":"./dist/index.js"}`, identity: "dep",
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
		"packed declaration exclusion": {
			unsupported: true, manifest: `{"name":"pkg","main":"dist/index.js","types":"dist/index.d.ts","files":["dist/index.js","dist/index.d.ts"]}`,
			declaration: `export type { Value } from "./other.js";`, files: map[string]string{"dist/other.d.ts": `export type Value = string;`},
		},
		"packed declaration dependency": {
			manifest:    `{"name":"pkg","main":"dist/index.js","types":"dist/index.d.ts","files":["dist"],"devDependencies":{"dep":"1.0.0"}}`,
			declaration: `export type { Value } from "./other.js";`, identity: "dep",
			files:   map[string]string{"dist/other.d.ts": `export type { Value } from "dep";`},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"nested scope self type import": {
			incompatible: []string{"node10"},
			declaration:  `export type { Value } from "./sub/inner.js";`, identity: "dep",
			files: map[string]string{
				"dist/sub/package.json": `{"name":"pkg","exports":{".":{"types":"./other.d.ts","default":"./other.js"}}}`,
				"dist/sub/inner.d.ts":   `export type { Value } from "pkg";`,
				"dist/sub/other.d.ts":   `export type { Value } from "dep";`,
			},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"dependency only node10 resolves": {
			declaration: `export type { Value } from "./sub/inner.js";`, identity: "dep",
			files: map[string]string{
				"dist/sub/package.json": `{"name":"dep","exports":{".":{"types":"./local.d.ts","default":"./local.js"}}}`,
				"dist/sub/inner.d.ts":   `export type { Value } from "dep";`,
				"dist/sub/local.d.ts":   `export type Value = string;`,
			},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"extensionless types entry": {
			manifest: `{"name":"pkg","main":"dist/index.js","types":"dist/types"}`,
			files:    map[string]string{"dist/types.d.ts": `export type Value = string;`},
		},
		"directory typings entry": {
			manifest: `{"name":"pkg","main":"dist/index.js","typings":"dist/types"}`,
			files:    map[string]string{"dist/types/index.d.ts": `export type Value = string;`},
		},
		"extensionless types entry dependency": {
			manifest: `{"name":"pkg","main":"dist/index.js","types":"dist/types"}`, identity: "dep",
			files:   map[string]string{"dist/types.d.ts": `export type { Value } from "dep";`},
			install: map[string]string{"dep/package.json": `{"types":"index.d.ts"}`, "dep/index.d.ts": `export type Value = string;`},
		},
		"unreachable private declaration": {
			declaration: `export declare const value: string;`,
			files:       map[string]string{"dist/private.d.ts": `export type { Value } from "dep";`},
		},
		"reference path to type reference": {
			declaration: "/// <reference path=\"./extra.d.ts\" />\nexport declare const value: Marker;\n", identity: "@types/dep",
			files:   map[string]string{"dist/extra.d.ts": "/// <reference types=\"dep\" />\n"},
			install: map[string]string{"@types/dep/index.d.ts": `interface Marker {}`},
		},
		"relative type reference":     {unsupported: true, declaration: "/// <reference types=\"./local\" />\nexport {};\n", files: map[string]string{"dist/local.d.ts": `export {};`}},
		"subpath type reference":      {unsupported: true, declaration: "/// <reference types=\"dep/sub\" />\nexport {};\n"},
		"absolute reference path":     {unsupported: true, declaration: "/// <reference path=\"/types/extra.d.ts\" />\nexport {};\n"},
		"file url type import":        {unsupported: true, declaration: `export type Value = import("file:///types/value").Value;`},
		"absolute type import":        {unsupported: true, declaration: `export type { Value } from "/types/value";`},
		"package imports type import": {unsupported: true, declaration: `export type { Value } from "#types";`},
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
			archive, consumer := writeTypeConsumer(t, node, testCase)
			if testCase.unsupported {
				result, err := bundle.PackedImports(t.Context(), archive, "pkg")
				if err != nil || len(result.Unsupported) == 0 {
					t.Fatalf("PackageImports = %+v, %v; want unsupported", result, err)
				}
				return
			}
			requireTypeConsumerNeed(t, node, compiler, consumer, testCase)
			result, err := bundle.PackedImports(t.Context(), archive, "pkg")
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

func writeTypeConsumer(t *testing.T, node string, testCase typeConsumerCase) (string, string) {
	t.Helper()
	consumer := t.TempDir()
	files := map[string]string{"package.json": testCase.manifest, "dist/index.js": `module.exports = {};`}
	if testCase.manifest == "" {
		files["package.json"] = `{"name":"pkg","main":"dist/index.js","types":"dist/index.d.ts"}`
	}
	if testCase.declaration != "" {
		files["dist/index.d.ts"] = testCase.declaration
	}
	for path, source := range testCase.files {
		files[path] = source
	}
	archive := packWithPackageManager(t, node, packedFixture{files: files})
	installPacked(t, archive, filepath.Join(consumer, "node_modules", "pkg"))
	for _, name := range []string{"index.ts", "index.mts", "index.cts"} {
		writeScript(t, filepath.Join(consumer, name), "import type * as pkg from \"pkg\";\nexport type Surface = typeof pkg;\n")
	}
	writeScript(t, filepath.Join(consumer, "compile.cjs"), typeScriptConsumerDriver)
	return archive, consumer
}

func compileTypeConsumer(t *testing.T, node, compiler, consumer string) map[string][]string {
	t.Helper()
	command := exec.CommandContext(t.Context(), node, filepath.Join(consumer, "compile.cjs"), compiler, consumer)
	command.Dir = consumer
	command.Env = []string{}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("compile consumer: %v: %s", err, stderr.String())
	}
	diagnostics := map[string][]string{}
	if err := json.Unmarshal(output, &diagnostics); err != nil || len(diagnostics) != len(typeConsumerConfigurations) {
		t.Fatalf("consumer diagnostics %s: %v", output, err)
	}
	return diagnostics
}

var typeConsumerConfigurations = []string{"node10", "nodenext-esm", "nodenext-cjs", "bundler"}

func requireTypeConsumerNeed(t *testing.T, node, compiler, consumer string, testCase typeConsumerCase) {
	t.Helper()
	before := compileTypeConsumer(t, node, compiler, consumer)
	failing := []string{}
	for _, configuration := range typeConsumerConfigurations {
		if len(before[configuration]) != 0 && !slices.Contains(testCase.incompatible, configuration) {
			failing = append(failing, configuration)
		}
	}
	if testCase.identity == "" {
		if len(failing) != 0 {
			t.Fatalf("neighbor fixture does not compile: %v", before)
		}
		return
	}
	if len(failing) == 0 {
		t.Fatal("fixture compiles without its type dependency in every configuration")
	}
	for path, source := range testCase.install {
		writeScript(t, filepath.Join(consumer, "node_modules", path), source)
	}
	after := compileTypeConsumer(t, node, compiler, consumer)
	for _, configuration := range failing {
		if len(after[configuration]) != 0 {
			t.Fatalf("fixture does not compile under %s with %s installed: %v", configuration, testCase.identity, after[configuration])
		}
	}
	for _, configuration := range testCase.incompatible {
		if len(after[configuration]) == 0 {
			t.Fatalf("fixture declares %s incompatible but it compiles with %s installed", configuration, testCase.identity)
		}
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
const { join } = require("node:path");
const nodeTypes = require("node:fs").existsSync(join(project, "node_modules", "@types", "node"));
const configurations = {
  "node10": ["index.ts", ts.ModuleKind.CommonJS, ts.ModuleResolutionKind.Node10],
  "nodenext-esm": ["index.mts", ts.ModuleKind.NodeNext, ts.ModuleResolutionKind.NodeNext],
  "nodenext-cjs": ["index.cts", ts.ModuleKind.NodeNext, ts.ModuleResolutionKind.NodeNext],
  "bundler": ["index.ts", ts.ModuleKind.ESNext, ts.ModuleResolutionKind.Bundler],
};
const results = {};
for (const [name, [root, module, moduleResolution]] of Object.entries(configurations)) {
  const program = ts.createProgram([join(project, root)], {
    module, moduleResolution, lib: ["lib.es5.d.ts"], types: nodeTypes ? ["node"] : [],
    noEmit: true, strict: true, skipLibCheck: false, ignoreDeprecations: "6.0",
  });
  results[name] = ts.getPreEmitDiagnostics(program).map((diagnostic) =>
    diagnostic.code + " " + ts.flattenDiagnosticMessageText(diagnostic.messageText, " "));
}
process.stdout.write(JSON.stringify(results));
`
