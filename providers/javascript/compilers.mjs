import { convertToTSX } from "@astrojs/compiler";
import { TraceMap, originalPositionFor } from "@jridgewell/trace-mapping";

const mappings = new Map();

export async function compileAstro(source, path) {
  const result = await convertToTSX(source, { filename: path });
  if (result.diagnostics.some((diagnostic) => diagnostic.severity === 1))
    throw new Error(`framework compilation failed for ${path}`);
  mappings.set(path, new TraceMap(result.map));
  return result.code;
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
