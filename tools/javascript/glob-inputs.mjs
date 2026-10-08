import { createRequire } from "node:module";

function validateGlobPattern(pattern) {
  if (typeof pattern !== "string") return;
  const stack = [];
  for (let index = 0; index < pattern.length; index++) {
    const character = pattern[index];
    if (["\\", "[", "'", '"', "`"].includes(character)) {
      index = literalEnd(pattern, index);
    } else if (character === "{" || character === "(") {
      stack.push(character === "{" ? "}" : ")");
      if (stack.length > 64)
        throw new Error("glob nesting exceeds the supported 64 levels");
    } else if (character === stack.at(-1)) {
      stack.pop();
    }
  }
}

function literalEnd(pattern, index) {
  const opening = pattern[index];
  if (opening === "\\") return index + 1;
  const closing = opening === "[" ? "]" : opening;
  let depth = 1;
  while (++index < pattern.length) {
    if (pattern[index] === "\\") index++;
    else if (pattern[index] === closing) {
      if (--depth === 0) break;
    } else if (opening === "[" && pattern[index] === opening) depth++;
  }
  return index;
}

export function bindKnipGlobs(knipModule) {
  const glob = createRequire(knipModule)("fast-glob");
  const originals = new Map();
  for (const method of ["glob", "sync", "globStream"]) {
    const original = glob[method];
    originals.set(method, original);
    glob[method] = function (patterns, options) {
      for (const pattern of [].concat(patterns, options?.ignore ?? []))
        validateGlobPattern(pattern);
      return original(patterns, options);
    };
  }
  return () => {
    for (const [method, original] of originals) glob[method] = original;
  };
}
