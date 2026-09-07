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
        language: input.path.endsWith(".ts") ? "typescript" : "",
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
          fixture.expectedStatus,
          JSON.stringify(response),
        );
        assert.deepEqual(
          response.coverage.analyzed.toSorted(),
          fixture.files.toSorted(),
        );
        assert.deepEqual(response.coverage.unsupported, []);
        for (const rule of fixture.expectedRules ?? [])
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
