import { lstatSync, realpathSync } from "node:fs";
import { createRequire, isBuiltin } from "node:module";
import { basename, dirname, extname, join } from "node:path";
import { normalize } from "node:path/posix";

import typescript from "typescript";

import {
  containedResolutionHost,
  sourceSpecifiers,
  specifiedPackage,
} from "./imports.mjs";
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
const TYPE_EXTENSIONS = new Set([".ts", ".mts", ".cts", ".tsx"]);
const DECLARATION_FILE = /\.d\.[cm]?ts$/;
const TYPE_RESOLUTIONS = [
  [{ moduleResolution: typescript.ModuleResolutionKind.Node10 }, undefined],
  [
    {
      module: typescript.ModuleKind.NodeNext,
      moduleResolution: typescript.ModuleResolutionKind.NodeNext,
    },
    typescript.ModuleKind.ESNext,
  ],
  [
    {
      module: typescript.ModuleKind.NodeNext,
      moduleResolution: typescript.ModuleResolutionKind.NodeNext,
    },
    typescript.ModuleKind.CommonJS,
  ],
  [
    {
      module: typescript.ModuleKind.ESNext,
      moduleResolution: typescript.ModuleResolutionKind.Bundler,
    },
    undefined,
  ],
];
const NODE_LOADER_MODULES = new Set(["module", "node:module"]);
const LOADER_NAMES = new Set(["require", "getBuiltinModule"]);
const REQUIRE_PROPERTIES = new Set(["main", "cache"]);
const ORDINARY = "ordinary";
const UNSUPPORTED = "unsupported";
const EXPRESSION_WRAPPERS = new Set([
  typescript.SyntaxKind.ParenthesizedExpression,
  typescript.SyntaxKind.AsExpression,
  typescript.SyntaxKind.SatisfiesExpression,
  typescript.SyntaxKind.NonNullExpression,
  typescript.SyntaxKind.TypeAssertionExpression,
]);
const NAME_DECLARATIONS = new Set([
  typescript.SyntaxKind.Parameter,
  typescript.SyntaxKind.VariableDeclaration,
  typescript.SyntaxKind.FunctionDeclaration,
  typescript.SyntaxKind.FunctionExpression,
  typescript.SyntaxKind.ClassDeclaration,
  typescript.SyntaxKind.ClassExpression,
  typescript.SyntaxKind.PropertyDeclaration,
  typescript.SyntaxKind.MethodDeclaration,
  typescript.SyntaxKind.GetAccessor,
  typescript.SyntaxKind.SetAccessor,
  typescript.SyntaxKind.PropertySignature,
  typescript.SyntaxKind.MethodSignature,
  typescript.SyntaxKind.EnumMember,
  typescript.SyntaxKind.ImportClause,
  typescript.SyntaxKind.NamespaceImport,
  typescript.SyntaxKind.ImportEqualsDeclaration,
  typescript.SyntaxKind.InterfaceDeclaration,
  typescript.SyntaxKind.TypeAliasDeclaration,
  typescript.SyntaxKind.TypeParameter,
  typescript.SyntaxKind.ModuleDeclaration,
  typescript.SyntaxKind.JsxAttribute,
]);
const LABEL_STATEMENTS = new Set([
  typescript.SyntaxKind.LabeledStatement,
  typescript.SyntaxKind.BreakStatement,
  typescript.SyntaxKind.ContinueStatement,
]);

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

function existingFile(state, candidates) {
  return candidates.find((path) => {
    const info = optionalArtifactInfo(state.request.root, path);
    return info && !info.isDirectory();
  });
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

function referencedArtifact(state, from, specifier) {
  const candidate = localCandidate(from, specifier);
  const file = existingFile(state, [
    candidate,
    `${candidate}.ts`,
    `${candidate}.tsx`,
    `${candidate}.d.ts`,
  ]);
  if (file) return enqueue(state, file);
  throw missingArtifact(from, specifier);
}

function typeResolutions(state, from, specifier) {
  const host = containedResolutionHost(state.request.root);
  const containing = join(state.request.root, from);
  const resolved = new Set();
  for (const [options, mode] of TYPE_RESOLUTIONS) {
    const target = typescript.resolveModuleName(
      specifier,
      containing,
      options,
      host,
      undefined,
      undefined,
      mode,
    ).resolvedModule;
    if (target) resolved.add(packageArtifact(state, target.resolvedFileName));
  }
  return resolved;
}

function typeArtifact(state, from, specifier) {
  const resolved = typeResolutions(state, from, specifier);
  if (resolved.size === 0) throw missingArtifact(from, specifier);
  return [...resolved].map((path) => enqueue(state, path))[0];
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

function localArtifact(state, from, specifier, reference) {
  if (reference) return referencedArtifact(state, from, specifier);
  if (TYPE_EXTENSIONS.has(extname(from)))
    return typeArtifact(state, from, specifier);
  return runtimeArtifact(state, from, specifier);
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
  return localArtifact(
    state,
    packagePath(state, basename(from)),
    `.${subpath.slice(1)}`,
    false,
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

function rejectLoaderModule(path, specifier, kind) {
  if (
    NODE_LOADER_MODULES.has(specifier) &&
    kind !== "type-only" &&
    !DECLARATION_FILE.test(path)
  )
    throw new Error(
      "published artifact imports the Node module loader API; its loaded packages cannot be verified",
    );
}

function typesCounterpart(name) {
  if (name.startsWith("@types/")) return name;
  return `@types/${name.startsWith("@") ? name.slice(1).replace("/", "__") : name}`;
}

function typeIdentities(path, specifier) {
  if (!TYPE_EXTENSIONS.has(extname(path))) return [];
  const builtin = isBuiltin(specifier);
  const identities = builtin ? ["@types/node"] : [];
  if (specifier.startsWith("node:")) return identities;
  const name = builtin ? specifier.split("/")[0] : specifiedPackage(specifier);
  return name === ""
    ? identities
    : [...identities, name, typesCounterpart(name)];
}

function resolvedReference(state, path, specifier, reference) {
  if (specifier.startsWith("."))
    return localArtifact(state, path, specifier, reference);
  if (/^[/#]/.test(specifier))
    throw new Error(
      `published import ${JSON.stringify(specifier)} needs explicit artifact resolution`,
    );
  const self = selfSubpath(state, specifier);
  return self === null ? null : selfArtifact(state, path, self);
}

function artifactReference(state, source, reference, position, kind) {
  const { specifier, file = false } = reference;
  const path = source.fileName;
  const location = source.getLineAndCharacterOfPosition(position);
  rejectLoaderModule(path, specifier, kind);
  const local = resolvedReference(state, path, specifier, file);
  const resolved = local ?? "";
  const fact = {
    path,
    line: location.line + 1,
    column: location.character + 1,
    specifier,
    resolved,
    package: specifiedPackage(specifier),
    kind,
  };
  state.imports.push(fact);
  const identities = local === null ? typeIdentities(path, specifier) : [];
  for (const identity of new Set(identities))
    if (identity !== fact.package)
      state.imports.push({ ...fact, package: identity });
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
  specifiers.push(...loaderSpecifiers(source));
  state.analyzed.push(path);
  for (const { node, kind } of specifiers)
    artifactReference(
      state,
      source,
      { specifier: node.text },
      node.getStart(source),
      kind,
    );
  for (const directive of source.referencedFiles)
    artifactReference(
      state,
      source,
      {
        specifier: directive.fileName.startsWith(".")
          ? directive.fileName
          : `./${directive.fileName}`,
        file: true,
      },
      directive.pos,
      "type-only",
    );
  for (const directive of source.typeReferenceDirectives)
    artifactReference(
      state,
      source,
      { specifier: directive.fileName },
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

function enqueuePackageEntries(state, manifest) {
  const { entries, executables } = packageEntries(manifest);
  for (const entry of [...entries, ...executables]) enqueueEntry(state, entry);
  enqueueRuntimeDirectory(state, manifest);
  for (const entry of [...entries, "./"])
    for (const path of typeResolutions(state, state.manifestPath, entry))
      enqueue(state, path);
}

function unwrapped(node) {
  let outer = node;
  while (
    EXPRESSION_WRAPPERS.has(outer.parent.kind) &&
    outer.parent.expression === outer
  )
    outer = outer.parent;
  return outer;
}

function calledBy(node) {
  const outer = unwrapped(node);
  const parent = outer.parent;
  return typescript.isCallExpression(parent) && parent.expression === outer
    ? parent
    : null;
}

function memberKeyRole(parent, key) {
  if (
    (typescript.isPropertyAccessExpression(parent) && parent.name === key) ||
    (typescript.isElementAccessExpression(parent) &&
      parent.argumentExpression === key)
  )
    return calledBy(parent) ?? UNSUPPORTED;
  return null;
}

function specifierKeyRole(parent, key) {
  if (typescript.isExportSpecifier(parent)) return UNSUPPORTED;
  if (typescript.isImportSpecifier(parent))
    return parent.propertyName === key ? UNSUPPORTED : ORDINARY;
  if (typescript.isBindingElement(parent))
    return parent.propertyName === key ? UNSUPPORTED : ORDINARY;
  return null;
}

function objectKeyRole(parent, key) {
  if (typescript.isShorthandPropertyAssignment(parent))
    return typescript.isAssignmentTarget(parent.parent)
      ? ORDINARY
      : UNSUPPORTED;
  if (typescript.isPropertyAssignment(parent) && parent.name === key)
    return typescript.isAssignmentTarget(parent.parent)
      ? UNSUPPORTED
      : ORDINARY;
  return null;
}

function declarationKeyRole(parent, key) {
  if (NAME_DECLARATIONS.has(parent.kind) && parent.name === key)
    return ORDINARY;
  if (LABEL_STATEMENTS.has(parent.kind) && parent.label === key)
    return ORDINARY;
  return null;
}

function keyRole(key) {
  const parent = key.parent;
  return (
    memberKeyRole(parent, key) ??
    specifierKeyRole(parent, key) ??
    objectKeyRole(parent, key) ??
    declarationKeyRole(parent, key)
  );
}

function valueRole(identifier) {
  const call = calledBy(identifier);
  if (call) return call;
  const outer = unwrapped(identifier);
  const parent = outer.parent;
  if (typescript.isTypeOfExpression(parent)) return ORDINARY;
  if (
    identifier.text !== "require" ||
    !typescript.isPropertyAccessExpression(parent) ||
    parent.expression !== outer
  )
    return UNSUPPORTED;
  if (REQUIRE_PROPERTIES.has(parent.name.text)) return ORDINARY;
  return (parent.name.text === "resolve" && calledBy(parent)) || UNSUPPORTED;
}

function identifierRole(identifier) {
  if (typescript.isPartOfTypeNode(identifier)) return ORDINARY;
  return keyRole(identifier) ?? valueRole(identifier);
}

function stringRole(literal) {
  const outer = unwrapped(literal);
  const key = typescript.isComputedPropertyName(outer.parent)
    ? outer.parent
    : outer;
  return keyRole(key) ?? ORDINARY;
}

function loaderRole(node) {
  if (typescript.isIdentifier(node) && LOADER_NAMES.has(node.text))
    return identifierRole(node);
  if (typescript.isStringLiteralLike(node) && LOADER_NAMES.has(node.text))
    return stringRole(node);
  return ORDINARY;
}

function loaderCall(node, specifiers) {
  if (!typescript.isStringLiteral(node.arguments[0] ?? node))
    throw new Error("published artifact has a computed loader call");
  if (
    !typescript.isIdentifier(node.expression) ||
    node.expression.text !== "require"
  )
    specifiers.push({ node: node.arguments[0], kind: "runtime" });
}

function loaderSpecifiers(source) {
  const specifiers = [];
  if (DECLARATION_FILE.test(source.fileName)) return specifiers;
  const visit = (node) => {
    const role = loaderRole(node);
    if (role === UNSUPPORTED)
      throw new Error(
        "published artifact uses require or getBuiltinModule outside a supported loader call",
      );
    if (role !== ORDINARY) loaderCall(role, specifiers);
    typescript.forEachChild(node, visit);
  };
  visit(source);
  return specifiers;
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
    enqueuePackageEntries(state, state.manifest);
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
