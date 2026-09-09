import {
  cpSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { materializeFixtures } from "./fixtures.mjs";

const source = dirname(fileURLToPath(import.meta.url));
const [operation, target] = process.argv.slice(2);
if (!target || !["prepare", "finish"].includes(operation))
  throw new Error("usage: build.mjs prepare|finish TARGET");
const provider = join(target, "providers/javascript");
if (operation === "prepare") prepare();
else finish();

function prepare() {
  if (existsSync(target))
    throw new Error("the provider destination already exists");
  mkdirSync(provider, { recursive: true });
  for (const name of readdirSync(source)) {
    if (
      name.endsWith(".test.mjs") ||
      ["node_modules", "build.mjs", "fixtures.mjs"].includes(name)
    )
      continue;
    cpSync(join(source, name), join(provider, name), { recursive: true });
  }
  mkdirSync(join(target, "tools/javascript"), { recursive: true });
  cpSync(
    join(source, "../../tools/javascript/policy.mjs"),
    join(target, "tools/javascript/policy.mjs"),
  );
  cpSync(join(source, "README.md"), join(target, "README.md"));
  const fixtures = materializeFixtures(target);
  const metadata = JSON.parse(
    readFileSync(join(source, "package.json"), "utf8"),
  );
  const manifest = {
    manifestVersion: 2,
    protocolVersion: 3,
    name: "javascript",
    version: metadata.version,
    platforms: [
      "darwin-amd64",
      "darwin-arm64",
      "linux-amd64",
      "linux-arm64",
      "windows-amd64",
    ],
    languages: [
      {
        id: "typescript",
        sourcePatterns: [
          "**/*.js",
          "**/*.jsx",
          "**/*.ts",
          "**/*.tsx",
          "**/*.cjs",
          "**/*.mjs",
          "**/*.cts",
          "**/*.mts",
          "**/*.astro",
        ],
      },
    ],
    commands: [
      {
        name: "format",
        argv: ["providers/javascript/run.mjs"],
        capabilities: ["format"],
        profiles: ["check", "gate", "format"],
        timeoutSeconds: 600,
        runtime: { name: "node", version: metadata.engines.node },
      },
      {
        name: "analyze",
        argv: ["providers/javascript/run.mjs"],
        capabilities: [
          "lint",
          "typecheck",
          "complexity",
          "dead-code",
          "architecture",
        ],
        profiles: ["check", "gate"],
        timeoutSeconds: 600,
        runtime: { name: "node", version: metadata.engines.node },
      },
    ],
    fixtures,
  };
  writeFileSync(
    join(target, "code-polishy-pack.json"),
    `${JSON.stringify(manifest, null, 2)}\n`,
  );
}

function finish() {
  const modules = join(provider, "node_modules");
  const entries = readdirSync(modules, {
    recursive: true,
    withFileTypes: true,
  });
  for (const entry of entries) {
    if (entry.name === ".bin" && entry.isDirectory())
      rmSync(join(entry.parentPath, entry.name), { recursive: true });
  }
  const { files, bytes } = treeSize(target);
  if (files > 20000 || bytes > 128 * 1024 * 1024)
    throw new Error("the provider tree exceeds pack inventory limits");
  process.stdout.write(`${files} files, ${bytes} bytes\n`);
}

function treeSize(target) {
  let files = 0,
    bytes = 0;
  for (const entry of readdirSync(target, {
    recursive: true,
    withFileTypes: true,
  })) {
    const path = join(entry.parentPath, entry.name);
    const info = fileInfo(path);
    if (!info.isFile()) continue;
    files++;
    bytes += info.size;
    if (info.size > 16 * 1024 * 1024)
      throw new Error(`provider file exceeds 16 MiB: ${path}`);
  }
  return { files, bytes };
}

function fileInfo(path) {
  const info = lstatSync(path);
  if (info.isSymbolicLink() || (!info.isDirectory() && !info.isFile()))
    throw new Error(`the provider tree contains an unsupported entry: ${path}`);
  return info;
}
