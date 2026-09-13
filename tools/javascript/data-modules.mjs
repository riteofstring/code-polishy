import ts from "typescript";

export function parseLiteralDataModule(path, source) {
  const file = ts.createSourceFile(
    path,
    source,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.JS,
  );
  if (file.parseDiagnostics.length)
    throw new Error("literal data module has invalid syntax");
  const statements = [...file.statements];
  if (![1, 2].includes(statements.length)) invalidModule();
  if (statements.length === 1 && ts.isExportAssignment(statements[0])) {
    literal(defaultExportExpression(statements[0]), 0);
    return;
  }
  const declaration = literalBinding(statements[0]);
  validateBindingExport(statements[0], statements[1], declaration.name.text);
  literal(declaration.initializer, 0);
}

function literalBinding(statement) {
  if (!ts.isVariableStatement(statement)) invalidModule();
  const list = statement.declarationList;
  if (!(list.flags & ts.NodeFlags.Const) || list.declarations.length !== 1)
    invalidModule();
  const declaration = list.declarations[0];
  if (
    !ts.isIdentifier(declaration.name) ||
    declaration.type ||
    !declaration.initializer
  )
    invalidModule();
  return declaration;
}

function validateBindingExport(statement, exported, name) {
  const modifiers = statement.modifiers ?? [];
  if (!exported) {
    if (
      modifiers.length !== 1 ||
      modifiers[0].kind !== ts.SyntaxKind.ExportKeyword
    )
      invalidModule();
    return;
  }
  if (modifiers.length) invalidModule();
  const expression = defaultExportExpression(exported);
  if (!ts.isIdentifier(expression) || expression.text !== name) invalidModule();
}

function defaultExportExpression(statement) {
  if (!ts.isExportAssignment(statement) || statement.isExportEquals)
    invalidModule();
  return statement.expression;
}

function literal(node, depth) {
  if (depth > 100) throw new Error("literal data module exceeds nesting limit");
  if (primitiveLiteral(node)) return;
  if (ts.isArrayLiteralExpression(node)) {
    for (const value of node.elements) literal(value, depth + 1);
    return;
  }
  if (ts.isObjectLiteralExpression(node)) {
    objectLiteral(node, depth);
    return;
  }
  invalidModule();
}

function primitiveLiteral(node) {
  if (
    ts.isStringLiteral(node) ||
    [
      ts.SyntaxKind.TrueKeyword,
      ts.SyntaxKind.FalseKeyword,
      ts.SyntaxKind.NullKeyword,
    ].includes(node.kind)
  )
    return true;
  if (ts.isNumericLiteral(node)) return Number.isFinite(Number(node.text));
  return (
    ts.isPrefixUnaryExpression(node) &&
    node.operator === ts.SyntaxKind.MinusToken &&
    ts.isNumericLiteral(node.operand) &&
    Number.isFinite(Number(node.operand.text))
  );
}

function objectLiteral(node, depth) {
  const keys = new Set();
  for (const property of node.properties) {
    if (
      !ts.isPropertyAssignment(property) ||
      ![
        ts.SyntaxKind.Identifier,
        ts.SyntaxKind.StringLiteral,
        ts.SyntaxKind.NumericLiteral,
      ].includes(property.name.kind)
    )
      invalidModule();
    const key = property.name.text;
    if (key === "__proto__" || keys.has(key)) invalidModule();
    keys.add(key);
    literal(property.initializer, depth + 1);
  }
}

function invalidModule() {
  throw new Error(
    "declared JavaScript data requires a single literal export without executable statements, imports, calls, computed values, getters, spreads, or prototype setters",
  );
}
