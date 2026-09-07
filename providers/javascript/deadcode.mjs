import { packageFor } from "./context.mjs";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, relative, sep } from "node:path";

import glob from "fast-glob";
import { frameworkEntryPoint } from "./frameworks/astro.mjs";

import { adapterFor } from "./frameworks.mjs";
import { compileAstro, originalLocation } from "./compilers.mjs";
import { sourceFacts } from "./ast.mjs";
import { projectConfiguration, resolveGlob } from "./imports.mjs";

const issueKinds = [
  "files",
  "exports",
  "types",
  "nsExports",
  "nsTypes",
  "enumMembers",
  "classMembers",
  "duplicates",
];

export async function deadcode(analysis) {
  const files = await deadCodeFiles(analysis);
  if (!files) return;
  const configuration = await configurationFor(analysis, files);
  const scratch = mkdtempSync(
    join(tmpdir(), "code-polishy-provider-deadcode-"),
  );
  const priorArgv = process.argv;
  try {
    const configPath = join(scratch, "knip.config.mjs");
    const compilerModule = new URL("./compilers.mjs", import.meta.url).href;
    writeFileSync(
      configPath,
      `import { compileAstro } from ${JSON.stringify(compilerModule)};\nexport default { ...${JSON.stringify(configuration)}, compilers: { astro: compileAstro } };\n`,
    );
    process.argv = [
      process.argv[0],
      process.argv[1],
      "--directory",
      analysis.root,
      "--config",
      relative(analysis.root, configPath),
    ];
    const { main } = await import("knip/dist/index.js");
    const result = await main({
      cwd: analysis.root,
      cacheLocation: join(scratch, "cache"),
      gitignore: false,
      includedIssueTypes: issueKinds,
      excludedIssueTypes: [],
      fixTypes: [],
      isCache: false,
      isDebug: false,
      isDependenciesShorthand: false,
      isExportsShorthand: false,
      isFilesShorthand: false,
      isFix: false,
      isFormat: false,
      isDisableConfigHints: true,
      isIncludeEntryExports: false,
      isIncludeLibs: false,
      isIsolateWorkspaces: false,
      isProduction: false,
      isRemoveFiles: false,
      isShowProgress: false,
      isStrict: false,
      isWatch: false,
      tags: [[], []],
    });
    reportIssues(analysis, result.issues);
    for (const path of analysis.request.files)
      if (
        !analysis.response.coverage.unsupported.some(
          (item) => item.path === path,
        )
      )
        analysis.response.coverage.analyzed.push(path);
  } finally {
    process.argv = priorArgv;
    rmSync(scratch, { recursive: true });
  }
}

async function configurationFor(analysis, files) {
  const workspaces = {};
  for (const path of files) {
    const owner = packageFor(analysis, path);
    if (!owner) throw new Error(`source has no package owner: ${path}`);
    const workspace = (workspaces[owner.root] ??= { entry: [], project: [] });
    const name = relative(owner.root, path).split(sep).join("/");
    workspace.project.push(glob.escapePath(name));
    if (entryPoint(analysis, owner, path))
      workspace.entry.push(glob.escapePath(name));
  }
  const { pluginNames } = await import("knip/dist/types/PluginNames.js");
  const configuration = { workspaces };
  for (const plugin of pluginNames) configuration[plugin] = false;
  return configuration;
}

function entryPoint(analysis, owner, path) {
  if (analysis.classifications.get(path)?.test) return true;
  if (
    (analysis.request.policy.entryPoints ?? []).some((pattern) =>
      glob.isDynamicPattern(pattern)
        ? glob.sync(pattern, { cwd: analysis.root }).includes(path)
        : pattern === path,
    )
  )
    return true;
  const name = relative(owner.root, path).split(sep).join("/");
  const entries = [
    owner.data.main,
    owner.data.module,
    owner.data.types,
    ...manifestPaths(owner.data.exports),
    ...manifestPaths(owner.data.bin),
  ]
    .filter((value) => typeof value === "string")
    .map((value) => value.replace(/^\.\//, ""));
  if (entries.includes(name) || /(?:^|\/)\w+\.config\.[cm]?[jt]s$/.test(name))
    return true;
  return frameworkEntryPoint(analysis, owner, name);
}

function manifestPaths(value) {
  if (typeof value === "string") return [value];
  if (Array.isArray(value)) return value.flatMap(manifestPaths);
  if (value && typeof value === "object")
    return Object.values(value).flatMap(manifestPaths);
  return [];
}

function reportIssues(analysis, issues) {
  for (const absolute of issues.files) {
    const path = relative(analysis.root, absolute).split(sep).join("/");
    if (analysis.request.files.includes(path))
      analysis.diagnostic(
        path,
        "unused-file",
        "Source file is unreachable from declared or framework entry points",
      );
  }
  for (const kind of issueKinds.filter((kind) => kind !== "files")) {
    for (const entries of Object.values(issues[kind])) {
      for (const issue of Object.values(entries)) {
        reportSymbol(analysis, kind, issue);
      }
    }
  }
}

function reportSymbol(analysis, kind, issue) {
  const path = relative(analysis.root, issue.filePath).split(sep).join("/");
  if (!analysis.request.files.includes(path)) return;
  const position = path.endsWith(".astro")
    ? originalLocation(issue.filePath, issue.line, issue.col)
    : { line: issue.line, column: issue.col };
  if (!position?.line || !position.column) {
    analysis.unsupported(
      path,
      "unused-symbol diagnostic has no reliable original-source mapping",
    );
    return;
  }
  const lines = analysis.read(path).split("\n");
  const prefix = lines.slice(0, position.line - 1).join("\n");
  analysis.diagnostic(
    path,
    `unused-${kind}`,
    `Unused ${kind}: ${issue.symbol}`,
    prefix.length + (position.line > 1 ? 1 : 0) + position.column - 1,
    issue.symbol,
  );
}

async function deadCodeFiles(analysis) {
  const files = [];
  for (const file of analysis.classifications.values()) {
    if (file.language !== "typescript") continue;
    try {
      await validateReachabilityInput(analysis, file);
      files.push(file.path);
    } catch (error) {
      for (const selected of analysis.request.files)
        analysis.unsupported(selected, `${file.path}: ${error.message}`);
      return;
    }
  }
  return files;
}

async function validateReachabilityInput(analysis, file) {
  const adapter = adapterFor(analysis, file.path);
  if (file.path.endsWith(".astro"))
    await compileAstro(
      analysis.read(file.path),
      join(analysis.root, file.path),
    );
  const parsed = adapter.parse(analysis.read(file.path), file.path);
  const facts = sourceFacts(
    analysis,
    file.path,
    parsed.parsed,
    0,
    [],
    parsed.references,
  );
  for (const script of parsed.scripts)
    facts.imports.push(
      ...sourceFacts(analysis, file.path, script.parsed, script.offset).imports,
    );
  for (const fact of facts.imports) {
    if (
      fact.glob &&
      resolveGlob(analysis, fact).some(
        (item) =>
          analysis.classifications.get(item.resolved)?.language ===
          "typescript",
      )
    )
      throw new Error(
        "unused-code analysis cannot establish executable glob reachability",
      );
  }
  projectConfiguration(analysis, file.path);
}
