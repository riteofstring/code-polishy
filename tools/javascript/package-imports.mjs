import { lstatSync, realpathSync } from "node:fs";
import { createRequire, isBuiltin } from "node:module";
import { basename, dirname, extname, join } from "node:path";
import { normalize } from "node:path/posix";

import typescript from "typescript";

import { sourceSpecifiers, specifiedPackage } from "./imports.mjs";
import { loaderSpecifiers } from "./package-loaders.mjs";
import { typeClosure } from "./package-types.mjs";
import { fail, insideRoot, readTargetFile, unsupported } from "./protocol.mjs";

const MAXIMUM_FILES = 4096;
const MAXIMUM_IMPORTS = 20000;
const RUNTIME_EXTENSIONS = new Set(["", ".js", ".mjs", ".cjs"]);
const OPAQUE_EXTENSIONS = new Set([".json", ".css"]);
const TYPE_EXTENSIONS = new Set([".ts", ".mts", ".cts", ".tsx"]);
const RELATIVE_SPECIFIER = /^\.\.?(?:\/|$)/;
const URL_SPECIFIER = /^[A-Za-z][A-Za-z0-9+.-]*:/;
const NODE_LOADER_MODULES = new Set(["module", "node:module"]);

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

function packagePath(state, entry) {
  return normalize(join(state.directory, entry).split("\\").join("/"));
}

function packageArtifact(state, absolute) {
  const relative = insideRoot(
    state.packageRoot,
    realpathSync(absolute).split("\\").join("/"),
  );
  if (relative === null)
    throw new Error(`published import leaves its package: ${absolute}`);
  return packagePath(state, relative);
}

function runtimeArtifact(state, from, specifier) {
  let target;
  try {
    target = createRequire(join(state.request.root, from)).resolve(specifier);
  } catch {
    throw missingArtifact(from, specifier);
  }
  return enqueue(state, packageArtifact(state, target));
}

function selfSubpath(state, specifier) {
  const name = state.manifest?.name;
  if (typeof name !== "string" || specifiedPackage(specifier) !== name)
    return null;
  return `.${specifier.slice(name.length)}`;
}

function exportedSubpaths(exports) {
  if (
    exports === null ||
    typeof exports !== "object" ||
    Array.isArray(exports) ||
    !Object.keys(exports).some((key) => key.startsWith("."))
  )
    return ["."];
  return Object.keys(exports);
}

function selfArtifact(state, from, subpath) {
  if (state.manifest.exports !== undefined) {
    if (!exportedSubpaths(state.manifest.exports).includes(subpath))
      throw new Error(
        `published self-import ${JSON.stringify(subpath)} from ${from} is not an exact package export`,
      );
    return "";
  }
  if (subpath === ".") return "";
  return runtimeArtifact(
    state,
    packagePath(state, basename(from)),
    `.${subpath.slice(1)}`,
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

function runtimeTarget(state, path, specifier) {
  const kind = specifierKind(specifier);
  if (NODE_LOADER_MODULES.has(specifier))
    throw new Error(
      "published artifact imports the Node module loader API; its loaded packages cannot be verified",
    );
  if (kind === "relative")
    return { resolved: runtimeArtifact(state, path, specifier) };
  if (kind === "builtin") return {};
  const self = selfSubpath(state, specifier);
  if (self !== null) return { resolved: selfArtifact(state, path, self) };
  return { packages: [specifiedPackage(specifier)] };
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

function runtimeReferences(path, text, source) {
  const { specifiers, computed } = sourceSpecifiers(source);
  if (computed.length > 0)
    throw new Error("published artifact has a computed dynamic import");
  const references = new Map();
  for (const { node, kind } of specifiers)
    references.set(node.getStart(source), { text: node.text, kind });
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
  for (const [position, { text: specifier, kind }] of references)
    recordFacts(state, source, position, {
      path,
      specifier,
      kind,
      ...runtimeTarget(state, path, specifier),
    });
}

function typeImportTarget(state, specifier, targets) {
  const kind = specifierKind(specifier);
  if (targets !== undefined)
    return { resolved: packageArtifact(state, [...targets][0]) };
  if (kind === "relative") throw new Error("has no contained artifact");
  if (kind === "builtin") return { packages: builtinTypePackages(specifier) };
  const name = specifiedPackage(specifier);
  return { packages: [name, typesCounterpart(name)] };
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
    let target;
    try {
      target = typeImportTarget(state, node.text, resolutions.get(position));
    } catch (error) {
      throw new Error(
        `published import ${JSON.stringify(node.text)} from ${path} ${error.message}`,
      );
    }
    recordFacts(state, source, position, {
      path,
      specifier: node.text,
      kind,
      ...target,
    });
  }
  typeDirectives(state, path, source);
}

function readTypeArtifacts(state, roots, entries) {
  const files = typeClosure(
    state.packageRoot,
    state.manifest.name,
    roots.map((path) => realpathSync(join(state.request.root, path))),
    entries,
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
  return enqueue(state, packagePath(state, entry));
}

function enqueueRuntimeDirectory(state, manifest) {
  if (manifest.exports !== undefined) return;
  let target;
  try {
    target = createRequire(
      join(state.request.root, state.manifestPath),
    ).resolve("./");
  } catch {
    return;
  }
  enqueue(state, packageArtifact(state, target));
}

function packageRoots(state, manifest) {
  const { entries, executables } = packageEntries(manifest);
  const typed = (entry) => TYPE_EXTENSIONS.has(extname(entry));
  for (const entry of [
    ...entries.filter((entry) => !typed(entry)),
    ...executables,
  ])
    enqueueEntry(state, entry);
  enqueueRuntimeDirectory(state, manifest);
  const roots = entries
    .filter(typed)
    .map((entry) => containedArtifact(state, packagePath(state, entry)));
  return { roots, entries };
}

export function packageImports(request) {
  if (request.paths.length !== 1 || !request.paths[0].endsWith("package.json"))
    fail("package-imports requires exactly one package manifest");
  const manifestPath = request.paths[0];
  const directory = dirname(manifestPath).split("\\").join("/");
  const state = {
    request,
    manifestPath,
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
    state.manifest = JSON.parse(text);
    const { roots, entries } = packageRoots(state, state.manifest);
    state.analyzed.push(manifestPath);
    for (const path of state.pending) {
      if (path === manifestPath) continue;
      try {
        readRuntimeArtifact(state, path);
      } catch (error) {
        state.unsupported.push(unsupported(path, error.message));
      }
    }
    readTypeArtifacts(state, roots, entries);
  } catch (error) {
    state.unsupported.push(unsupported(manifestPath, error.message));
  }
  return {
    analyzed: state.analyzed,
    imports: state.imports,
    unsupported: state.unsupported,
  };
}
