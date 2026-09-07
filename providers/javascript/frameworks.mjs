import { extname } from "node:path";

import { parseJavaScript } from "./ast.mjs";
import { astro, astroInstallation } from "./frameworks/astro.mjs";

const javascript = {
  parse: (source, path) => ({
    parsed: parseJavaScript(source, path),
    scripts: [],
  }),
  formatter: {},
};
const nativeExtensions = new Set([
  ".js",
  ".jsx",
  ".ts",
  ".tsx",
  ".cjs",
  ".mjs",
  ".cts",
  ".mts",
]);

export function adapterFor(analysis, path) {
  if (astro.matches(path)) {
    astroInstallation(analysis, path);
    return astro;
  }
  if (nativeExtensions.has(extname(path))) return javascript;
  throw new Error(
    "the installed JS/TS provider has no adapter for this source syntax",
  );
}
