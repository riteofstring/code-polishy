import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

const valid =
  "export function greet(name: string): string {\n  return `Hello ${name}`;\n}\n";
const defects = {
  format: ["export const value=1\n", "format"],
  lint: [
    "export function value(): number { return 1; return 2; }\n",
    "no-unreachable",
  ],
  typecheck: ["export const value: number = 'wrong';\n", "type-2322"],
  complexity: [
    `export function choose(value: number): number {\n${Array.from({ length: 10 }, (_, index) => `  if (value === ${index}) return ${index};`).join("\n")}\n  return -1;\n}\n`,
    "quality.functioncomplexity",
  ],
  architecture: ['import "./missing.ts";\nexport {};\n', "unresolved-import"],
  "dead-code": ["export const unused = 1;\n", "unused-file"],
};

export function materializeFixtures(root) {
  const fixtures = [];
  for (const [capability, [source, rule]] of Object.entries(defects)) {
    for (const failing of [false, true]) {
      const name = `${capability}-${failing ? "defect" : "valid"}`;
      const project = `fixtures/${name}`;
      const files = fixtureFiles(capability, source, failing);
      for (const [path, content] of Object.entries(files)) {
        const absolute = join(root, project, path);
        mkdirSync(dirname(absolute), { recursive: true });
        writeFileSync(absolute, content);
      }
      fixtures.push({
        name,
        command: capability === "format" ? "format" : "analyze",
        capability,
        project,
        files: Object.keys(files).filter((path) => path.endsWith(".ts")),
        expectedStatus: failing ? "findings" : "pass",
        ...(failing ? { expectedRules: [rule] } : {}),
      });
    }
  }
  return [...fixtures, ...generatedFixtures(root)];
}

function fixtureFiles(capability, source, failing) {
  const files = {
    "package.json":
      '{"name":"fixture","type":"module","main":"src/index.ts"}\n',
    "tsconfig.json":
      '{"compilerOptions":{"strict":true,"target":"ESNext","module":"ESNext","moduleResolution":"Bundler","skipLibCheck":true},"include":["src/**/*"]}\n',
    "src/index.ts": failing && capability !== "dead-code" ? source : valid,
  };
  if (failing && capability === "dead-code")
    files["src/unreachable.ts"] = source;
  return files;
}

function generatedFixtures(root) {
  const fixtures = [];
  for (const failing of [false, true]) {
    const name = `generated-typecheck-${failing ? "defect" : "valid"}`;
    const project = `fixtures/${name}`;
    const files = {
      ".code-polishy.json": JSON.stringify({
        version: 4,
        project: { kind: "application", capabilities: [] },
        modules: [{ name: "frontend", paths: ["frontend/**"] }],
        tests: {
          ownership: [
            {
              paths: ["frontend/boundary.test.mjs"],
              module: "frontend",
              focusedSuite: "frontend-boundary",
            },
          ],
          suites: [
            {
              name: "frontend-boundary",
              kind: "boundary",
              scope: "module",
              modules: ["frontend"],
              paths: ["frontend/boundary.test.mjs"],
              argv: ["node", "--test", "frontend/boundary.test.mjs"],
            },
          ],
        },
        scope: {
          generated: ["python_pkg/generated/bundle.js"],
          generatedJavaScript: [
            {
              paths: ["python_pkg/generated/bundle.js"],
              sourcePackage: "frontend/package.json",
            },
          ],
        },
      }),
      "frontend/package.json": '{"type":"module"}',
      "frontend/tsconfig.app.json":
        '{"compilerOptions":{"strict":true,"module":"ESNext","moduleResolution":"Bundler"},"include":["*.ts"]}',
      "frontend/index.ts": "export const value = 1;\n",
      "frontend/boundary.test.mjs":
        'import assert from "node:assert/strict";\nimport { value } from "../python_pkg/generated/bundle.js";\nassert.equal(value, 1);\n',
      "python_pkg/generated/bundle.js": failing
        ? "export const value = 1; value.toUpperCase();\n"
        : "export const value = 1; value.toFixed();\n",
    };
    for (const [path, content] of Object.entries(files)) {
      const absolute = join(root, project, path);
      mkdirSync(dirname(absolute), { recursive: true });
      writeFileSync(absolute, content);
    }
    fixtures.push({
      name,
      command: "analyze",
      capability: "typecheck",
      project,
      files: ["frontend/index.ts", "python_pkg/generated/bundle.js"],
      expectedStatus: failing ? "findings" : "pass",
      ...(failing ? { expectedRules: ["type-2339"] } : {}),
    });
  }
  return fixtures;
}
