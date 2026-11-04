import { lstatSync, realpathSync } from "node:fs";
import { dirname, extname, join } from "node:path";
import { normalize } from "node:path/posix";

import typescript from "typescript";

import { sourceSpecifiers, specifiedPackage } from "./imports.mjs";
import { fail, insideRoot, readTargetFile, unsupported } from "./protocol.mjs";

const MAXIMUM_FILES = 4096;
const MAXIMUM_IMPORTS = 20000;
const CODE_EXTENSIONS = new Set([
  ".js",
  ".mjs",
  ".cjs",
  ".ts",
  ".mts",
  ".cts",
  ".jsx",
  ".tsx",
]);
const OPAQUE_EXTENSIONS = new Set([".json", ".css"]);

function exportTargets(value, targets) {
  if (value === null) return;
  if (typeof value === "string") {
    if (!value.startsWith("./") || /[*\\?#]/.test(value)) {
      throw new Error(
        "package exports must name exact local files for bundled dependency verification",
      );
    }
    targets.push(value);
    return;
  }
  if (typeof value !== "object")
    throw new Error("package exports are not readable");
  for (const target of Object.values(value)) exportTargets(target, targets);
}

function fieldTarget(name, value, targets) {
  if (typeof value !== "string")
    throw new Error(`package ${name} must name a file`);
  exportTargets(value.startsWith("./") ? value : `./${value}`, targets);
}

function executableEntries(manifest) {
  const executables = [];
  if (manifest.bin === undefined) return executables;
  if (
    manifest.bin === null ||
    !["string", "object"].includes(typeof manifest.bin)
  )
    throw new Error("package bin is not readable");
  const targets =
    typeof manifest.bin === "string"
      ? [manifest.bin]
      : Object.values(manifest.bin);
  for (const target of targets) fieldTarget("bin", target, executables);
  return executables;
}

function rejectUnmodeledMappings(manifest) {
  if (
    manifest.typesVersions !== undefined ||
    manifest.browser !== undefined ||
    manifest.directories?.bin !== undefined
  ) {
    throw new Error(
      "bundled dependency verification does not support browser, typesVersions, or directories.bin mappings; use exact exports, entry fields, or bin files",
    );
  }
}

function packageEntries(manifest) {
  const entries = [];
  if (manifest.exports !== undefined) exportTargets(manifest.exports, entries);
  for (const name of ["main", "module", "types", "typings"]) {
    if (manifest[name] !== undefined)
      fieldTarget(name, manifest[name], entries);
  }
  const executables = executableEntries(manifest);
  rejectUnmodeledMappings(manifest);
  if (entries.length === 0 && executables.length === 0)
    throw new Error(
      "package has no explicit published entry files; declare exports, main/types, or bin before claiming bundled dependencies",
    );
  return { entries, executables };
}

function implicitEntries(state, manifest) {
  const implicit = [];
  if (manifest.exports === undefined && manifest.main === undefined)
    implicit.push("index.js");
  if (manifest.types === undefined && manifest.typings === undefined)
    implicit.push("index.d.ts");
  for (const entry of implicit) {
    const path = packagePath(state, entry);
    if (optionalArtifactInfo(state.request.root, path)) enqueue(state, path);
  }
}

function containedPackagePath(state, path) {
  const normalized = normalize(path);
  if (
    normalized !== path ||
    path.includes("\\") ||
    path.startsWith("/") ||
    path === ".." ||
    path.startsWith("../")
  ) {
    throw new Error(`artifact path is not contained: ${path}`);
  }
  if (state.directory !== "." && !path.startsWith(`${state.directory}/`)) {
    throw new Error(`artifact leaves its package: ${path}`);
  }
}

function containedArtifact(state, path) {
  containedPackagePath(state, path);
  const absolute = join(state.request.root, path);
  const real = realpathSync(absolute).split("\\").join("/");
  if (insideRoot(state.packageRoot, real) === null)
    throw new Error(`artifact leaves its package: ${path}`);
  let parent = absolute;
  while (parent !== state.request.root) {
    if (lstatSync(parent).isSymbolicLink())
      throw new Error(`artifact uses a symbolic link: ${path}`);
    parent = dirname(parent);
  }
  return path;
}

function enqueue(state, path) {
  const contained = containedArtifact(state, path);
  if (!state.pending.has(contained)) state.pending.add(contained);
  if (state.pending.size >= MAXIMUM_FILES)
    throw new Error(`published artifact graph exceeds ${MAXIMUM_FILES} files`);
  return contained;
}

function localArtifact(state, from, specifier) {
  const candidate = normalize(
    join(dirname(from), specifier).split("\\").join("/"),
  );
  const declaration = /\.d\.[cm]?ts$/.test(from);
  const candidates = declaration
    ? [
        candidate.replace(/\.[cm]?js$/, (suffix) =>
          suffix === ".js" ? ".d.ts" : suffix === ".mjs" ? ".d.mts" : ".d.cts",
        ),
        `${candidate}.d.ts`,
      ]
    : [candidate, `${candidate}.js`, `${candidate}.cjs`];
  for (const path of candidates) {
    const info = optionalArtifactInfo(state.request.root, path);
    if (info && !info.isDirectory()) return enqueue(state, path);
  }
  if (optionalArtifactInfo(state.request.root, `${candidate}/package.json`))
    throw new Error(
      `published import ${JSON.stringify(specifier)} from ${from} resolves through a nested package manifest; import its file directly`,
    );
  const index = `${candidate}/index${declaration ? ".d.ts" : ".js"}`;
  const info = optionalArtifactInfo(state.request.root, index);
  if (info && !info.isDirectory()) return enqueue(state, index);
  throw new Error(
    `published import ${JSON.stringify(specifier)} from ${from} has no contained artifact`,
  );
}

function optionalArtifactInfo(root, path) {
  try {
    return lstatSync(join(root, path));
  } catch (error) {
    if (error.code === "ENOENT" || error.code === "ENOTDIR") return null;
    throw error;
  }
}

function artifactReference(state, source, specifier, position, kind) {
  const path = source.fileName;
  const location = source.getLineAndCharacterOfPosition(position);
  let resolved = "";
  if (specifier.startsWith(".")) {
    resolved = localArtifact(state, path, specifier);
  } else if (/^[/#]/.test(specifier)) {
    throw new Error(
      `published import ${JSON.stringify(specifier)} needs explicit artifact resolution`,
    );
  }
  state.imports.push({
    path,
    line: location.line + 1,
    column: location.character + 1,
    specifier,
    resolved,
    package: specifiedPackage(specifier),
    kind,
  });
  if (state.imports.length > MAXIMUM_IMPORTS)
    throw new Error(`published imports exceed ${MAXIMUM_IMPORTS} facts`);
}

function parsedArtifact(path, text) {
  const extension = extname(path);
  if (extension !== "" && !CODE_EXTENSIONS.has(extension))
    throw new Error(
      "published artifact is not JavaScript, TypeScript, JSON, or CSS",
    );
  const source = typescript.createSourceFile(
    path,
    text,
    typescript.ScriptTarget.Latest,
    true,
    extension === "" ? typescript.ScriptKind.JS : undefined,
  );
  if (source.parseDiagnostics.length > 0)
    throw new Error(
      "published artifact has invalid JavaScript or TypeScript syntax",
    );
  return source;
}

function readArtifact(state, path) {
  const text = readTargetFile(
    join(state.request.root, path),
    path,
    state.unsupported,
  );
  if (text === null) return;
  if (OPAQUE_EXTENSIONS.has(extname(path))) {
    state.analyzed.push(path);
    return;
  }
  const source = parsedArtifact(path, text);
  const { specifiers, computed } = sourceSpecifiers(source);
  if (computed.length > 0)
    throw new Error("published artifact has a computed dynamic import");
  rejectComputedRequires(source);
  state.analyzed.push(path);
  for (const { node, kind } of specifiers)
    artifactReference(state, source, node.text, node.getStart(source), kind);
  for (const directive of source.referencedFiles)
    artifactReference(
      state,
      source,
      directive.fileName.startsWith(".")
        ? directive.fileName
        : `./${directive.fileName}`,
      directive.pos,
      "type-only",
    );
  for (const directive of source.typeReferenceDirectives)
    artifactReference(
      state,
      source,
      directive.fileName,
      directive.pos,
      "type-only",
    );
}

function packagePath(state, entry) {
  return normalize(join(state.directory, entry).split("\\").join("/"));
}

function enqueueEntry(state, entry) {
  return enqueue(state, packagePath(state, entry));
}

function enqueueLibraryEntry(state, entry) {
  const path = enqueueEntry(state, entry);
  const extension = extname(path);
  let declaration = null;
  if (extension === "") declaration = `${path}.d.ts`;
  else if (/^\.[cm]?js$/.test(extension))
    declaration = path.replace(/\.[cm]?js$/, (suffix) =>
      suffix === ".js" ? ".d.ts" : suffix === ".mjs" ? ".d.mts" : ".d.cts",
    );
  if (declaration && optionalArtifactInfo(state.request.root, declaration))
    enqueue(state, declaration);
}

function enqueuePackageEntries(state, manifest) {
  const { entries, executables } = packageEntries(manifest);
  for (const entry of entries) enqueueLibraryEntry(state, entry);
  for (const entry of executables) enqueueEntry(state, entry);
  implicitEntries(state, manifest);
}

function rejectComputedRequires(source) {
  const visit = (node) => {
    if (
      typescript.isCallExpression(node) &&
      typescript.isIdentifier(node.expression) &&
      node.expression.text === "require" &&
      !typescript.isStringLiteral(node.arguments[0] ?? source)
    ) {
      throw new Error("published artifact has a computed require");
    }
    typescript.forEachChild(node, visit);
  };
  visit(source);
}

export function packageImports(request) {
  if (request.paths.length !== 1 || !request.paths[0].endsWith("package.json"))
    fail("package-imports requires exactly one package manifest");
  const manifestPath = request.paths[0];
  const directory = dirname(manifestPath).split("\\").join("/");
  const state = {
    request,
    directory,
    packageRoot: realpathSync(join(request.root, directory))
      .split("\\")
      .join("/"),
    analyzed: [],
    imports: [],
    unsupported: [],
    pending: new Set(),
  };
  try {
    containedArtifact(state, manifestPath);
    const text = readTargetFile(
      join(request.root, manifestPath),
      manifestPath,
      state.unsupported,
    );
    if (text === null)
      return { analyzed: [], imports: [], unsupported: state.unsupported };
    enqueuePackageEntries(state, JSON.parse(text));
    state.analyzed.push(manifestPath);
    for (const path of state.pending) {
      if (path === manifestPath) continue;
      try {
        readArtifact(state, path);
      } catch (error) {
        state.unsupported.push(unsupported(path, error.message));
      }
    }
  } catch (error) {
    state.unsupported.push(unsupported(manifestPath, error.message));
  }
  return {
    analyzed: state.analyzed,
    imports: state.imports,
    unsupported: state.unsupported,
  };
}
