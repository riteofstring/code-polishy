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
import { join } from "node:path";
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
  return {
    protocolVersion: 2,
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
        language: /\.(?:[cm]?[jt]s|[jt]sx|astro)$/.test(input.path)
          ? "typescript"
          : "",
        generated: false,
        test: false,
        development: false,
      })),
      entryPoints: [],
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
      'src="https://example.test/client.js"',
      "{...properties}",
    ]) {
      writeFileSync(join(root, path), `<script ${attributes}></script>\n`);
      const incomplete = analyze(requestFor(root, [path], "architecture"));
      assert.equal(incomplete.status, "incomplete", JSON.stringify(incomplete));
      assert.deepEqual(incomplete.coverage.analyzed, []);
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
    const native = analyze(requestFor(root, ["view.tsx"], "lint"));
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
    const dom = analyze(requestFor(root, ["view.tsx"], "lint"));
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
