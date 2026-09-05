import typescript from "./node_modules/typescript/lib/typescript.js";

export function parseDataModule(path, text) {
  const source = typescript.createSourceFile(
    path,
    text,
    typescript.ScriptTarget.Latest,
    true,
    typescript.ScriptKind.JS,
  );
  if (source.parseDiagnostics.length) {
    throw new Error("invalid JavaScript data module syntax");
  }
  const statements = source.statements;
  const last = statements.at(-1);
  if (!last || !typescript.isExportAssignment(last) || last.isExportEquals) {
    throw new Error("a data module must have one default JSON export");
  }
  let value = last.expression;
  if (statements.length === 2) {
    value = declaredValue(statements[0], value);
  } else if (statements.length !== 1) {
    throw new Error("a data module cannot contain other statements");
  }
  JSON.parse(value.getText(source));
}

function declaredValue(statement, exported) {
  if (!singleConst(statement)) {
    throw new Error("a data module may declare only one unmodified const");
  }
  const declaration = statement.declarationList.declarations[0];
  if (
    !typescript.isIdentifier(declaration.name) ||
    !typescript.isIdentifier(exported) ||
    declaration.name.text !== exported.text ||
    declaration.type ||
    !declaration.initializer
  ) {
    throw new Error("the default export must name the JSON const");
  }
  return declaration.initializer;
}

function singleConst(statement) {
  return (
    typescript.isVariableStatement(statement) &&
    !statement.modifiers?.length &&
    (statement.declarationList.flags & typescript.NodeFlags.BlockScoped) ===
      typescript.NodeFlags.Const &&
    statement.declarationList.declarations.length === 1
  );
}
