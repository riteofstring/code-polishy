import tsParser from "@typescript-eslint/parser";

export function parseJavaScript(source, path) {
  return tsParser.parseForESLint(source, {
    filePath: path,
    loc: true,
    range: true,
    tokens: true,
    comment: true,
    ecmaVersion: "latest",
    sourceType: path.endsWith(".cjs") ? "commonjs" : "module",
    ecmaFeatures: { jsx: true },
  });
}

export function walk(ast, keys, enter, leave = () => {}) {
  function visit(node, parent, depth) {
    if (depth > 1000) throw new Error("source AST exceeds traversal depth");
    if (enter(node, parent) !== false) {
      for (const key of keys[node.type] ?? []) {
        const children = Array.isArray(node[key]) ? node[key] : [node[key]];
        for (const child of children)
          if (child?.type) visit(child, node, depth + 1);
      }
    }
    leave(node, parent);
  }
  visit(ast, null, 0);
}

export function sourceFacts(
  analysis,
  path,
  parsed,
  { offset = 0, comments: extraComments = [], references = [] } = {},
) {
  const source = analysis.read(path);
  const imports = [],
    functions = [];
  const comments = sourceComments(
    analysis,
    path,
    parsed,
    offset,
    extraComments,
  );
  if (["architecture", "dead-code"].includes(analysis.request.capability))
    for (const reference of references) {
      if (reference.problem) throw new Error(reference.problem);
      imports.push({
        path,
        ...analysis.location(path, reference.offset),
        specifier: reference.specifier,
        kind: reference.kind,
      });
    }
  walk(parsed.ast, parsed.visitorKeys, (node, parent) => {
    if (node.type === "AstroHTMLComment")
      comments.push({
        path,
        kind: "HTML",
        raw: source.slice(offset + node.range[0], offset + node.range[1]),
        complete: true,
        ...analysis.location(path, offset + node.range[0]),
        beforeCode: false,
        preamble: false,
        byteZero: false,
      });
    const reference = ["architecture", "dead-code"].includes(
      analysis.request.capability,
    )
      ? importReference(node)
      : null;
    if (reference)
      imports.push({
        path,
        ...analysis.location(path, offset + node.range[0]),
        ...reference,
      });
  });
  return { imports, functions, comments };
}

function importReference(node) {
  if (
    [
      "ImportDeclaration",
      "ExportNamedDeclaration",
      "ExportAllDeclaration",
    ].includes(node.type) &&
    node.source
  ) {
    return declarationReference(node);
  }
  if (isModuleGlob(node)) return globReference(node);
  if (node.type === "TSImportEqualsDeclaration")
    return importEqualsReference(node);
  if (node.type === "TSImportType")
    return literalReference(
      node.argument.type === "TSLiteralType"
        ? node.argument.literal
        : node.argument,
      "type-only",
    );
  if (node.type === "ImportExpression")
    return literalReference(node.source, "proven-dynamic");
  if (isRequire(node)) return literalReference(node.arguments[0], "runtime");
  return null;
}

function importEqualsReference(node) {
  if (node.moduleReference.type !== "TSExternalModuleReference") return null;
  return literalReference(
    node.moduleReference.expression,
    node.importKind === "type" ? "type-only" : "runtime",
  );
}

function isModuleGlob(node) {
  return (
    node.type === "CallExpression" &&
    node.callee.type === "MemberExpression" &&
    node.callee.object.type === "MetaProperty" &&
    node.callee.object.meta.name === "import" &&
    node.callee.property.name === "glob"
  );
}

function globReference(node) {
  if (node.arguments.length > 2)
    throw new Error("module glob has unsupported arguments");
  const reference = literalReference(node.arguments[0], "proven-dynamic");
  const options = node.arguments[1];
  if (
    options &&
    (options.type !== "ObjectExpression" ||
      options.properties.some(unsupportedGlobOption))
  )
    throw new Error("module glob options are not statically supported");
  return { ...reference, glob: true };
}

function unsupportedGlobOption(property) {
  return (
    property.type !== "Property" ||
    !["eager", "import"].includes(property.key.name) ||
    property.value.type !== "Literal"
  );
}

function literalReference(node, kind) {
  if (node?.type !== "Literal" || typeof node.value !== "string")
    throw new Error("computed module load has no statically proven target");
  return { specifier: node.value, kind };
}

function sourceComments(analysis, path, parsed, offset, extraComments) {
  const source = analysis.read(path);
  const comments = [];
  const firstToken = parsed.ast.tokens?.[0]?.range[0] ?? source.length;
  for (const [index, comment] of [
    ...(parsed.ast.comments ?? []),
    ...extraComments,
  ].entries()) {
    const start = offset + comment.range[0];
    comments.push({
      path,
      kind: comment.type,
      raw: source.slice(start, offset + comment.range[1]),
      complete: true,
      ...analysis.location(path, start),
      beforeCode: offset === 0 && comment.range[0] < firstToken,
      preamble:
        offset === 0 && index === 0 && source.slice(0, start).trim() === "",
      byteZero: start === 0,
    });
  }
  return comments;
}

function declarationReference(node) {
  const typeOnly =
    node.importKind === "type" ||
    node.exportKind === "type" ||
    (node.specifiers?.length > 0 &&
      node.specifiers.every(
        (item) => item.importKind === "type" || item.exportKind === "type",
      ));
  return {
    specifier: node.source.value,
    kind: typeOnly
      ? "type-only"
      : node.type.startsWith("Export")
        ? "re-export"
        : "runtime",
  };
}

function isRequire(node) {
  return (
    node.type === "CallExpression" &&
    node.callee.type === "Identifier" &&
    node.callee.name === "require"
  );
}
