import typescript from "typescript";

import { sourceSpecifiers } from "./imports.mjs";
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

function publishedPath(packageRoot, path) {
  if (path === packageRoot) return path;
  const relative = insideRoot(packageRoot, path);
  if (relative === null || relative.split("/").includes("node_modules"))
    return null;
  return containedRead(path) ? path : null;
}

function packageHost(options, packageRoot, entries) {
  const base = typescript.createCompilerHost(options, true);
  const published = (path) =>
    entries.has(path) ? path : publishedPath(packageRoot, path);
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
      packageRoot.startsWith(`${path}/`) ||
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

function typePrograms(packageRoot, entries) {
  const imports = [...entries, "./"]
    .map((entry) => `import type {} from ${JSON.stringify(entry)};\n`)
    .join("");
  return CONSUMER_MODES.map(([mode, extension]) => {
    const options = { ...BASE_OPTIONS, ...mode };
    const entry = `${packageRoot}/__published_entries__${extension}`;
    const host = packageHost(options, packageRoot, new Map([[entry, imports]]));
    return { entry, program: typescript.createProgram([entry], options, host) };
  });
}

function resolvedFile(program, file, node) {
  return (
    program.getResolvedModule(
      file,
      node.text,
      program.getModeForUsageLocation(file, node),
    )?.resolvedModule?.resolvedFileName ?? null
  );
}

function recordResolutions(program, file, resolutions) {
  for (const { node } of sourceSpecifiers(file).specifiers) {
    const position = node.getStart(file);
    const outcomes = resolutions.get(position) ?? [];
    outcomes.push(resolvedFile(program, file, node));
    resolutions.set(position, outcomes);
  }
}

function recordEntries(program, file, resolvedEntries) {
  for (const { node } of sourceSpecifiers(file).specifiers)
    if (resolvedFile(program, file, node) !== null)
      resolvedEntries.add(node.text);
}

export function typeClosure(packageRoot, entries) {
  const files = new Map();
  const resolvedEntries = new Set();
  for (const { entry, program } of typePrograms(packageRoot, entries)) {
    for (const file of program.getSourceFiles()) {
      if (file.fileName === entry) {
        recordEntries(program, file, resolvedEntries);
        continue;
      }
      const record = files.get(file.fileName) ?? {
        source: file,
        resolutions: new Map(),
      };
      files.set(file.fileName, record);
      recordResolutions(program, file, record.resolutions);
    }
  }
  return { files, resolvedEntries };
}
