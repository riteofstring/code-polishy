import { convertToTSX } from "./node_modules/@astrojs/compiler/dist/node/index.js";
import {
  TraceMap,
  originalPositionFor,
} from "./node_modules/@jridgewell/trace-mapping/dist/trace-mapping.mjs";
import typescript from "./node_modules/typescript/lib/typescript.js";

const compiledMaps = new Map();

export async function astroSource(text, path) {
  const result = await convertToTSX(text, { filename: path });
  const errors = result.diagnostics.filter((entry) => entry.severity === 1);
  if (errors.length) {
    throw new Error(errors.map((entry) => entry.text).join("; "));
  }
  const parsed = typescript.createSourceFile(
    path,
    result.code,
    typescript.ScriptTarget.Latest,
    true,
    typescript.ScriptKind.TSX,
  );
  if (parsed.parseDiagnostics.length) {
    throw new Error(
      typescript.flattenDiagnosticMessageText(
        parsed.parseDiagnostics[0].messageText,
        " ",
      ),
    );
  }
  return { code: result.code, map: new TraceMap(result.map) };
}

export function astroPosition(map, line, column) {
  const position = originalPositionFor(map, { line, column });
  if (position.line === null) {
    throw new Error(
      "The Astro compiler could not map an import to its source.",
    );
  }
  return position;
}

export async function compileAstro(text, path) {
  const source = await astroSource(text, path);
  const compiled = typescript.transpileModule(
    hoistClientImports(source.code, path),
    {
      fileName: `${path}.tsx`,
      compilerOptions: {
        target: typescript.ScriptTarget.ESNext,
        module: typescript.ModuleKind.ESNext,
        jsx: typescript.JsxEmit.React,
        jsxFactory: "__astro_element",
        jsxFragmentFactory: "__astro_fragment",
        verbatimModuleSyntax: true,
        sourceMap: true,
      },
    },
  );
  compiledMaps.set(path, {
    virtual: source.map,
    compiled: new TraceMap(JSON.parse(compiled.sourceMapText)),
  });
  return compiled.outputText;
}

function hoistClientImports(code, path) {
  const source = typescript.createSourceFile(
    path,
    code,
    typescript.ScriptTarget.Latest,
    true,
    typescript.ScriptKind.TSX,
  );
  const imports = [];
  const visit = (node, clientScript) => {
    const inScript =
      clientScript ||
      (typescript.isJsxElement(node) &&
        node.openingElement.tagName.getText(source) === "script");
    if (inScript && typescript.isImportDeclaration(node)) imports.push(node);
    typescript.forEachChild(node, (child) => visit(child, inScript));
  };
  visit(source, false);
  let result = code;
  for (const node of [...imports].reverse()) {
    const start = node.getStart(source);
    result =
      result.slice(0, start) +
      result.slice(start, node.end).replace(/[^\r\n]/g, " ") +
      result.slice(node.end);
  }
  return result + "\n" + imports.map((node) => node.getText(source)).join("\n");
}

export function astroIssuePosition(path, line, column) {
  const maps = compiledMaps.get(path);
  if (!maps) return { line, column };
  const virtual = originalPositionFor(maps.compiled, {
    line,
    column: column - 1,
  });
  if (virtual.line === null) return { line, column };
  const original = originalPositionFor(maps.virtual, virtual);
  return original.line === null
    ? { line, column }
    : { line: original.line, column: original.column + 1 };
}
