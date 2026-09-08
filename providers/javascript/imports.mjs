import { packageFor } from "./context.mjs";
import { isBuiltin, createRequire } from "node:module";
import { dirname, extname, join, relative, resolve, sep } from "node:path";
import { existsSync, realpathSync, statSync } from "node:fs";

import ts from "typescript";
import glob from "fast-glob";

import { contained } from "./context.mjs";

export function projectConfiguration(analysis, path) {
  let root = dirname(path);
  for (;;) {
    for (const name of ["tsconfig.json", "jsconfig.json"]) {
      const candidate = root === "." ? name : `${root}/${name}`;
      if (analysis.inputs.has(candidate))
        return parseConfiguration(analysis, candidate, root);
    }
    if (root === ".") return null;
    root = dirname(root);
  }
}

export function resolveImport(analysis, fact, configuration) {
  if (fact.glob) return resolveGlob(analysis, fact);
  const options = configuration?.parsed.options ?? {
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    module: ts.ModuleKind.ESNext,
    allowJs: true,
    resolveJsonModule: true,
  };
  const result = { ...fact, resolved: "", package: "" };
  const name = fact.specifier;
  const provided = runtimePackage(analysis, fact);
  if (provided) return { ...result, package: provided };
  const local =
    localCandidate(analysis, fact, options, configuration) ||
    packageAsset(analysis, fact);
  if (local) {
    result.resolved = local;
    result.package = packageName(name);
    return result;
  }
  if (assetExtension(name)) {
    unresolvedImport(analysis, fact);
    return result;
  }
  return resolveTypedImport(analysis, fact, options);
}

function resolveTypedImport(analysis, fact, options) {
  const result = { ...fact, resolved: "", package: "" };
  const name = fact.specifier;
  const resolved = ts.resolveModuleName(
    name,
    join(analysis.root, fact.path),
    options,
    ts.sys,
  ).resolvedModule;
  if (resolved) {
    const absolute = realpathSync(resolved.resolvedFileName);
    if (!contained(analysis.root, absolute))
      throw new Error(`module resolution escapes the project: ${name}`);
    result.resolved = relative(analysis.root, absolute).split(sep).join("/");
    analysis.read(result.resolved);
  }
  result.package = packageName(name);
  if (!resolved) unresolvedImport(analysis, fact);
  return result;
}

function localCandidate(analysis, fact, options, configuration) {
  const candidates = aliasCandidates(
    analysis,
    fact.specifier,
    options,
    configuration,
  );
  if (fact.specifier.startsWith("."))
    candidates.unshift(
      resolve(analysis.root, dirname(fact.path), fact.specifier),
    );
  for (const candidate of candidates) {
    const path = containedAsset(analysis, candidate);
    if (path) return path;
  }
  return "";
}

function aliasCandidates(analysis, name, options, configuration) {
  const candidates = [];
  const base =
    options.baseUrl ?? join(analysis.root, configuration?.root ?? ".");
  for (const [pattern, targets] of Object.entries(options.paths ?? {})) {
    const match = aliasMatch(name, pattern);
    if (match === null) continue;
    for (const target of targets)
      candidates.push(resolve(base, target.replace("*", match)));
  }
  return candidates;
}

function aliasMatch(name, pattern) {
  const [prefix, suffix = ""] = pattern.split("*");
  if (
    !name.startsWith(prefix) ||
    !name.endsWith(suffix) ||
    (!pattern.includes("*") && pattern !== name)
  )
    return null;
  return name.slice(prefix.length, name.length - suffix.length);
}

function containedAsset(analysis, candidate) {
  if (
    !assetExtension(candidate) ||
    !contained(analysis.root, candidate) ||
    !existsSync(candidate)
  )
    return "";
  const canonical = realpathSync(candidate);
  if (!contained(analysis.root, canonical))
    throw new Error("local import escapes the project");
  if (!statSync(canonical).isFile())
    throw new Error("asset import does not resolve to a regular file");
  const path = relative(analysis.root, canonical).split(sep).join("/");
  analysis.read(path);
  return path;
}

function sourceImportOffset(analysis, fact) {
  const lines = analysis.read(fact.path).split("\n");
  const prefix = lines.slice(0, fact.line - 1).join("\n");
  return (
    prefix.length +
    (fact.line > 1 ? 1 : 0) +
    Buffer.from(lines[fact.line - 1])
      .subarray(0, fact.column - 1)
      .toString("utf8").length
  );
}

export function resolveGlob(analysis, fact) {
  if (!fact.specifier.startsWith("."))
    throw new Error("module glob must be relative to its source");
  const cwd = join(analysis.root, dirname(fact.path));
  const base = fact.specifier.split(/[*?{\[]/)[0];
  if (!contained(analysis.root, resolve(cwd, base)))
    throw new Error("module glob escapes project");
  const { glob: _, ...reference } = fact;
  return glob
    .sync(fact.specifier, {
      cwd,
      absolute: true,
      onlyFiles: true,
      dot: true,
      followSymbolicLinks: false,
    })
    .toSorted()
    .map((absolute) => {
      const canonical = realpathSync(absolute);
      if (!contained(analysis.root, canonical))
        throw new Error("module glob resolves outside project");
      const path = relative(analysis.root, canonical).split(sep).join("/");
      analysis.read(path);
      return { ...reference, resolved: path, package: "" };
    });
}

function parseConfiguration(analysis, candidate, root) {
  const errors = [];
  const host = {
    ...ts.sys,
    onUnRecoverableConfigFileDiagnostic: (diagnostic) =>
      errors.push(diagnostic),
  };
  const parsed = ts.getParsedCommandLineOfConfigFile(
    join(analysis.root, candidate),
    { allowJs: true, checkJs: true },
    host,
    undefined,
    undefined,
    [
      {
        extension: ".astro",
        isMixedContent: true,
        scriptKind: ts.ScriptKind.Deferred,
      },
    ],
  );
  if (!parsed || errors.length || parsed.errors.length)
    throw new Error(`project configuration cannot be read: ${candidate}`);
  if (parsed.options.plugins?.length)
    throw new Error(
      "target TypeScript compiler plugins are not authorized analyzer tools",
    );
  return { path: candidate, root, parsed };
}

function runtimePackage(analysis, fact) {
  if (isBuiltin(fact.specifier)) return fact.specifier;
  const owner = packageFor(analysis, fact.path);
  if (
    fact.specifier.startsWith("astro:") &&
    (owner?.data.dependencies?.astro || owner?.data.devDependencies?.astro)
  )
    return "astro";
  return "";
}

function assetExtension(name) {
  return [
    ".css",
    ".json",
    ".astro",
    ".png",
    ".jpg",
    ".jpeg",
    ".gif",
    ".svg",
    ".webp",
    ".avif",
    ".ico",
    ".mp3",
    ".wav",
    ".ogg",
    ".flac",
    ".mp4",
    ".webm",
    ".woff",
    ".woff2",
    ".ttf",
    ".otf",
    ".eot",
    ".pdf",
  ].includes(extname(name).toLowerCase());
}

function packageName(name) {
  if (/^[./#]/.test(name)) return "";
  return name.startsWith("@")
    ? name.split("/").slice(0, 2).join("/")
    : name.split("/")[0];
}

function packageAsset(analysis, fact) {
  if (!packageName(fact.specifier) || !assetExtension(fact.specifier))
    return "";
  let path;
  try {
    path = createRequire(join(analysis.root, fact.path)).resolve(
      fact.specifier,
    );
  } catch {
    return "";
  }
  const absolute = realpathSync(path);
  if (!contained(analysis.root, absolute))
    throw new Error("package asset escapes project");
  if (!statSync(absolute).isFile())
    throw new Error("package asset is not a regular file");
  const resolved = relative(analysis.root, absolute).split(sep).join("/");
  analysis.read(resolved);
  return resolved;
}

function unresolvedImport(analysis, fact) {
  analysis.diagnostic(
    fact.path,
    "unresolved-import",
    `Cannot resolve import ${JSON.stringify(fact.specifier)}`,
    sourceImportOffset(analysis, fact),
    fact.specifier,
  );
}
