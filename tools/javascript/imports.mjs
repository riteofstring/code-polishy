// The import facts of one selection of governed source, for the sealed,
// policy-owned JavaScript tool bundle.
//
// Go enforces module direction and which package may reach which dependency.
// This operation decides nothing about whether an edge is allowed: it reports
// what each selected file imports, which file inside the target tree a
// specifier names, and which external package it reaches.
//
// Which file a specifier names is the ecosystem mechanic Code Polishy will not
// approximate in Go. That `./helper.js` names helper.ts in a TypeScript
// package, that a workspace specifier links to a sibling package's source, and
// that a package's exports map selects one entry of several are all
// TypeScript's answers rather than a path rewrite Go could guess.

import { builtinModules } from "node:module";
import { extname, join } from "node:path";
// Resolved relative to this file rather than as a bare specifier, so the
// analyzer can only ever be the one installed beside the runner.
import typescript from "./node_modules/typescript/lib/typescript.js";

import {
  containedRead,
  fail,
  insideRoot,
  readTargetFile,
  truncate,
  unsupported,
} from "./protocol.mjs";

// One imports exchange reports bounded facts. A selection whose source declares
// more edges than this has a systemic problem Go should be told about as a
// failed operation rather than handed a truncated answer that looks complete.
const MAXIMUM_IMPORT_FACTS = 20000;

// The modules the runtime itself provides. A specifier naming one names no
// installed package, and which names those are belongs to the pinned Node
// rather than to a list Go would have to keep in step with it.
const RUNTIME_MODULES = new Set(builtinModules);

// The npm package-name grammar. Text that is not a package name -- a project's
// own path alias, a URL, a data specifier -- names no package, rather than a
// package the target could only ever fail to declare.
const PACKAGE_NAME = /^(?:@[a-z0-9~-][a-z0-9._~-]*\/)?[a-z0-9~-][a-z0-9._~-]*$/;

// The longest name the registry publishes, so a reported package name is
// bounded by the ecosystem's own limit rather than by the length of whatever a
// source file happened to write.
const MAXIMUM_PACKAGE_NAME_CHARACTERS = 214;

// How each governed source file is parsed, by its name alone. A file type that
// is not here is reported rather than parsed under a guessed language.
const IMPORT_LANGUAGES = {
  ".cjs": typescript.ScriptKind.JS,
  ".js": typescript.ScriptKind.JS,
  ".jsx": typescript.ScriptKind.JSX,
  ".mjs": typescript.ScriptKind.JS,
  ".cts": typescript.ScriptKind.TS,
  ".mts": typescript.ScriptKind.TS,
  ".ts": typescript.ScriptKind.TS,
  ".tsx": typescript.ScriptKind.TSX,
};

// The settings every specifier is resolved under. A target's own project decides
// how its compiler runs; it does not decide which file a written specifier
// names, so these are Code Polishy's and are the most permissive reading of a
// specifier: an edge missed here would be a module boundary crossed silently.
const RESOLUTION_OPTIONS = {
  allowImportingTsExtensions: true,
  allowJs: true,
  module: typescript.ModuleKind.ESNext,
  moduleResolution: typescript.ModuleResolutionKind.Bundler,
  resolveJsonModule: true,
  target: typescript.ScriptTarget.ESNext,
};

// The host every specifier is resolved through. It reads only what is really
// inside the target tree, so a resolution that would climb above the repository
// or follow a link out of it finds nothing rather than reading another tree.
// Resolving to a file the target does not contain is not a failure: an external
// package is exactly that.
function containedResolutionHost(root) {
  return {
    useCaseSensitiveFileNames: true,
    getCurrentDirectory: () => root,
    fileExists: (path) =>
      typescript.sys.fileExists(path) && containedRead(path),
    directoryExists: (path) =>
      typescript.sys.directoryExists(path) && containedRead(path),
    getDirectories: (path) =>
      containedRead(path) ? typescript.sys.getDirectories(path) : [],
    readFile: (path) =>
      containedRead(path) ? typescript.sys.readFile(path) : undefined,
    // A workspace package is reached through a link, so the file a specifier
    // names is the file the link points at rather than the link itself.
    realpath: (path) => typescript.sys.realpath(path),
  };
}

// One written specifier, or nothing when the node does not write one.
function literalSpecifier(node) {
  return node !== undefined && typescript.isStringLiteral(node) ? node : null;
}

// Every specifier one node writes, and every dynamic import that writes none.
function collectSpecifiers(node, specifiers, computed) {
  const declared = declaredSpecifier(node);
  if (declared !== null) {
    specifiers.push(declared);
    return;
  }
  if (typescript.isCallExpression(node)) {
    collectCalledSpecifier(node, specifiers, computed);
  }
}

// The specifier a declaration writes: an import, a re-export, an import-equals
// external reference, or a type position.
function declaredSpecifier(node) {
  if (
    typescript.isImportDeclaration(node) ||
    typescript.isExportDeclaration(node)
  ) {
    return literalSpecifier(node.moduleSpecifier);
  }
  if (
    typescript.isImportEqualsDeclaration(node) &&
    typescript.isExternalModuleReference(node.moduleReference)
  ) {
    return literalSpecifier(node.moduleReference.expression);
  }
  if (
    typescript.isImportTypeNode(node) &&
    typescript.isLiteralTypeNode(node.argument)
  ) {
    return literalSpecifier(node.argument.literal);
  }
  return null;
}

// A dynamic import is an import; one whose specifier is an expression names no
// file to resolve, so it is collected as computed rather than dropped, because
// a module boundary must not be crossable by computing the name of what is on
// the far side of it. A literal require is an import too, but an expression
// handed to something named require is not reported, because require is an
// ordinary identifier a target may bind to anything.
function collectCalledSpecifier(node, specifiers, computed) {
  const written = literalSpecifier(node.arguments[0]);
  if (node.expression.kind === typescript.SyntaxKind.ImportKeyword) {
    if (written === null) {
      computed.push(node);
    } else {
      specifiers.push(written);
    }
    return;
  }
  if (
    written !== null &&
    typescript.isIdentifier(node.expression) &&
    node.expression.text === "require"
  ) {
    specifiers.push(written);
  }
}

function sourceSpecifiers(source) {
  const specifiers = [];
  const computed = [];
  const visit = (node) => {
    collectSpecifiers(node, specifiers, computed);
    typescript.forEachChild(node, visit);
  };
  typescript.forEachChild(source, visit);
  return { specifiers, computed };
}

function nodeLine(source, node) {
  return source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
}

// The external package one specifier names, or nothing when it names none: a
// relative or absolute path, a subpath import the importing manifest resolves
// itself, a module the runtime provides, or text that is no package name at
// all. A subpath of a package is that package: which of its files the subpath
// selects is resolution, and this is only which package was reached.
function specifiedPackage(specifier) {
  if (/^[./#]/.test(specifier) || specifier.startsWith("node:")) {
    return "";
  }
  if (RUNTIME_MODULES.has(specifier)) {
    return "";
  }
  const segments = specifier.split("/");
  const name = specifier.startsWith("@")
    ? segments.slice(0, 2).join("/")
    : segments[0];
  if (
    name.length > MAXIMUM_PACKAGE_NAME_CHARACTERS ||
    !PACKAGE_NAME.test(name)
  ) {
    return "";
  }
  return name;
}

// One import as a bounded fact: a target-relative path, a position, the exact
// specifier the source wrote, the target-relative file it names, and the
// external package it reaches. A specifier naming nothing inside the target
// resolves to no file, which is what an import of an external package, a
// missing dependency, or a build artifact looks like; Go decides what each of
// those means.
function importFact(root, path, source, node, resolved) {
  return {
    path,
    line: nodeLine(source, node),
    specifier: truncate(node.text),
    resolved:
      resolved === undefined
        ? ""
        : (insideRoot(root, resolved.resolvedFileName) ?? ""),
    package: specifiedPackage(node.text),
  };
}

function fileFacts(request, path, resolve, facts, unsupportedPaths) {
  const kind = IMPORT_LANGUAGES[extname(path).toLowerCase()];
  if (kind === undefined) {
    unsupportedPaths.push(
      unsupported(
        path,
        "the policy-owned import reader does not read this file",
      ),
    );
    return;
  }
  const absolute = join(request.root, path);
  const text = readTargetFile(absolute, path, unsupportedPaths);
  if (text === null) {
    return;
  }
  let source;
  let written;
  try {
    source = typescript.createSourceFile(
      absolute,
      text,
      typescript.ScriptTarget.Latest,
      true,
      kind,
    );
    written = sourceSpecifiers(source);
  } catch (error) {
    unsupportedPaths.push(unsupported(path, error.message));
    return;
  }
  for (const node of written.computed) {
    unsupportedPaths.push(
      unsupported(
        path,
        `line ${nodeLine(source, node)}: a dynamic import whose specifier is computed names no file to resolve`,
      ),
    );
  }
  for (const node of written.specifiers) {
    facts.push(
      importFact(
        request.root,
        path,
        source,
        node,
        resolve(node.text, absolute),
      ),
    );
  }
}

// Report every import the selected files declare. Go owns which of them crosses
// a module boundary it did not declare, and what an unread file means.
export function imports(request) {
  const host = containedResolutionHost(request.root);
  const cache = typescript.createModuleResolutionCache(
    request.root,
    (name) => name,
    RESOLUTION_OPTIONS,
  );
  const resolve = (specifier, containingFile) =>
    typescript.resolveModuleName(
      specifier,
      containingFile,
      RESOLUTION_OPTIONS,
      host,
      cache,
    ).resolvedModule;
  const facts = [];
  const unsupportedPaths = [];
  for (const path of request.paths) {
    fileFacts(request, path, resolve, facts, unsupportedPaths);
    if (facts.length > MAXIMUM_IMPORT_FACTS) {
      fail(
        `the imports operation produced more than the ${MAXIMUM_IMPORT_FACTS} result limit`,
      );
    }
  }
  return { imports: facts, unsupported: unsupportedPaths };
}
