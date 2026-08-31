import { lstatSync, readFileSync, realpathSync } from "node:fs";
import { dirname, isAbsolute, join, normalize, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";

export const PROTOCOL_VERSION = 3;
export const MAXIMUM_REQUEST_BYTES = 1024 * 1024;
export const MAXIMUM_OPERATION_PATHS = 4096;
const MAXIMUM_FILE_BYTES = 4 * 1024 * 1024;
const MAXIMUM_REASON_CHARACTERS = 200;
const BUNDLE_MANIFEST_NAME = "bundle-manifest.json";

const PROHIBITED_ENVIRONMENT = [
  "NODE_OPTIONS",
  "NODE_PATH",
  "NODE_REPL_EXTERNAL_MODULE",
];

const bundleDirectory = dirname(fileURLToPath(import.meta.url));

const bundleRoot = realpathSync(bundleDirectory);

let targetRoot = "";

export function respond(response) {
  process.stdout.write(`${JSON.stringify(response)}\n`);
}

export function fail(message) {
  respond({ protocolVersion: PROTOCOL_VERSION, error: message });
  process.exit(1);
}

function readBundleJson(...segments) {
  const path = join(bundleDirectory, ...segments);
  let text;
  try {
    text = readFileSync(path, "utf8");
  } catch {
    fail(`the installed bundle is missing ${path}`);
  }
  try {
    return JSON.parse(text);
  } catch (error) {
    fail(
      `the installed bundle file ${path} is not readable JSON: ${error.message}`,
    );
  }
}

export function requireSealedLaunch() {
  if (process.execArgv.length > 0) {
    fail(
      `the runner was launched with extra Node options: ${process.execArgv.join(" ")}`,
    );
  }
  for (const variable of PROHIBITED_ENVIRONMENT) {
    if (process.env[variable] !== undefined) {
      fail(`the runner was launched with ${variable} set`);
    }
  }
  const pinnedNode = readBundleJson("package.json").engines?.node;
  if (process.versions.node !== pinnedNode) {
    fail(
      `the runner requires the policy-owned Node ${pinnedNode}, not ${process.versions.node}`,
    );
  }
}

export function provenance() {
  const manifest = readBundleJson(BUNDLE_MANIFEST_NAME);
  const tools = {};
  for (const [name, declared] of Object.entries(manifest.tools)) {
    const installed = readBundleJson(
      "node_modules",
      name,
      "package.json",
    ).version;
    if (installed !== declared) {
      fail(
        `the installed bundle declares ${name} ${declared} but installed ${installed}`,
      );
    }
    tools[name] = installed;
  }
  return {
    bundleDigest: manifest.bundleDigest,
    node: manifest.node,
    pnpm: manifest.pnpm,
    tools,
  };
}

export function truncate(text) {
  const single = text.split("\n", 1)[0];
  return single.length > MAXIMUM_REASON_CHARACTERS
    ? `${single.slice(0, MAXIMUM_REASON_CHARACTERS)}…`
    : single;
}

export function unsupported(path, reason) {
  return { path, reason: truncate(reason) };
}

function resolved(path) {
  try {
    return realpathSync(path);
  } catch {
    return "";
  }
}

function insideTree(tree, path) {
  if (tree === "" || path === "") {
    return false;
  }
  const difference = relative(tree, path);
  return (
    difference === "" ||
    (!isAbsolute(difference) &&
      difference !== ".." &&
      !difference.startsWith(`..${sep}`))
  );
}

export function containedRead(path) {
  return insideTree(targetRoot, resolved(path));
}

export function containedProgramRead(path) {
  const real = resolved(path);
  return insideTree(targetRoot, real) || insideTree(bundleRoot, real);
}

export function insideRoot(root, fileName) {
  const prefix = `${root}/`;
  return fileName.startsWith(prefix) ? fileName.slice(prefix.length) : null;
}

export function readTargetFile(absolute, path, unsupportedPaths) {
  let info;
  let contained = false;
  let data;
  try {
    info = lstatSync(absolute);
    contained = info.isFile() && containedRead(absolute);
    if (contained && info.size <= MAXIMUM_FILE_BYTES) {
      data = readFileSync(absolute);
    }
  } catch (error) {
    unsupportedPaths.push(
      unsupported(
        path,
        `the file is unreadable: ${error.message.replaceAll(absolute, path)}`,
      ),
    );
    return null;
  }
  if (!info.isFile()) {
    unsupportedPaths.push(unsupported(path, "the path is not a regular file"));
    return null;
  }
  if (!contained) {
    unsupportedPaths.push(
      unsupported(path, "the path resolves outside the target tree"),
    );
    return null;
  }
  if (info.size > MAXIMUM_FILE_BYTES) {
    unsupportedPaths.push(
      unsupported(
        path,
        `the file exceeds the ${MAXIMUM_FILE_BYTES} byte limit`,
      ),
    );
    return null;
  }
  const text = data.toString("utf8");
  if (!Buffer.from(text, "utf8").equals(data)) {
    unsupportedPaths.push(unsupported(path, "the file is not UTF-8 text"));
    return null;
  }
  return text;
}

export function requireContainedRoot(root) {
  if (
    typeof root !== "string" ||
    !isAbsolute(root) ||
    normalize(root) !== root
  ) {
    fail(
      `the request declares root ${JSON.stringify(root)}, not a normal absolute path`,
    );
  }
  const real = resolved(root);
  if (real === "") {
    fail(
      `the request declares root ${JSON.stringify(root)}, which names no directory`,
    );
  }
  targetRoot = real;
}

export function requireContainedPaths(paths) {
  if (!Array.isArray(paths)) {
    fail("the request declares paths that are not an array");
  }
  if (paths.length > MAXIMUM_OPERATION_PATHS) {
    fail(
      `the request declares ${paths.length} paths, more than the ${MAXIMUM_OPERATION_PATHS} limit`,
    );
  }
  for (const path of paths) {
    requireContainedPath(path);
  }
}

export function requireContainedPath(path) {
  if (typeof path !== "string" || path === "" || path.includes("\\")) {
    fail(
      `the request declares path ${JSON.stringify(path)}, not a repository-relative path`,
    );
  }
  if (
    isAbsolute(path) ||
    normalize(path) !== path ||
    path.split("/").includes("..")
  ) {
    fail(
      `the request declares path ${JSON.stringify(path)}, not a contained relative path`,
    );
  }
}

export function requireExactObject(value, description, names) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    fail(`${description} is not a JSON object`);
  }
  for (const name of names) {
    if (!(name in value)) {
      fail(`${description} is missing field '${name}'`);
    }
  }
  for (const name of Object.keys(value)) {
    if (!names.includes(name)) {
      fail(`${description} declares unknown field '${name}'`);
    }
  }
}
