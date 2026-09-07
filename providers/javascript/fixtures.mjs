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
    "function-complexity",
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
        command: "analyze",
        capability,
        project,
        files: Object.keys(files).filter((path) => path.endsWith(".ts")),
        expectedStatus: failing ? "findings" : "pass",
        ...(failing ? { expectedRules: [rule] } : {}),
      });
    }
  }
  return fixtures;
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
