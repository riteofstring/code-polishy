// The bounded JSON protocol every operation of the sealed, policy-owned
// JavaScript tool bundle answers under, and the reading it is allowed to do.
//
// One request arrives on stdin or in the exact runner argument and one response
// leaves on stdout. Nothing here is resolved from the invoking environment, the
// working directory, a target's
// node_modules, a user cache, or a global installation: every bundle path is
// derived from this file's own installed location, and every target path is
// required to name something inside the declared target tree.

import { lstatSync, readFileSync, realpathSync } from "node:fs";
import { dirname, isAbsolute, join, normalize } from "node:path";
import { fileURLToPath } from "node:url";

export const PROTOCOL_VERSION = 2;
export const MAXIMUM_REQUEST_BYTES = 1024 * 1024;
export const MAXIMUM_OPERATION_PATHS = 4096;
const MAXIMUM_FILE_BYTES = 4 * 1024 * 1024;
const MAXIMUM_REASON_CHARACTERS = 200;
const BUNDLE_MANIFEST_NAME = "bundle-manifest.json";
// Environment that would let another party inject code, a module path, or a
// debugger into a policy run. Go scrubs these; the bundle refuses them anyway.
const PROHIBITED_ENVIRONMENT = [
  "NODE_OPTIONS",
  "NODE_PATH",
  "NODE_REPL_EXTERNAL_MODULE",
];

const bundleDirectory = dirname(fileURLToPath(import.meta.url));
// The installed bundle as the file system really names it. Every analyzer is
// installed behind a link, so a policy-owned declaration the type checker reads
// arrives as a path inside the virtual store rather than the one it was
// imported through.
const bundleRoot = realpathSync(bundleDirectory);
// The target tree as the file system really names it, recorded when a request
// declares its root. A read is checked against this rather than against the
// path the request wrote, because a lexical check only describes the text of a
// path: a symlinked directory component makes a contained-looking relative path
// name a file anywhere on the host.
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

// Refuse a launch that is not the sealed one before reading a request.
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

// What ran, from the installed bytes themselves. Every declared tool version is
// confirmed against the package actually installed beside the runner, so a
// drifted or substituted tree fails instead of reporting the version it claims.
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

// One bounded line of text. A tool's own diagnostics are the only unbounded
// thing an operation handles, and none of them crosses the protocol whole.
export function truncate(text) {
  const single = text.split("\n", 1)[0];
  return single.length > MAXIMUM_REASON_CHARACTERS
    ? `${single.slice(0, MAXIMUM_REASON_CHARACTERS)}…`
    : single;
}

// A file a sealed analyzer cannot decide. Reporting it is how coverage fails
// closed: Go turns each one into a finding rather than treating it as clean.
export function unsupported(path, reason) {
  return { path, reason: truncate(reason) };
}

// Where a path really is, or nothing when it names nothing. A path that does
// not exist resolves nowhere, which is also the answer every caller wants: an
// absent file is not a readable one.
function resolved(path) {
  try {
    return realpathSync(path);
  } catch {
    return "";
  }
}

function insideTree(tree, path) {
  return tree !== "" && (path === tree || path.startsWith(`${tree}/`));
}

// Whether a path really names something inside the target tree. This is the one
// question every static operation asks before it reads: a selected path, an
// extension chain, a resolved module, and a link inside the target are all
// admitted only when what they actually name is still inside the repository.
export function containedRead(path) {
  return insideTree(targetRoot, resolved(path));
}

// Whether a path really names something the type checker may read. Its program
// also reads the library declarations installed inside the bundle, which are
// policy-owned bytes and the only tree outside the target a static operation
// ever opens.
export function containedProgramRead(path) {
  const real = resolved(path);
  return insideTree(targetRoot, real) || insideTree(bundleRoot, real);
}

// The remainder of an absolute file name inside the declared target tree, or
// null for anything else. A tool names files absolutely; a result names one the
// way the request did, and a file outside the target is never named at all.
export function insideRoot(root, fileName) {
  const prefix = `${root}/`;
  return fileName.startsWith(prefix) ? fileName.slice(prefix.length) : null;
}

// Read one selected target file as text. Anything an analyzer must not decide
// on bytes alone — an oversized file, a file that is not UTF-8 text, a symlink,
// a directory, a path that really names a file outside the target tree — is
// unsupported rather than silently analyzed or rewritten.
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
    // The runtime names the file absolutely; the reported reason names it the
    // way the request did, so no host path crosses the protocol.
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

// The target tree a file operation reads. It is absolute and already normal, so
// nothing here has to guess what an unnormalized or relative root would mean,
// and it is recorded as the file system really names it, because that is what
// every later read is checked against.
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

// Selected paths are repository relative and clean, so every one of them names
// a file inside the declared root and no other tree can be reached through one.
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

// One JSON object with exactly the declared fields. An unknown field is a
// protocol disagreement, and a missing one is never defaulted.
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
