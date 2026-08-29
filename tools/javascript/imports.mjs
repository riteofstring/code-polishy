import { builtinModules } from "node:module";
import { extname, join } from "node:path";

import typescript from "./node_modules/typescript/lib/typescript.js";

import {
  containedRead,
  fail,
  insideRoot,
  readTargetFile,
  truncate,
  unsupported,
} from "./protocol.mjs";

const MAXIMUM_IMPORT_FACTS = 20000;

const RUNTIME_MODULES = new Set(builtinModules);

const PACKAGE_NAME = /^(?:@[a-z0-9~-][a-z0-9._~-]*\/)?[a-z0-9~-][a-z0-9._~-]*$/;

const MAXIMUM_PACKAGE_NAME_CHARACTERS = 214;

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

const RESOLUTION_OPTIONS = {
  allowImportingTsExtensions: true,
  allowJs: true,
  module: typescript.ModuleKind.ESNext,
  moduleResolution: typescript.ModuleResolutionKind.Bundler,
  resolveJsonModule: true,
  target: typescript.ScriptTarget.ESNext,
};

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

    realpath: (path) => typescript.sys.realpath(path),
  };
}

function literalSpecifier(node) {
  return node !== undefined && typescript.isStringLiteral(node) ? node : null;
}

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
