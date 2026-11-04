import { lstatSync, realpathSync, statSync } from "node:fs";
import { createRequire, isBuiltin } from "node:module";
import { basename, dirname, extname, join } from "node:path";
import { normalize } from "node:path/posix";
import { fileURLToPath, pathToFileURL } from "node:url";

import typescript from "typescript";

import { sourceSpecifiers, specifiedPackage } from "./imports.mjs";
import { loaderSpecifiers } from "./package-loaders.mjs";
import { typeClosure } from "./package-types.mjs";
import { fail, insideRoot, readTargetFile, unsupported } from "./protocol.mjs";

const MANIFEST = "package.json";
const MAXIMUM_FILES = 4096;
const MAXIMUM_IMPORTS = 20000;
const RUNTIME_EXTENSIONS = new Set(["", ".js", ".mjs", ".cjs"]);
const OPAQUE_EXTENSIONS = new Set([".json", ".css"]);
const TYPE_EXTENSIONS = new Set([".ts", ".mts", ".cts", ".tsx"]);
const RELATIVE_SPECIFIER = /^\.\.?(?:\/|$)/;
const URL_SPECIFIER = /^[A-Za-z][A-Za-z0-9+.-]*:/;
const NODE_LOADER_MODULES = new Set(["module", "node:module"]);
const NOT_FOUND = new Set(["MODULE_NOT_FOUND", "ERR_MODULE_NOT_FOUND"]);

function exportTargets(value, entries, kind) {
  if (value === null) return;
  if (typeof value === "string") {
    if (!value.startsWith("./") || /[*\\?#%]/.test(value)) {
      throw new Error(
        "package exports must name exact local files for bundled dependency verification",
      );
    }
    entries[kind].push(value);
    return;
  }
  if (typeof value !== "object")
    throw new Error("package exports are not readable");
  for (const [condition, target] of Object.entries(value))
    exportTargets(target, entries, condition === "types" ? "types" : kind);
}

function fieldTarget(name, value, entries, kind) {
  if (typeof value !== "string")
    throw new Error(`package ${name} must name a file`);
  exportTargets(value.startsWith("./") ? value : `./${value}`, entries, kind);
}

function executableEntries(manifest, entries) {
  if (manifest.bin === undefined) return;
  if (
    manifest.bin === null ||
    !["string", "object"].includes(typeof manifest.bin)
  )
    throw new Error("package bin is not readable");
  const targets =
    typeof manifest.bin === "string"
      ? [manifest.bin]
      : Object.values(manifest.bin);
  for (const target of targets)
    fieldTarget("bin", target, entries, "executables");
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
  const entries = { runtime: [], types: [], main: [], executables: [] };
  if (manifest.exports !== undefined)
    exportTargets(manifest.exports, entries, "runtime");
  for (const [field, kind] of [
    ["main", "main"],
    ["module", "runtime"],
    ["types", "types"],
    ["typings", "types"],
  ])
    if (manifest[field] !== undefined)
      fieldTarget(field, manifest[field], entries, kind);
  executableEntries(manifest, entries);
  rejectUnmodeledMappings(manifest);
  if (Object.values(entries).every((list) => list.length === 0))
    throw new Error(
      "package has no explicit published entry files; declare exports, main/types, or bin before claiming bundled dependencies",
    );
  return entries;
}

function containedArtifact(state, path) {
  if (
    normalize(path) !== path ||
    path.includes("\\") ||
    path.startsWith("/") ||
    path === ".." ||
    path.startsWith("../")
  ) {
    throw new Error(`artifact path is not contained: ${path}`);
  }
  const real = realpathSync(join(state.request.root, path));
  if (insideRoot(state.packageRoot, real.split("\\").join("/")) === null)
    throw new Error(`artifact leaves its package: ${path}`);
  return path;
}

function enqueue(state, path) {
  const contained = containedArtifact(state, path);
  if (!state.pending.has(contained)) state.pending.add(contained);
  if (state.pending.size >= MAXIMUM_FILES)
    throw new Error(`published artifact graph exceeds ${MAXIMUM_FILES} files`);
  return contained;
}

function localCandidate(from, specifier) {
  return normalize(join(dirname(from), specifier).split("\\").join("/"));
}

function missingArtifact(from, specifier) {
  return new Error(
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

function packagePath(entry) {
  return normalize(entry.split("\\").join("/"));
}

function packageArtifact(state, absolute) {
  const relative = insideRoot(
    state.packageRoot,
    realpathSync(absolute).split("\\").join("/"),
  );
  if (relative === null)
    throw new Error(`published import leaves its package: ${absolute}`);
  return packagePath(relative);
}

function existingFile(path) {
  try {
    return statSync(path).isFile();
  } catch {
    return false;
  }
}

function nodeResolution(mode, importer, specifier) {
  if (mode === "require") return createRequire(importer).resolve(specifier);
  const url = new URL(
    import.meta.resolve(specifier, pathToFileURL(importer).href),
  );
  if (url.protocol !== "file:")
    throw new Error(`resolves to a ${url.protocol} URL`);
  return fileURLToPath(url);
}

function resolvedArtifact(state, target) {
  const relative = insideRoot(state.packageRoot, target.split("\\").join("/"));
  if (relative === null) return null;
  if (!existingFile(target)) throw new Error("names no packed file");
  return enqueue(state, packageArtifact(state, target));
}

function localFailure(state, error) {
  return (
    typeof error?.path === "string" &&
    insideRoot(state.packageRoot, error.path.split("\\").join("/")) !== null
  );
}

function externalPackage(state, kind, specifier, error) {
  const name = specifiedPackage(specifier);
  return kind === "package" &&
    name !== state.manifest.name &&
    (error === undefined ||
      (NOT_FOUND.has(error.code) && !localFailure(state, error)))
    ? { packages: [name] }
    : null;
}

function nodeTarget(state, path, specifier, mode, kind) {
  let target;
  try {
    target = nodeResolution(mode, join(state.request.root, path), specifier);
  } catch (error) {
    const external = externalPackage(state, kind, specifier, error);
    if (external !== null) return external;
    throw new Error(
      `published import ${JSON.stringify(specifier)} from ${path} does not resolve: ${error.code ?? error.message}`,
    );
  }
  const resolved = resolvedArtifact(state, target);
  if (resolved !== null) return { resolved };
  const external = externalPackage(state, kind, specifier);
  if (external !== null) return external;
  throw new Error(
    `published import ${JSON.stringify(specifier)} from ${path} resolves outside the packed package`,
  );
}

function typesCounterpart(name) {
  if (name.startsWith("@types/")) return name;
  return `@types/${name.startsWith("@") ? name.slice(1).replace("/", "__") : name}`;
}

function specifierKind(specifier) {
  if (RELATIVE_SPECIFIER.test(specifier)) return "relative";
  if (isBuiltin(specifier)) return "builtin";
  if (!URL_SPECIFIER.test(specifier) && specifiedPackage(specifier) !== "")
    return "package";
  throw new Error(
    `published import ${JSON.stringify(specifier)} is not a relative path, Node built-in, or package name`,
  );
}

function builtinTypePackages(specifier) {
  if (specifier.startsWith("node:")) return ["", "@types/node"];
  const name = specifier.split("/")[0];
  return ["", "@types/node", name, typesCounterpart(name)];
}

function recordFacts(state, source, position, reference) {
  const location = source.getLineAndCharacterOfPosition(position);
  for (const identity of new Set(reference.packages ?? [""]))
    state.imports.push({
      path: reference.path,
      line: location.line + 1,
      column: location.character + 1,
      specifier: reference.specifier,
      resolved: reference.resolved ?? "",
      package: identity,
      kind: reference.kind,
    });
  if (state.imports.length > MAXIMUM_IMPORTS)
    throw new Error(`published imports exceed ${MAXIMUM_IMPORTS} facts`);
}

function runtimeTarget(state, path, specifier, mode) {
  const kind = specifierKind(specifier);
  if (NODE_LOADER_MODULES.has(specifier))
    throw new Error(
      "published artifact imports the Node module loader API; its loaded packages cannot be verified",
    );
  if (kind === "builtin") return {};
  return nodeTarget(state, path, specifier, mode, kind);
}

function runtimeSource(path, text) {
  const extension = extname(path);
  if (TYPE_EXTENSIONS.has(extension))
    throw new Error("TypeScript source cannot run from a published package");
  if (!RUNTIME_EXTENSIONS.has(extension))
    throw new Error(
      "published artifact is not JavaScript, TypeScript, JSON, or CSS",
    );
  const source = typescript.createSourceFile(
    path,
    text,
    typescript.ScriptTarget.Latest,
    true,
    typescript.ScriptKind.JS,
  );
  if (source.parseDiagnostics.length > 0)
    throw new Error("published artifact has invalid JavaScript syntax");
  return source;
}

function requireCall(node) {
  return (
    typescript.isCallExpression(node.parent) &&
    typescript.isIdentifier(node.parent.expression) &&
    node.parent.expression.text === "require"
  );
}

function runtimeReferences(path, text, source) {
  const { specifiers, computed } = sourceSpecifiers(source);
  if (computed.length > 0)
    throw new Error("published artifact has a computed dynamic import");
  const references = new Map();
  for (const { node, kind } of specifiers)
    references.set(node.getStart(source), {
      text: node.text,
      kind,
      mode: requireCall(node) ? "require" : "import",
    });
  for (const loaded of loaderSpecifiers(path, text))
    if (!references.has(loaded.position))
      references.set(loaded.position, loaded);
  return references;
}

function readRuntimeArtifact(state, path) {
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
  const source = runtimeSource(path, text);
  const references = runtimeReferences(path, text, source);
  state.analyzed.push(path);
  for (const [position, { text: specifier, kind, mode }] of references)
    recordFacts(state, source, position, {
      path,
      specifier,
      kind,
      ...runtimeTarget(state, path, specifier, mode),
    });
}

function typeImportTarget(state, specifier, kind, outcome) {
  if (outcome !== null) return { resolved: packageArtifact(state, outcome) };
  if (kind === "relative") throw new Error("has no contained artifact");
  if (kind === "builtin") return { packages: builtinTypePackages(specifier) };
  const name = specifiedPackage(specifier);
  return { packages: [name, typesCounterpart(name)] };
}

function typeImportTargets(state, specifier, outcomes) {
  const kind = specifierKind(specifier);
  return [...new Set(outcomes ?? [null])].map((outcome) =>
    typeImportTarget(state, specifier, kind, outcome),
  );
}

function typeReferencePackages(specifier) {
  const name =
    specifiedPackage(specifier) ||
    (isBuiltin(specifier) ? specifier.split("/")[0] : "");
  if (name !== specifier || URL_SPECIFIER.test(specifier))
    throw new Error(
      `type reference ${JSON.stringify(specifier)} must name a type package`,
    );
  return [name, typesCounterpart(name)];
}

function referencedFile(state, path, specifier) {
  if (specifier.startsWith("/") || URL_SPECIFIER.test(specifier))
    throw new Error(
      `reference path ${JSON.stringify(specifier)} must name a relative file`,
    );
  const candidate = localCandidate(path, specifier);
  const file = [
    candidate,
    `${candidate}.ts`,
    `${candidate}.tsx`,
    `${candidate}.d.ts`,
  ].find((option) => {
    const info = optionalArtifactInfo(state.request.root, option);
    return info && !info.isDirectory();
  });
  if (!file) throw missingArtifact(path, specifier);
  return file;
}

function typeDirectives(state, path, source) {
  for (const directive of source.referencedFiles)
    recordFacts(state, source, directive.pos, {
      path,
      specifier: directive.fileName,
      kind: "type-only",
      resolved: referencedFile(state, path, directive.fileName),
    });
  for (const directive of source.typeReferenceDirectives)
    recordFacts(state, source, directive.pos, {
      path,
      specifier: directive.fileName,
      kind: "type-only",
      packages: typeReferencePackages(directive.fileName),
    });
}

function readTypeArtifact(state, path, { source, resolutions }) {
  if (
    readTargetFile(join(state.request.root, path), path, state.unsupported) ===
    null
  )
    return;
  if (source.parseDiagnostics.length > 0)
    throw new Error("published artifact has invalid TypeScript syntax");
  const { specifiers, computed } = sourceSpecifiers(source);
  if (computed.length > 0)
    throw new Error("published artifact has a computed dynamic import");
  state.analyzed.push(path);
  for (const { node, kind } of specifiers) {
    const position = node.getStart(source);
    let targets;
    try {
      targets = typeImportTargets(state, node.text, resolutions.get(position));
    } catch (error) {
      throw new Error(
        `published import ${JSON.stringify(node.text)} from ${path} ${error.message}`,
      );
    }
    for (const target of targets)
      recordFacts(state, source, position, {
        path,
        specifier: node.text,
        kind,
        ...target,
      });
  }
  typeDirectives(state, path, source);
}

function readTypeArtifacts(state, entries) {
  const { files, resolvedEntries } = typeClosure(state.packageRoot, [
    ...entries.runtime,
    ...entries.main,
    ...entries.types,
  ]);
  for (const entry of entries.types)
    if (!resolvedEntries.has(entry))
      state.unsupported.push(
        unsupported(MANIFEST, `package type entry ${entry} does not resolve`),
      );
  if (files.size + state.pending.size > MAXIMUM_FILES)
    throw new Error(`published artifact graph exceeds ${MAXIMUM_FILES} files`);
  for (const [fileName, record] of files) {
    const path = packageArtifact(state, fileName);
    try {
      containedArtifact(state, path);
      readTypeArtifact(state, path, record);
    } catch (error) {
      state.unsupported.push(unsupported(path, error.message));
    }
  }
}

function enqueueEntry(state, entry) {
  return enqueue(state, packagePath(entry));
}

function stagedConsumer(state) {
  const depth = String(state.manifest.name).split("/").length;
  const modules = join(state.packageRoot, ...Array(depth).fill(".."));
  if (basename(modules) !== "node_modules")
    throw new Error("the packed package is not staged as an installed package");
  return join(dirname(modules), "consumer.js");
}

function enqueueNodeEntries(state, manifest) {
  const consumer = stagedConsumer(state);
  for (const mode of ["require", "import"]) {
    try {
      resolvedArtifact(state, nodeResolution(mode, consumer, manifest.name));
    } catch (error) {
      if (error.code === undefined) throw error;
    }
  }
  if (manifest.main === undefined) return;
  let target;
  try {
    target = createRequire(join(state.request.root, MANIFEST)).resolve("./");
  } catch (error) {
    throw new Error(`package main does not resolve: ${error.code}`);
  }
  if (resolvedArtifact(state, target) === null)
    throw new Error("package main resolves outside the packed package");
}

function packageRoots(state, manifest) {
  const entries = packageEntries(manifest);
  for (const entry of [...entries.runtime, ...entries.executables])
    enqueueEntry(state, entry);
  enqueueNodeEntries(state, manifest);
  return entries;
}

export function packageImports(request) {
  if (request.paths.length !== 1 || request.paths[0] !== MANIFEST)
    fail("package-imports requires the packed package manifest");
  const state = {
    request,
    packageRoot: realpathSync(request.root).split("\\").join("/"),
    analyzed: [],
    imports: [],
    unsupported: [],
    pending: new Set(),
  };
  try {
    containedArtifact(state, MANIFEST);
    const text = readTargetFile(
      join(request.root, MANIFEST),
      MANIFEST,
      state.unsupported,
    );
    if (text === null)
      return { analyzed: [], imports: [], unsupported: state.unsupported };
    state.manifest = JSON.parse(text);
    const entries = packageRoots(state, state.manifest);
    state.analyzed.push(MANIFEST);
    for (const path of state.pending) {
      if (path === MANIFEST) continue;
      try {
        readRuntimeArtifact(state, path);
      } catch (error) {
        state.unsupported.push(unsupported(path, error.message));
      }
    }
    readTypeArtifacts(state, entries);
  } catch (error) {
    state.unsupported.push(unsupported(MANIFEST, error.message));
  }
  return {
    analyzed: state.analyzed,
    imports: state.imports,
    unsupported: state.unsupported,
  };
}
