import { lstatSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { extname, join, relative } from "node:path";

import typescript from "./node_modules/typescript/lib/typescript.js";

import {
  MAXIMUM_OPERATION_PATHS,
  containedProgramRead,
  containedRead,
  fail,
  insideRoot,
  requireContainedPath,
  requireExactObject,
  truncate,
  unsupported,
} from "./protocol.mjs";

const MAXIMUM_DEADCODE_RESULTS = 5000;

const DEADCODE_EXTENSIONS = [
  ".cjs",
  ".cts",
  ".js",
  ".jsx",
  ".mjs",
  ".mts",
  ".ts",
  ".tsx",
];

const DEADCODE_ISSUE_TYPES = [
  "files",
  "exports",
  "types",
  "nsExports",
  "nsTypes",
  "enumMembers",
  "classMembers",
  "duplicates",
];

const DEADCODE_PATTERN_CHARACTERS = /[*?[\]{}()!]/;

function insideDirectory(directory, path) {
  return (
    directory === "." || path === directory || path.startsWith(`${directory}/`)
  );
}

function requireWorkspace(directory, workspace) {
  if (!("inherited" in workspace)) {
    workspace.inherited = [];
  }
  requireExactObject(workspace, "a dead-code workspace", [
    "root",
    "entry",
    "project",
    "inherited",
  ]);
  const named = JSON.stringify(workspace.root);
  requireContainedPath(workspace.root);
  if (!insideDirectory(directory, workspace.root)) {
    fail(`the dead-code workspace ${named} is outside the analyzed directory`);
  }
  requireWorkspaceCollections(workspace, named);
  const inherited = requireInheritedPaths(directory, workspace, named);
  const owned = requireOwnedPaths(directory, workspace, inherited, named);
  requireInheritedOwnership(inherited, owned, named);
  requireWorkspaceEntries(workspace.entry, owned, named);
}

function requireWorkspaceCollections(workspace, named) {
  if (!Array.isArray(workspace.project) || workspace.project.length === 0) {
    fail(`the dead-code workspace ${named} selects no files`);
  }
  if (!Array.isArray(workspace.entry)) {
    fail(
      `the dead-code workspace ${named} declares entry points that are not an array`,
    );
  }
  if (!Array.isArray(workspace.inherited)) {
    fail(`the dead-code workspace ${named} has no inherited-path array`);
  }
}

function requireInheritedPaths(directory, workspace, named) {
  const inherited = new Set();
  for (const path of workspace.inherited) {
    requireContainedPath(path);
    if (!insideDirectory(directory, path) || inherited.has(path)) {
      fail(
        `the dead-code workspace ${named} declares an invalid inherited path`,
      );
    }
    inherited.add(path);
  }
  return inherited;
}

function requireOwnedPaths(directory, workspace, inherited, named) {
  const owned = new Set();
  for (const path of workspace.project) {
    requireContainedPath(path);
    if (
      !insideDirectory(directory, path) ||
      (!insideDirectory(workspace.root, path) && !inherited.has(path))
    ) {
      fail(
        `the dead-code workspace ${named} selects ${JSON.stringify(path)} outside the analyzed directory`,
      );
    }
    owned.add(path);
  }
  return owned;
}

function requireInheritedOwnership(inherited, owned, named) {
  for (const path of inherited) {
    if (!owned.has(path)) {
      fail(
        `the dead-code workspace ${named} inherits a path it does not select`,
      );
    }
  }
}

function requireWorkspaceEntries(entries, owned, named) {
  for (const path of entries) {
    requireContainedPath(path);
    if (!owned.has(path)) {
      fail(
        `the dead-code workspace ${named} declares the entry point ${JSON.stringify(path)}, which it does not select`,
      );
    }
  }
}

export function requireWorkspaces(directory, workspaces) {
  if (!Array.isArray(workspaces) || workspaces.length === 0) {
    fail("the request declares no dead-code workspaces");
  }
  const roots = new Set();
  let files = 0;
  for (const workspace of workspaces) {
    requireWorkspace(directory, workspace);
    if (roots.has(workspace.root)) {
      fail(
        `the request declares the dead-code workspace ${JSON.stringify(workspace.root)} twice`,
      );
    }
    roots.add(workspace.root);
    files += workspace.project.length;
  }
  if (files > MAXIMUM_OPERATION_PATHS) {
    fail(
      `the request selects ${files} files, more than the ${MAXIMUM_OPERATION_PATHS} limit`,
    );
  }
}

function containedName(directory, path) {
  if (directory === path) {
    return ".";
  }
  return directory === "." ? path : path.slice(directory.length + 1);
}

function workspaceFiles(request, workspace, unsupportedPaths) {
  const project = [];
  for (const path of workspace.project) {
    const absolute = join(request.root, path);
    if (!DEADCODE_EXTENSIONS.includes(extname(path).toLowerCase())) {
      unsupportedPaths.push(
        unsupported(
          path,
          "the policy-owned dead-code analyzer does not analyze this file",
        ),
      );
    } else if (DEADCODE_PATTERN_CHARACTERS.test(path)) {
      unsupportedPaths.push(
        unsupported(path, "the file name contains a pattern character"),
      );
    } else if (isRegularFile(absolute, path, unsupportedPaths)) {
      project.push(path);
    }
  }
  return project;
}

function isRegularFile(absolute, path, unsupportedPaths) {
  let info;
  try {
    info = lstatSync(absolute);
  } catch (error) {
    unsupportedPaths.push(
      unsupported(
        path,
        `the file is unreadable: ${error.message.replaceAll(absolute, path)}`,
      ),
    );
    return false;
  }
  if (!info.isFile()) {
    unsupportedPaths.push(unsupported(path, "the path is not a regular file"));
    return false;
  }
  if (!containedRead(absolute)) {
    unsupportedPaths.push(
      unsupported(path, "the path resolves outside the target tree"),
    );
    return false;
  }
  return true;
}

async function configurationFor(request, covered, unsupportedPaths) {
  const { pluginNames } =
    await import("./node_modules/knip/dist/types/PluginNames.js");
  const workspaces = {};
  for (const workspace of request.workspaces) {
    const project = workspaceFiles(request, workspace, unsupportedPaths);
    covered.push(...project);
    const analyzed = new Set(project);
    workspaces[containedName(request.directory, workspace.root)] = {
      entry: workspace.entry
        .filter((path) => analyzed.has(path))
        .map((path) =>
          relative(
            join(request.root, workspace.root),
            join(request.root, path),
          ).replaceAll("\\", "/"),
        ),
      project: project.map((path) =>
        relative(
          join(request.root, workspace.root),
          join(request.root, path),
        ).replaceAll("\\", "/"),
      ),
    };
  }
  const configuration = { workspaces };
  for (const name of pluginNames) {
    configuration[name] = false;
  }
  return configuration;
}

function unusedExport(root, kind, issue) {
  const path = insideRoot(root, issue.filePath);
  const symbol =
    issue.parentSymbol === undefined
      ? issue.symbol
      : `${issue.parentSymbol}.${issue.symbol}`;
  return path === null
    ? null
    : {
        path,
        line: issue.line ?? 0,
        column: issue.col ?? 0,
        symbol: truncate(String(symbol)),
        kind,
      };
}

function reportedIssues(root, issues) {
  const unusedFiles = [];
  for (const filePath of issues.files) {
    const path = insideRoot(root, filePath);
    if (path !== null) {
      unusedFiles.push(path);
    }
  }
  const unusedExports = [];
  for (const kind of DEADCODE_ISSUE_TYPES) {
    if (kind === "files") {
      continue;
    }
    for (const found of Object.values(issues[kind])) {
      for (const issue of Object.values(found)) {
        const reported = unusedExport(root, kind, issue);
        if (reported !== null) {
          unusedExports.push(reported);
        }
      }
    }
  }
  unusedFiles.sort();
  unusedExports.sort(
    (left, right) =>
      left.path.localeCompare(right.path) ||
      left.line - right.line ||
      left.column - right.column ||
      left.symbol.localeCompare(right.symbol),
  );
  return { unusedFiles, unusedExports };
}

function containAnalyzerReads() {
  const readFile = typescript.sys.readFile;
  typescript.sys.readFile = (path, encoding) =>
    containedProgramRead(path) ? readFile(path, encoding) : undefined;
}

export async function deadcode(request) {
  containAnalyzerReads();
  const unsupportedPaths = [];
  const covered = [];
  const configuration = await configurationFor(
    request,
    covered,
    unsupportedPaths,
  );
  if (covered.length === 0) {
    return {
      unusedFiles: [],
      unusedExports: [],
      covered,
      unsupported: unsupportedPaths,
    };
  }

  const scratch = mkdtempSync(join(tmpdir(), "code-polishy-deadcode-"));
  try {
    return await analyze(
      request,
      configuration,
      covered,
      unsupportedPaths,
      scratch,
    );
  } finally {
    rmSync(scratch, { force: true, recursive: true });
  }
}

async function analyze(
  request,
  configuration,
  covered,
  unsupportedPaths,
  scratch,
) {
  const configurationPath = join(scratch, "policy-knip.json");
  writeFileSync(configurationPath, JSON.stringify(configuration));
  const directory = join(request.root, request.directory);

  process.argv = [
    process.argv[0],
    process.argv[1],
    "--directory",
    directory,
    "--config",
    relative(directory, configurationPath),
  ];
  const { main } = await import("./node_modules/knip/dist/index.js");
  let analysis;
  try {
    analysis = await main(analyzerOptions(directory, scratch));
  } catch (error) {
    fail(`the dead-code analysis failed: ${truncate(error.message)}`);
  }
  const { unusedFiles, unusedExports } = reportedIssues(
    request.root,
    analysis.issues,
  );
  if (unusedFiles.length + unusedExports.length > MAXIMUM_DEADCODE_RESULTS) {
    fail(
      `the deadcode operation produced more than the ${MAXIMUM_DEADCODE_RESULTS} result limit`,
    );
  }
  return { unusedFiles, unusedExports, covered, unsupported: unsupportedPaths };
}

function analyzerOptions(directory, scratch) {
  return {
    cacheLocation: join(scratch, "cache"),
    cwd: directory,
    excludedIssueTypes: [],
    fixTypes: [],

    gitignore: false,
    includedIssueTypes: DEADCODE_ISSUE_TYPES,
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
    tsConfigFile: undefined,
    workspace: undefined,
  };
}
