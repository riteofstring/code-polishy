import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import {
  mkdtempSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { materializeFixtures } from "./fixtures.mjs";

function requestFor(root, files, capability) {
  const context = readdirSync(root, { recursive: true, withFileTypes: true })
    .filter((entry) => entry.isFile())
    .map((entry) => {
      const path = join(entry.parentPath, entry.name)
        .slice(root.length + 1)
        .replaceAll("\\", "/");
      return {
        path,
        sha256: createHash("sha256")
          .update(readFileSync(join(root, path)))
          .digest("hex"),
      };
    });
  const request = {
    protocolVersion: 3,
    projectRoot: root,
    operation: "check",
    capability,
    files,
    context,
    policy: {
      quality: {
        allowComments: false,
        complexity: { typescript: 10, typescriptTest: 20 },
      },
      files: context.map((input) => ({
        path: input.path,
        language:
          !input.path.includes("node_modules/") &&
          /\.(?:[cm]?[jt]s|[jt]sx|astro)$/.test(input.path)
            ? "typescript"
            : "",
        generated: false,
        test: false,
        development: false,
      })),
    },
    runtime: {
      name: "node",
      version: process.versions.node,
      sha256: "0".repeat(64),
    },
    mode: "check",
    profile: "check",
    modules: [],
    complete: true,
  };
  const declarations = context.some(
    (input) => input.path === ".code-polishy.json",
  )
    ? (JSON.parse(readFileSync(join(root, ".code-polishy.json"), "utf8")).scope
        ?.generatedJavaScript ?? [])
    : [];
  const mappings = Object.fromEntries(
    declarations.flatMap((entry) =>
      entry.paths.map((path) => [path, entry.sourcePackage]),
    ),
  );
  resolveTestUnits(request, mappings);
  return request;
}

function resolveTestUnits(request, mappings = {}) {
  request.provider = "javascript";
  const inputs = new Set(request.context.map((file) => file.path));
  const units = new Map();
  for (const file of request.policy.files) {
    if (!file.language || file.path.includes("node_modules/")) continue;
    const candidate = testUnitFor(inputs, mappings[file.path] ?? file.path);
    if (!units.has(candidate.id)) units.set(candidate.id, candidate);
    bindTestSource(request, file, units.get(candidate.id), mappings);
  }
  request.units = [...units.values()];
  request.diagnosticFiles = ["typecheck", "architecture", "dead-code"].includes(
    request.capability,
  )
    ? request.units.flatMap((unit) => unit.members)
    : request.files;
  request.writeFiles = request.files.filter((path) => !mappings[path]);
}

function nearestTestInput(inputs, path, names) {
  for (let root = dirname(path); ; root = dirname(root)) {
    for (const name of names) {
      const candidate = join(root, name).replaceAll("\\", "/");
      if (inputs.has(candidate)) return candidate;
    }
    const alternatives = [...inputs].filter(
      (file) =>
        dirname(file) === root &&
        /^tsconfig.*\.json$/.test(file.split("/").at(-1)),
    );
    if (names.includes("tsconfig.json") && alternatives.length === 1)
      return alternatives[0];
    if (root === ".") return "";
  }
}

function testUnitFor(inputs, context) {
  const manifest = nearestTestInput(inputs, context, ["package.json"]);
  const configuration = nearestTestInput(inputs, context, [
    "tsconfig.json",
    "jsconfig.json",
  ]);
  const packageRoot = dirname(manifest || ".");
  let workspaceRoot = packageRoot;
  for (let directory = packageRoot; directory !== ".";) {
    directory = dirname(directory);
    if (inputs.has(join(directory, "package.json"))) workspaceRoot = directory;
  }
  return {
    id: `${manifest}:${configuration}`,
    root: configuration ? dirname(configuration) : packageRoot,
    packageRoot,
    manifest,
    workspaceRoot,
    configuration,
    members: [],
    entryFiles: [],
  };
}

function bindTestSource(request, file, unit, mappings) {
  file.unit = unit.id;
  file.owner = /\.(?:[cm]?[jt]s|[jt]sx|astro)$/.test(file.path)
    ? request.provider
    : "";
  file.sourcePackage = mappings[file.path] ?? "";
  file.generated = Boolean(file.sourcePackage);
  file.lint = { reactHooks: false, jsxAccessibility: false };
  if (file.owner) unit.members.push(file.path);
  const name = file.path.slice(
    unit.packageRoot === "." ? 0 : unit.packageRoot.length + 1,
  );
  if (
    /^(?:src\/)?(?:index|main|cli)\.[cm]?[jt]sx?$/.test(name) ||
    /^[^/]+\.config\.[cm]?[jt]sx?$/.test(name)
  )
    unit.entryFiles.push(file.path);
}

function analyze(request) {
  const run = spawnSync(
    process.execPath,
    [fileURLToPath(new URL("./run.mjs", import.meta.url))],
    {
      input: JSON.stringify(request),
      encoding: "utf8",
      timeout: 60000,
      maxBuffer: 8 * 1024 * 1024,
      cwd: request.projectRoot,
    },
  );
  assert.equal(run.status, 0, run.stderr);
  return JSON.parse(run.stdout);
}

test("each claimed capability analyzes valid source and catches its seeded defect", async (suite) => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-test-"));
  try {
    for (const fixture of materializeFixtures(root)) {
      await suite.test(fixture.name, () => {
        const request = requestFor(
          join(root, fixture.project),
          fixture.files,
          fixture.capability,
        );
        const response = analyze(request);
        assert.equal(
          response.status,
          fixture.capability === "complexity" ? "pass" : fixture.expectedStatus,
          JSON.stringify(response),
        );
        assert.deepEqual(
          response.coverage.analyzed.toSorted(),
          fixture.files.toSorted(),
        );
        assert.deepEqual(response.coverage.unsupported, []);
        if (fixture.capability === "complexity") {
          assert.deepEqual(response.findings, []);
          assert.deepEqual(
            response.facts.functions.map((fact) => fact.complexity),
            [fixture.expectedStatus === "findings" ? 11 : 1],
          );
        }
        for (const rule of fixture.capability === "complexity"
          ? []
          : (fixture.expectedRules ?? []))
          assert.ok(
            response.findings.some((finding) => finding.rule === rule),
            JSON.stringify(response),
          );
        assert.ok(
          request.files.every((path) =>
            response.inputs.some((input) => input.path === path),
          ),
        );
      });
    }
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("TypeScript import-equals retains runtime and type-only dependency targets", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-equals-"));
  try {
    writeFileSync(join(root, "package.json"), '{"type":"commonjs"}');
    writeFileSync(
      join(root, "value.cts"),
      "const value = 1; export = value;\n",
    );
    for (const [keyword, kind] of [
      ["", "runtime"],
      ["type ", "type-only"],
    ]) {
      writeFileSync(
        join(root, "source.cts"),
        `import ${keyword}value = require("./value.cts");\n`,
      );
      const response = analyze(
        requestFor(root, ["source.cts"], "architecture"),
      );
      assert.equal(response.status, "pass", JSON.stringify(response));
      assert.deepEqual(
        response.facts.imports.map(({ resolved, kind }) => ({
          resolved,
          kind,
        })),
        [{ resolved: "value.cts", kind }],
      );
    }
    rmSync(join(root, "value.cts"));
    const missing = analyze(requestFor(root, ["source.cts"], "architecture"));
    assert.ok(
      missing.findings.some((finding) => finding.rule === "unresolved-import"),
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("framework script sources retain dependency and reachability evidence", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-script-src-"));
  try {
    mkdirSync(join(root, "node_modules/astro"), { recursive: true });
    mkdirSync(join(root, "src/pages"), { recursive: true });
    writeFileSync(
      join(root, "package.json"),
      '{"type":"module","dependencies":{"astro":"7.2.4"}}',
    );
    writeFileSync(
      join(root, "node_modules/astro/package.json"),
      '{"version":"7.2.4"}',
    );
    const path = "src/pages/index.astro";
    writeFileSync(
      join(root, path),
      '<h1>Olá</h1>\n<script src="../client.ts"></script>\n',
    );
    writeFileSync(
      join(root, "src/client.ts"),
      "document.body.dataset.ready = 'yes';\n",
    );
    const response = analyze(requestFor(root, [path], "architecture"));
    assert.equal(response.status, "pass", JSON.stringify(response));
    assert.deepEqual(
      response.facts.imports.map(({ resolved, line, kind }) => ({
        resolved,
        line,
        kind,
      })),
      [{ resolved: "src/client.ts", line: 2, kind: "runtime" }],
    );
    const request = requestFor(root, [path, "src/client.ts"], "dead-code");
    const reachable = analyze(request);
    assert.equal(reachable.status, "pass", JSON.stringify(reachable));
    writeFileSync(
      join(root, path),
      '<script src="https://example.com/browser.js" is:inline></script>\n<script src="../client.ts"></script>\n',
    );
    const externalReachable = analyze(
      requestFor(root, [path, "src/client.ts"], "dead-code"),
    );
    assert.equal(
      externalReachable.status,
      "pass",
      JSON.stringify(externalReachable),
    );
    assert.ok(
      externalReachable.notes.some((note) =>
        note.includes("external browser script"),
      ),
      JSON.stringify(externalReachable),
    );
    writeFileSync(join(root, path), "<h1>Olá</h1>\n");
    const unused = analyze(requestFor(root, ["src/client.ts"], "dead-code"));
    assert.ok(
      unused.findings.some(
        (finding) =>
          finding.rule === "unused-file" && finding.path === "src/client.ts",
      ),
      JSON.stringify(unused),
    );
    for (const attributes of [
      "src={location}",
      'src="../client.ts" is:inline',
      "{...properties}",
    ]) {
      writeFileSync(join(root, path), `<script ${attributes}></script>\n`);
      const incomplete = analyze(requestFor(root, [path], "architecture"));
      assert.equal(incomplete.status, "incomplete", JSON.stringify(incomplete));
      assert.ok(!incomplete.coverage.analyzed.includes(path));
    }
    writeFileSync(join(root, path), '<script src="../missing.ts"></script>\n');
    const missing = analyze(requestFor(root, [path], "architecture"));
    assert.ok(
      missing.findings.some((finding) => finding.rule === "unresolved-import"),
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("external browser scripts retain local diagnostics and bounded dependency coverage", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-external-"));
  try {
    mkdirSync(join(root, "node_modules/astro"), { recursive: true });
    writeFileSync(
      join(root, "node_modules/astro/package.json"),
      '{"version":"7.2.4"}',
    );
    writeFileSync(
      join(root, "package.json"),
      '{"dependencies":{"astro":"7.2.4"}}',
    );
    const path = "page.astro";
    for (const value of [
      '"https://example.test/client.js"',
      "{`https://example.test/client.js?id=${client}`}",
      "{`//example.test/client.js?id=${client}`}",
    ]) {
      writeFileSync(
        join(root, path),
        `<script is:inline src=${value}></script>\n`,
      );
      const response = analyze(requestFor(root, [path], "architecture"));
      assert.equal(response.status, "pass", JSON.stringify(response));
      assert.deepEqual(response.facts.imports, []);
      assert.ok(
        response.notes.some((note) =>
          note.includes("remote bytes were not fetched or checked"),
        ),
      );
    }
    const source =
      "<h1>Olá</h1>\n<script is:inline src={`https://example.test/a?id=${1 === NaN}`}></script>\n";
    writeFileSync(join(root, path), source);
    const lint = analyze(requestFor(root, [path], "lint"));
    const finding = lint.findings.find((item) => item.rule === "use-isnan");
    assert.ok(finding, JSON.stringify(lint));
    assert.equal(finding.path, path);
    assert.equal(finding.line, 2);
    assert.equal(
      finding.column,
      source.split("\n")[1].indexOf("1 === NaN") + 1,
    );
    for (const value of [
      "{source}",
      "{`https://${host}/a.js`}",
      "{`https://example.test${tail}`}",
    ]) {
      writeFileSync(join(root, path), `<script src=${value}></script>\n`);
      const response = analyze(requestFor(root, [path], "architecture"));
      assert.equal(response.status, "incomplete", JSON.stringify(response));
    }
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("nearest compilation units retain strict JavaScript checks and explicit exclusions", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-units-"));
  try {
    mkdirSync(join(root, "src"));
    mkdirSync(join(root, "scripts"));
    writeFileSync(join(root, "package.json"), '{"type":"module"}');
    writeFileSync(
      join(root, "tsconfig.json"),
      '{"include":["src/**/*.ts"],"compilerOptions":{"strict":true}}',
    );
    writeFileSync(
      join(root, "src/main.ts"),
      "export const value: number = 1;\n",
    );
    writeFileSync(
      join(root, "scripts/task.js"),
      "export const value = 1; value();\n",
    );
    const excluded = analyze(
      requestFor(root, ["src/main.ts", "scripts/task.js"], "typecheck"),
    );
    assert.ok(excluded.coverage.analyzed.includes("src/main.ts"));
    assert.ok(
      excluded.coverage.unsupported.some(
        (item) => item.path === "scripts/task.js",
      ),
    );
    writeFileSync(
      join(root, "scripts/tsconfig.json"),
      '{"include":["*.js"],"compilerOptions":{"allowJs":false,"checkJs":false}}',
    );
    const checked = analyze(
      requestFor(root, ["src/main.ts", "scripts/task.js"], "typecheck"),
    );
    assert.deepEqual(checked.coverage.unsupported, []);
    assert.ok(
      checked.findings.some(
        (item) => item.path === "scripts/task.js" && item.rule === "type-2349",
      ),
      JSON.stringify(checked),
    );
    assert.ok(
      checked.notes.some(
        (note) =>
          note.includes("scripts/tsconfig.json") &&
          note.includes("checkJs=true"),
      ),
    );
    writeFileSync(join(root, "scripts/task.js"), "export const value = 1;\n");
    const valid = analyze(
      requestFor(root, ["src/main.ts", "scripts/task.js"], "typecheck"),
    );
    assert.equal(valid.status, "pass", JSON.stringify(valid));
    writeFileSync(
      join(root, "scripts/tsconfig.json"),
      '{"include":["*.js"],"compilerOptions":{"types":["missing-fixture-types"]}}',
    );
    const missingTypes = analyze(
      requestFor(root, ["scripts/task.js"], "typecheck"),
    );
    assert.notEqual(missingTypes.status, "pass", JSON.stringify(missingTypes));
    assert.ok(
      missingTypes.findings.some((finding) => finding.rule === "type-2688") ||
        missingTypes.coverage.unsupported.length > 0,
      JSON.stringify(missingTypes),
    );
    writeFileSync(
      join(root, "scripts/tsconfig.json"),
      '{"include":["*.js"],"references":[{"path":"../src"}]}',
    );
    const references = analyze(
      requestFor(root, ["scripts/task.js"], "typecheck"),
    );
    assert.equal(references.status, "incomplete", JSON.stringify(references));
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("asset resolution requires real files and preserves unknown executable coverage", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-images-"));
  try {
    writeFileSync(
      join(root, "icon.svg"),
      '<svg xmlns="http://www.w3.org/2000/svg"/>\n',
    );
    writeFileSync(
      join(root, "source.ts"),
      'import icon from "./icon.svg"; export { icon };\n',
    );
    const resolved = analyze(requestFor(root, ["source.ts"], "architecture"));
    assert.equal(resolved.status, "pass", JSON.stringify(resolved));
    assert.equal(resolved.facts.imports[0].resolved, "icon.svg");
    rmSync(join(root, "icon.svg"));
    writeFileSync(
      join(root, "icon.d.svg.ts"),
      "declare const value: string; export default value;\n",
    );
    const missing = analyze(requestFor(root, ["source.ts"], "architecture"));
    assert.ok(
      missing.findings.some((finding) => finding.rule === "unresolved-import"),
    );
    assert.equal(missing.facts.imports[0].resolved, "");
    writeFileSync(
      join(root, "tsconfig.json"),
      JSON.stringify({ compilerOptions: { paths: { "@/*": ["./*"] } } }),
    );
    writeFileSync(join(root, "source.ts"), 'import "@/icon.svg";\n');
    const missingAlias = analyze(
      requestFor(root, ["source.ts"], "architecture"),
    );
    assert.ok(
      missingAlias.findings.some(
        (finding) => finding.rule === "unresolved-import",
      ),
    );
    assert.equal(missingAlias.facts.imports[0].resolved, "");
    for (const extension of ["mdx", "vue", "svelte", "wasm"]) {
      writeFileSync(join(root, `unknown.${extension}`), "opaque input\n");
      writeFileSync(
        join(root, "source.ts"),
        `import "./unknown.${extension}";\n`,
      );
      const unknown = analyze(requestFor(root, ["source.ts"], "architecture"));
      assert.ok(
        unknown.findings.some(
          (finding) => finding.rule === "unresolved-import",
        ),
      );
      assert.equal(unknown.facts.imports[0].resolved, "");
    }
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("configuration exclusions and source writes retain selected-file authority", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-scope-"));
  try {
    writeFileSync(join(root, "package.json"), '{"type":"module"}');
    writeFileSync(join(root, "one.ts"), "export const one=1\n");
    writeFileSync(join(root, "two.ts"), "export const two=2\n");
    writeFileSync(join(root, "tsconfig.json"), '{"files":["two.ts"]}');
    const request = requestFor(root, ["one.ts"], "typecheck");
    const excluded = analyze(request);
    assert.equal(excluded.status, "incomplete");
    assert.equal(excluded.coverage.unsupported[0].path, "one.ts");
    const write = analyze({
      ...request,
      capability: "format",
      operation: "format",
      mode: "write",
    });
    assert.equal(write.status, "pass");
    assert.deepEqual(write.edits, [
      { path: "one.ts", content: "export const one = 1;\n" },
    ]);
    assert.equal(
      readFileSync(join(root, "one.ts"), "utf8"),
      "export const one=1\n",
    );
    assert.equal(
      readFileSync(join(root, "two.ts"), "utf8"),
      "export const two=2\n",
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("framework source retains embedded-script coordinates and literal route names", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-framework-"));
  try {
    for (const directory of ["node_modules/astro", "src/pages"])
      mkdirSync(join(root, directory), { recursive: true });
    writeFileSync(
      join(root, "package.json"),
      '{"type":"module","dependencies":{"astro":"7.2.4"}}',
    );
    writeFileSync(
      join(root, "node_modules/astro/package.json"),
      '{"version":"7.2.4"}',
    );
    const path = "src/pages/[slug].astro";
    writeFileSync(
      join(root, path),
      "---\nconst greeting = 'Olá';\n---\n<h1>{greeting}</h1>\n<script>\nfunction value() { return 1; return 2; }\nvalue();\n</script>\n",
    );
    const response = analyze(requestFor(root, [path], "lint"));
    assert.equal(response.status, "findings", JSON.stringify(response));
    assert.ok(
      response.findings.some(
        (finding) =>
          finding.rule === "no-unreachable" &&
          finding.path === path &&
          finding.line === 6,
      ),
    );
    writeFileSync(
      join(root, path),
      "---\nconst greeting = 'Olá';\n---\n<h1>{greeting}</h1>\n",
    );
    const repaired = analyze(requestFor(root, [path], "lint"));
    assert.equal(repaired.status, "pass", JSON.stringify(repaired));
    const graph = analyze(requestFor(root, [path], "architecture"));
    assert.equal(graph.status, "pass", JSON.stringify(graph));
    assert.deepEqual(graph.coverage.analyzed, [path]);
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("metrics use the same nested branches and parameter semantics as native ESLint", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-metrics-"));
  try {
    writeFileSync(
      join(root, "source.ts"),
      "export function value(this: void, input: number, other?: {value: number}) { input ||= 1; if (input > 0) { if (other?.value) return input; } else if (input < 0) return 0; return 1; }\n",
    );
    const result = analyze(requestFor(root, ["source.ts"], "complexity"));
    assert.equal(result.status, "pass", JSON.stringify(result));
    const fact = result.facts.functions.find((item) => item.name === "value");
    assert.equal(fact.complexity, 6);
    assert.equal(fact.depth, 2);
    assert.equal(fact.parameters, 2);
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("builtins and package stylesheets resolve without executing target tools", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-assets-"));
  try {
    mkdirSync(join(root, "node_modules/styles"), { recursive: true });
    writeFileSync(join(root, "package.json"), '{"type":"module"}');
    writeFileSync(
      join(root, "node_modules/styles/package.json"),
      '{"name":"styles","exports":{"./main.css":"./main.css"}}',
    );
    writeFileSync(
      join(root, "node_modules/styles/main.css"),
      "body { color: red; }\n",
    );
    writeFileSync(
      join(root, "source.ts"),
      'import "node:test"; import "styles/main.css";\n',
    );
    const result = analyze(requestFor(root, ["source.ts"], "architecture"));
    assert.equal(result.status, "pass", JSON.stringify(result));
    assert.equal(result.facts.imports[0].package, "node:test");
    assert.equal(
      result.facts.imports[1].resolved,
      "node_modules/styles/main.css",
    );
    rmSync(join(root, "node_modules/styles/main.css"));
    const missing = analyze(requestFor(root, ["source.ts"], "architecture"));
    assert.equal(missing.status, "findings", JSON.stringify(missing));
    assert.ok(
      missing.findings.some((item) => item.rule === "unresolved-import"),
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("React peer and optional dependencies preserve native rule activation", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-react-"));
  try {
    const manifest = { peerDependencies: { react: "19.2.6" } };
    writeFileSync(join(root, "package.json"), JSON.stringify(manifest));
    writeFileSync(
      join(root, "view.tsx"),
      'import { useState } from "react"; export function View({enabled}) { if (enabled) useState(0); return <img />; }\n',
    );
    const nativeRequest = requestFor(root, ["view.tsx"], "lint");
    nativeRequest.policy.files.find(
      (file) => file.path === "view.tsx",
    ).lint.reactHooks = true;
    const native = analyze(nativeRequest);
    assert.ok(
      native.findings.some(
        (finding) => finding.rule === "react-hooks/rules-of-hooks",
      ),
    );
    assert.ok(
      !native.findings.some((finding) => finding.rule.startsWith("jsx-a11y/")),
    );
    manifest.optionalDependencies = { "react-dom": "19.2.6" };
    writeFileSync(join(root, "package.json"), JSON.stringify(manifest));
    const domRequest = requestFor(root, ["view.tsx"], "lint");
    domRequest.policy.files.find((file) => file.path === "view.tsx").lint = {
      reactHooks: true,
      jsxAccessibility: true,
    };
    const dom = analyze(domRequest);
    assert.ok(
      dom.findings.some((finding) => finding.rule === "jsx-a11y/alt-text"),
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("embedded CSS comments remain original-source facts without treating strings as comments", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-style-"));
  try {
    mkdirSync(join(root, "node_modules/astro"), { recursive: true });
    writeFileSync(
      join(root, "package.json"),
      '{"dependencies":{"astro":"7.2.4"}}',
    );
    writeFileSync(
      join(root, "node_modules/astro/package.json"),
      '{"version":"7.2.4"}',
    );
    writeFileSync(
      join(root, "page.astro"),
      '<h1>Olá</h1>\n<style>\n/* top */ a/* selector */ { color: /* value */red; content: "/* literal */"; }\n</style>\n',
    );
    const result = analyze(requestFor(root, ["page.astro"], "lint"));
    assert.equal(result.status, "pass", JSON.stringify(result));
    assert.deepEqual(
      result.facts.comments.map((fact) => fact.raw),
      ["/* top */", "/* selector */", "/* value */"],
    );
    assert.ok(
      result.facts.comments.every((fact) => fact.line === 3 && fact.complete),
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("focused type checking reports errors in unchanged compilation members", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-dependent-"));
  try {
    writeFileSync(join(root, "package.json"), '{"type":"module"}');
    writeFileSync(
      join(root, "tsconfig.app.json"),
      '{"compilerOptions":{"strict":true},"include":["*.ts"]}',
    );
    writeFileSync(join(root, "a.ts"), 'export const value = "text";\n');
    writeFileSync(
      join(root, "b.ts"),
      'import { value } from "./a"; export const length = value.toFixed();\n',
    );
    const request = requestFor(root, ["a.ts"], "typecheck");
    request.complete = false;
    const response = analyze(request);
    assert.equal(response.status, "findings", JSON.stringify(response));
    assert.ok(
      response.findings.some(
        (finding) => finding.path === "b.ts" && finding.rule === "type-2551",
      ),
      JSON.stringify(response),
    );
    assert.ok(response.coverage.analyzed.includes("b.ts"));
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("generated source uses its frontend package and remains non-writable", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-generated-"));
  const path = "python_pkg/generated/bundle.js";
  try {
    mkdirSync(join(root, "frontend/node_modules/library"), { recursive: true });
    mkdirSync(join(root, "python_pkg/generated"), { recursive: true });
    writeFileSync(
      join(root, "frontend/package.json"),
      '{"type":"module","dependencies":{"library":"1.0.0"}}',
    );
    writeFileSync(
      join(root, "frontend/tsconfig.app.json"),
      '{"compilerOptions":{"strict":true},"include":["*.ts"]}',
    );
    writeFileSync(join(root, "frontend/index.ts"), "export const value = 1;\n");
    writeFileSync(
      join(root, "frontend/node_modules/library/package.json"),
      '{"name":"library","version":"1.0.0","types":"index.d.ts"}',
    );
    writeFileSync(
      join(root, "frontend/node_modules/library/index.d.ts"),
      "export function consume(value: number): void;\n",
    );
    writeFileSync(
      join(root, path),
      'import { consume } from "library"; consume("wrong");\n',
    );
    const mappings = { [path]: "frontend/package.json" };
    for (const capability of [
      "lint",
      "typecheck",
      "architecture",
      "dead-code",
    ]) {
      const request = requestFor(root, [path], capability);
      resolveTestUnits(request, mappings);
      for (const unit of request.units)
        if (unit.members.includes(path)) unit.entryFiles.push(path);
      const response = analyze(request);
      assert.ok(
        ["pass", "findings"].includes(response.status),
        JSON.stringify(response),
      );
      assert.ok(
        response.coverage.analyzed.includes(path),
        JSON.stringify(response),
      );
      assert.equal(
        response.coverage.unsupported.length,
        0,
        JSON.stringify(response),
      );
      if (capability === "typecheck")
        assert.ok(
          response.findings.some(
            (finding) => finding.path === path && finding.rule === "type-2345",
          ),
          JSON.stringify(response),
        );
      if (capability === "architecture")
        assert.equal(
          response.facts.imports[0].resolved,
          "frontend/node_modules/library/index.d.ts",
        );
      assert.ok(!response.edits?.length);
    }
    const request = requestFor(root, [path], "format");
    resolveTestUnits(request, mappings);
    request.mode = "write";
    const before = readFileSync(join(root, path));
    const response = analyze(request);
    assert.equal(response.status, "incomplete", JSON.stringify(response));
    assert.ok(!response.edits?.length);
    assert.deepEqual(readFileSync(join(root, path)), before);
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("focused dead code checks nested package trees and unchanged files", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-trees-"));
  try {
    for (const name of ["frontend", "worker"]) {
      mkdirSync(join(root, name));
      writeFileSync(join(root, name, "package.json"), '{"type":"module"}');
      writeFileSync(join(root, name, "index.ts"), "export const value = 1;\n");
      writeFileSync(
        join(root, name, "unreachable.ts"),
        "export const lost = 1;\n",
      );
    }
    writeFileSync(join(root, "frontend/unknown.vue"), "<invalid>");
    const request = requestFor(root, ["frontend/index.ts"], "dead-code");
    request.complete = false;
    request.policy.files.push({
      path: "frontend/unknown.vue",
      language: "typescript",
      owner: "native:typescript",
    });
    const response = analyze(request);
    assert.equal(response.status, "findings", JSON.stringify(response));
    for (const name of ["frontend", "worker"])
      assert.ok(
        response.findings.some(
          (finding) =>
            finding.path === `${name}/unreachable.ts` &&
            finding.rule === "unused-file",
        ),
        JSON.stringify(response),
      );
    assert.deepEqual(response.coverage.unsupported, []);
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("architecture follows connected projects while ignoring unrelated failures", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-closure-"));
  try {
    for (const name of ["app", "shared", "unrelated"]) {
      mkdirSync(join(root, name));
      writeFileSync(join(root, name, "package.json"), '{"type":"module"}');
    }
    writeFileSync(
      join(root, "app/main.ts"),
      'export { value } from "../shared/value";\n',
    );
    writeFileSync(join(root, "shared/value.ts"), "export const value = 1;\n");
    writeFileSync(join(root, "shared/other.ts"), "export const other = 2;\n");
    writeFileSync(join(root, "unrelated/broken.ts"), "this is invalid syntax");
    const request = requestFor(root, ["app/main.ts"], "architecture");
    request.complete = false;
    const response = analyze(request);
    assert.equal(response.status, "pass", JSON.stringify(response));
    assert.deepEqual(response.coverage.analyzed.toSorted(), [
      "app/main.ts",
      "shared/other.ts",
      "shared/value.ts",
    ]);
    assert.ok(
      !response.inputs.some((input) => input.path === "unrelated/broken.ts"),
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("effective lint policy survives invalid metadata and disabled React", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-policy-"));
  try {
    writeFileSync(
      join(root, "package.json"),
      '{"dependencies":{"react":"19.2.6","react-dom":"19.2.6"}}',
    );
    writeFileSync(
      join(root, "view.tsx"),
      'import { useState } from "react"; export function View({ show }: { show: boolean }) { if (show) useState(0); return <img src="picture.png" />; }\n',
    );
    const disabled = analyze(requestFor(root, ["view.tsx"], "lint"));
    assert.ok(
      !disabled.findings.some((finding) =>
        /^(react-hooks|jsx-a11y)\//.test(finding.rule),
      ),
      JSON.stringify(disabled),
    );
    writeFileSync(join(root, "package.json"), "{broken");
    writeFileSync(
      join(root, "view.tsx"),
      "export function broken() { return; return 1; }\n",
    );
    const broken = analyze(requestFor(root, ["view.tsx"], "lint"));
    assert.ok(
      broken.findings.some((finding) => finding.rule === "no-unreachable"),
      JSON.stringify(broken),
    );
    assert.deepEqual(broken.coverage.unsupported, []);
    writeFileSync(join(root, "tsconfig.json"), "{broken");
    writeFileSync(join(root, "other.ts"), "export const value = 1;\n");
    writeFileSync(join(root, "view.tsx"), 'export { value } from "./other";\n');
    const graph = analyze(requestFor(root, ["view.tsx"], "architecture"));
    assert.equal(graph.status, "pass", JSON.stringify(graph));
    assert.equal(graph.facts.imports[0].resolved, "other.ts");
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("invalid UTF-8 is rejected and oversized comments remain bounded facts", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-bytes-"));
  try {
    const invalid = Buffer.from([47, 47, 32, 255, 10]);
    writeFileSync(join(root, "source.js"), invalid);
    const request = requestFor(root, ["source.js"], "format");
    request.mode = "write";
    const response = analyze(request);
    assert.equal(
      response.status,
      "operational-failure",
      JSON.stringify(response),
    );
    assert.match(response.failure, /UTF-8/);
    assert.deepEqual(readFileSync(join(root, "source.js")), invalid);
    writeFileSync(
      join(root, "source.js"),
      `/* ${"é".repeat(40000)} */\nexport const value = 1;\n`,
    );
    const comments = analyze(requestFor(root, ["source.js"], "lint"));
    assert.equal(comments.status, "pass", JSON.stringify(comments));
    assert.equal(comments.facts.comments[0].complete, false);
    assert.ok(Buffer.byteLength(comments.facts.comments[0].raw) <= 65536);
    assert.ok(!comments.facts.comments[0].raw.includes("\ufffd"));
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("normalized entries preserve literal route names and configuration conventions", () => {
  const root = mkdtempSync(join(tmpdir(), "code-polishy-provider-entries-"));
  try {
    mkdirSync(join(root, "src/routes"), { recursive: true });
    writeFileSync(join(root, "package.json"), '{"type":"module"}');
    for (const path of [
      "src/routes/[id].ts",
      "src/routes/i.ts",
      "build-tool.config.ts",
      "src/cli.ts",
    ])
      writeFileSync(join(root, path), "export const value = 1;\n");
    const request = requestFor(root, ["src/cli.ts"], "dead-code");
    request.units[0].entryFiles.push("src/routes/[id].ts");
    const result = analyze(request);
    assert.equal(result.status, "findings", JSON.stringify(result));
    assert.deepEqual(
      result.findings
        .filter((finding) => finding.rule === "unused-file")
        .map((finding) => finding.path),
      ["src/routes/i.ts"],
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("generated entries resolve package imports without relocating relative imports", () => {
  const root = mkdtempSync(
    join(tmpdir(), "code-polishy-provider-import-context-"),
  );
  const bundle = "python_pkg/generated/bundle.js";
  const local = "python_pkg/generated/local.js";
  try {
    mkdirSync(join(root, "frontend"));
    mkdirSync(join(root, "python_pkg/generated"), { recursive: true });
    writeFileSync(
      join(root, "frontend/package.json"),
      JSON.stringify({
        type: "module",
        imports: { "#internal": "./used.ts" },
      }),
    );
    writeFileSync(
      join(root, "frontend/tsconfig.app.json"),
      JSON.stringify({
        compilerOptions: { module: "esnext", moduleResolution: "bundler" },
        include: ["*.ts"],
      }),
    );
    writeFileSync(join(root, "frontend/used.ts"), "export const value = 1;\n");
    writeFileSync(
      join(root, "frontend/local.js"),
      "export const nearby = 2;\n",
    );
    writeFileSync(join(root, local), "export const nearby = 3;\n");
    writeFileSync(
      join(root, bundle),
      'import { value } from "#internal"; import { nearby } from "./local.js"; console.log(value, nearby);\n',
    );
    const mappings = {
      [bundle]: "frontend/package.json",
      [local]: "frontend/package.json",
    };
    const check = () => {
      const request = requestFor(root, [bundle], "dead-code");
      resolveTestUnits(request, mappings);
      for (const unit of request.units)
        if (unit.members.includes(bundle)) unit.entryFiles.push(bundle);
      request.complete = false;
      return analyze(request);
    };
    const used = check();
    assert.deepEqual(used.coverage.unsupported, [], JSON.stringify(used));
    assert.deepEqual(
      used.findings
        .filter((finding) => finding.rule === "unused-file")
        .map((finding) => finding.path),
      ["frontend/local.js"],
    );
    writeFileSync(
      join(root, bundle),
      'import { nearby } from "./local.js"; console.log(nearby);\n',
    );
    const removed = check();
    assert.ok(
      removed.findings.some(
        (finding) =>
          finding.path === "frontend/used.ts" && finding.rule === "unused-file",
      ),
      JSON.stringify(removed),
    );
    assert.ok(
      !removed.findings.some((finding) => finding.path === local),
      JSON.stringify(removed),
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("dead code uses each nested compilation unit's aliases", () => {
  const root = mkdtempSync(
    join(tmpdir(), "code-polishy-provider-nested-aliases-"),
  );
  try {
    writeFileSync(join(root, "package.json"), '{"type":"module"}');
    for (const directory of ["src", "tools"]) {
      mkdirSync(join(root, directory));
      writeFileSync(
        join(root, directory, "tsconfig.app.json"),
        JSON.stringify({
          compilerOptions: {
            module: "esnext",
            moduleResolution: "bundler",
            paths: { "@internal": ["./used.ts"] },
          },
          include: ["*.ts"],
        }),
      );
      writeFileSync(
        join(root, directory, "used.ts"),
        "export const value = 1;\n",
      );
      writeFileSync(
        join(root, directory, "unused.ts"),
        "export const lost = 2;\n",
      );
      writeFileSync(
        join(root, directory, "main.ts"),
        'import { value } from "@internal"; console.log(value);\n',
      );
    }
    const request = requestFor(root, ["src/main.ts"], "dead-code");
    for (const unit of request.units)
      unit.entryFiles.push(`${unit.root}/main.ts`);
    const response = analyze(request);
    assert.deepEqual(
      response.coverage.unsupported,
      [],
      JSON.stringify(response),
    );
    assert.deepEqual(
      response.findings
        .filter((finding) => finding.rule === "unused-file")
        .map((finding) => finding.path)
        .toSorted(),
      ["src/unused.ts", "tools/unused.ts"],
    );
  } finally {
    rmSync(root, { recursive: true });
  }
});

test("generated module format follows its source package and explicit extensions", () => {
  const root = mkdtempSync(
    join(tmpdir(), "code-polishy-provider-module-format-"),
  );
  try {
    mkdirSync(join(root, "frontend/node_modules/library"), { recursive: true });
    mkdirSync(join(root, "python_pkg/generated"), { recursive: true });
    writeFileSync(
      join(root, "frontend/tsconfig.app.json"),
      JSON.stringify({
        compilerOptions: { module: "nodenext", moduleResolution: "nodenext" },
        include: ["*.ts"],
      }),
    );
    writeFileSync(join(root, "frontend/index.ts"), "export const value = 1;\n");
    writeFileSync(
      join(root, "frontend/node_modules/library/package.json"),
      JSON.stringify({
        name: "library",
        type: "module",
        exports: { import: "./esm.d.ts", require: "./cjs.d.cts" },
      }),
    );
    writeFileSync(
      join(root, "frontend/node_modules/library/esm.d.ts"),
      "export function consume(value: number): void;\n",
    );
    writeFileSync(
      join(root, "frontend/node_modules/library/cjs.d.cts"),
      "export function consume(value: string): void;\n",
    );
    const files = ["js", "mjs", "cjs"].map(
      (extension) => `python_pkg/generated/bundle.${extension}`,
    );
    const mappings = Object.fromEntries(
      files.map((path) => [path, "frontend/package.json"]),
    );
    for (const [type, commonjsFiles] of [
      ["module", [files[2]]],
      ["commonjs", [files[2], files[0]]],
    ]) {
      writeFileSync(
        join(root, "frontend/package.json"),
        JSON.stringify({ type }),
      );
      writeModuleTypecheckSources(root, files, commonjsFiles);
      const request = requestFor(root, files, "typecheck");
      resolveTestUnits(request, mappings);
      const response = analyze(request);
      assert.deepEqual(
        response.coverage.unsupported,
        [],
        JSON.stringify(response),
      );
      const commonjs = response.findings
        .filter((finding) => finding.rule === "type-1309")
        .map((finding) => finding.path)
        .toSorted();
      assert.deepEqual(commonjs, commonjsFiles, JSON.stringify(response));
      assert.ok(
        response.findings.every((finding) => finding.rule === "type-1309"),
        JSON.stringify(response),
      );
      assertModuleGraph(root, files, mappings, commonjsFiles);
    }
  } finally {
    rmSync(root, { recursive: true });
  }
});

function writeModuleTypecheckSources(root, files, commonjsFiles) {
  for (const path of files)
    writeFileSync(
      join(root, path),
      `import { consume } from "library"; consume(${commonjsFiles.includes(path) ? '\"text\"' : "1"}); export const value = await Promise.resolve(1);\n`,
    );
}

function assertModuleGraph(root, files, mappings, commonjsFiles) {
  for (const path of files)
    writeFileSync(
      join(root, path),
      commonjsFiles.includes(path)
        ? 'const { consume } = require("library"); consume("text"); import("library");\n'
        : 'import { consume } from "library"; consume(1); import("library");\n',
    );
  const request = requestFor(root, files, "architecture");
  resolveTestUnits(request, mappings);
  const graph = analyze(request);
  assert.equal(graph.status, "pass", JSON.stringify(graph));
  assert.equal(graph.facts.imports.length, files.length * 2);
  for (const fact of graph.facts.imports) {
    const commonjs =
      fact.kind !== "proven-dynamic" && commonjsFiles.includes(fact.path);
    assert.equal(
      fact.resolved,
      `frontend/node_modules/library/${commonjs ? "cjs.d.cts" : "esm.d.ts"}`,
      JSON.stringify(fact),
    );
  }
}
