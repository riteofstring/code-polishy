import { createHash } from "node:crypto";
import {
  lstatSync,
  readFileSync,
  readdirSync,
  readlinkSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { basename, join, relative, resolve, sep } from "node:path";

const MANIFEST_NAME = "bundle-manifest.json";
const SOURCE_FILES_NAME = "source-files.txt";
function fail(message, status = 1) {
  process.stderr.write(`${message}\n`);
  process.exit(status);
}

function digest(value) {
  return createHash("sha256").update(value).digest("hex");
}

function fileDigest(path) {
  return digest(readFileSync(path));
}

function readText(path) {
  return readFileSync(path, "utf8").trim();
}

function readJson(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function sourceFiles(sourceDirectory) {
  const paths = readText(join(sourceDirectory, SOURCE_FILES_NAME)).split(
    /\r?\n/,
  );
  const unique = new Set(paths);
  if (
    paths.length === 0 ||
    unique.size !== paths.length ||
    paths.some(
      (path) =>
        path === "" ||
        path === "." ||
        path === ".." ||
        path.includes("/") ||
        path.includes("\\"),
    )
  ) {
    fail(`The JavaScript bundle source inventory is invalid.`);
  }
  return paths;
}

function sourceDigest(sourceDirectory) {
  const entries = sourceFiles(sourceDirectory).map(
    (path) => `${fileDigest(join(sourceDirectory, path))}  ${path}\n`,
  );
  return digest(entries.join(""));
}

function bundleEntries(root, directory = root, entries = []) {
  for (const name of readdirSync(directory).sort()) {
    const path = join(directory, name);
    const installed = relative(root, path).split(sep).join("/");
    const info = lstatSync(path);
    if (info.isSymbolicLink()) {
      entries.push(`./${installed}\tsymlink ${readlinkSync(path)}`);
    } else if (info.isDirectory()) {
      bundleEntries(root, path, entries);
    } else if (info.isFile() && basename(path) !== MANIFEST_NAME) {
      entries.push(`./${installed}\t${fileDigest(path)}`);
    } else if (!info.isFile()) {
      fail(`The JavaScript bundle contains an unsupported entry at ${path}.`);
    }
  }
  return entries;
}

function bundleFacts(bundleDirectory) {
  const entries = bundleEntries(bundleDirectory).sort();
  if (entries.length === 0) {
    fail(
      `The JavaScript bundle at ${bundleDirectory} contains no installed entries.`,
    );
  }
  return {
    bundleDigest: digest(`${entries.join("\n")}\n`),
    entryCount: entries.length,
  };
}

function declaredTools(sourceDirectory) {
  const tools = readJson(join(sourceDirectory, "package.json")).dependencies;
  if (
    tools === null ||
    typeof tools !== "object" ||
    Array.isArray(tools) ||
    Object.keys(tools).length === 0 ||
    Object.values(tools).some((version) => typeof version !== "string")
  ) {
    fail(`The checked-in JavaScript bundle declares no tools.`);
  }
  return tools;
}

function renderManifest(bundleDirectory, policyRoot) {
  const sourceDirectory = join(policyRoot, "tools", "javascript");
  const facts = bundleFacts(bundleDirectory);
  return `${JSON.stringify(
    {
      manifestVersion: 1,
      sourceDigest: sourceDigest(sourceDirectory),
      bundleDigest: facts.bundleDigest,
      entryCount: facts.entryCount,
      node: readText(join(policyRoot, "tools", "node-version.txt")),
      pnpm: readText(join(policyRoot, "tools", "pnpm-version.txt")),
      tools: declaredTools(sourceDirectory),
    },
    null,
    2,
  )}\n`;
}

function main() {
  if (process.argv.length !== 5) {
    fail(
      "usage: bundle-manifest.mjs <write|verify> <bundle-dir> <policy-root>",
      2,
    );
  }
  const [, , mode, requestedBundle, requestedPolicyRoot] = process.argv;
  if (mode !== "write" && mode !== "verify") {
    fail(
      "usage: bundle-manifest.mjs <write|verify> <bundle-dir> <policy-root>",
      2,
    );
  }
  const bundleDirectory = resolve(requestedBundle);
  const policyRoot = resolve(requestedPolicyRoot);
  if (!statSync(bundleDirectory).isDirectory()) {
    fail(`Missing JavaScript bundle directory ${bundleDirectory}.`);
  }
  const manifestPath = join(bundleDirectory, MANIFEST_NAME);
  const rendered = renderManifest(bundleDirectory, policyRoot);
  if (mode === "write") {
    writeFileSync(manifestPath, rendered);
    return;
  }
  let installed;
  try {
    installed = readFileSync(manifestPath, "utf8");
  } catch {
    fail(
      `The JavaScript bundle at ${bundleDirectory} has no ${MANIFEST_NAME}.`,
    );
  }
  if (installed !== rendered) {
    fail(
      `The JavaScript bundle at ${bundleDirectory} does not exactly match its installed entries and checked-in pins.`,
    );
  }
}

try {
  main();
} catch (error) {
  fail(error.message);
}
