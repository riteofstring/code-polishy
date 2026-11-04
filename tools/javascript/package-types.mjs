import typescript from "typescript";

import { sourceSpecifiers, specifiedPackage } from "./imports.mjs";
import { containedRead, insideRoot } from "./protocol.mjs";

const BASE_OPTIONS = {
  noLib: true,
  noEmit: true,
  types: [],
  skipLibCheck: true,
  ignoreDeprecations: "6.0",
};
const CONSUMER_MODES = [
  [
    {
      module: typescript.ModuleKind.CommonJS,
      moduleResolution: typescript.ModuleResolutionKind.Node10,
    },
    ".ts",
  ],
  [
    {
      module: typescript.ModuleKind.NodeNext,
      moduleResolution: typescript.ModuleResolutionKind.NodeNext,
    },
    ".mts",
  ],
  [
    {
      module: typescript.ModuleKind.NodeNext,
      moduleResolution: typescript.ModuleResolutionKind.NodeNext,
    },
    ".cts",
  ],
  [
    {
      module: typescript.ModuleKind.ESNext,
      moduleResolution: typescript.ModuleResolutionKind.Bundler,
    },
    ".ts",
  ],
];

function selfAlias(packageRoot, name) {
  if (typeof name !== "string" || specifiedPackage(name) !== name) return null;
  return `${packageRoot}/node_modules/${name}`;
}

function publishedPath(packageRoot, alias, path) {
  const mapped =
    alias !== null && (path === alias || path.startsWith(`${alias}/`))
      ? `${packageRoot}${path.slice(alias.length)}`
      : path;
  if (mapped === packageRoot) return mapped;
  const relative = insideRoot(packageRoot, mapped);
  if (relative === null || relative.split("/").includes("node_modules"))
    return null;
  return containedRead(mapped) ? mapped : null;
}

function aliasDirectory(alias, path) {
  if (alias === null) return false;
  return alias === path || alias.startsWith(`${path}/`);
}

function packageHost(options, packageRoot, alias, entries) {
  const base = typescript.createCompilerHost(options, true);
  const published = (path) =>
    entries.has(path) ? path : publishedPath(packageRoot, alias, path);
  const readFile = (path) => {
    if (entries.has(path)) return entries.get(path);
    const target = published(path);
    return target === null ? undefined : base.readFile(target);
  };
  return {
    ...base,
    getCurrentDirectory: () => packageRoot,
    fileExists: (path) => readFile(path) !== undefined,
    readFile,
    directoryExists: (path) =>
      aliasDirectory(alias, path) ||
      (published(path) !== null && base.directoryExists(published(path))),
    getDirectories: (path) =>
      published(path) === null ? [] : base.getDirectories(published(path)),
    realpath: (path) => {
      const target = published(path);
      return target === null || entries.has(path)
        ? path
        : base.realpath(target);
    },
    getSourceFile: (fileName, languageVersion) => {
      const text = readFile(fileName);
      return text === undefined
        ? undefined
        : typescript.createSourceFile(fileName, text, languageVersion, true);
    },
    writeFile: () => {},
  };
}

function typePrograms(packageRoot, name, roots, entries) {
  const alias = selfAlias(packageRoot, name);
  const imports = [...entries, "./"]
    .map((entry) => `import type {} from ${JSON.stringify(entry)};\n`)
    .join("");
  return CONSUMER_MODES.map(([mode, extension]) => {
    const options = { ...BASE_OPTIONS, ...mode };
    const entry = `${packageRoot}/__published_entries__${extension}`;
    const host = packageHost(
      options,
      packageRoot,
      alias,
      new Map([[entry, imports]]),
    );
    return {
      entry,
      program: typescript.createProgram([entry, ...roots], options, host),
    };
  });
}

export function typeClosure(packageRoot, name, roots, entries) {
  const files = new Map();
  for (const { entry, program } of typePrograms(
    packageRoot,
    name,
    roots,
    entries,
  )) {
    for (const file of program.getSourceFiles()) {
      if (file.fileName === entry) continue;
      const record = files.get(file.fileName) ?? {
        source: file,
        resolutions: new Map(),
      };
      files.set(file.fileName, record);
      for (const { node } of sourceSpecifiers(file).specifiers) {
        const resolved = program.getResolvedModule(
          file,
          node.text,
          program.getModeForUsageLocation(file, node),
        )?.resolvedModule;
        if (!resolved) continue;
        const position = node.getStart(file);
        const targets = record.resolutions.get(position) ?? new Set();
        targets.add(resolved.resolvedFileName);
        record.resolutions.set(position, targets);
      }
    }
  }
  return files;
}
