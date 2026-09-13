import { convertToTSX } from "@astrojs/compiler";
import { TraceMap, originalPositionFor } from "@jridgewell/trace-mapping";

import { astro } from "./frameworks/astro.mjs";

const mappings = new Map();

export async function compileAstro(source, path) {
  const result = await convertToTSX(source, { filename: path });
  if (result.diagnostics.some((diagnostic) => diagnostic.severity === 1))
    throw new Error(`framework compilation failed for ${path}`);
  const references = astro.parse(source, path).references;
  const imports = references
    .filter((reference) => !reference.external)
    .map((reference) => {
      if (reference.problem) throw new Error(reference.problem);
      return `import ${JSON.stringify(reference.specifier)};`;
    });
  mappings.set(path, new TraceMap(result.map));
  return `${result.code}\n${imports.join("\n")}`;
}

export function originalLocation(path, line, column) {
  const mapping = mappings.get(path);
  if (!mapping) return null;
  const original = originalPositionFor(mapping, {
    line,
    column: Math.max(0, column - 1),
  });
  return original.line === null || original.column === null
    ? null
    : { line: original.line, column: original.column + 1 };
}
