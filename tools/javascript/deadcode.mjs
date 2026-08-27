// The dead-code operation of the sealed, policy-owned JavaScript tool bundle.
//
// It reports the governed source no entry point reaches and the exported
// symbols nothing uses, across one tree of packages. Go decides everything the
// analysis depends on: which packages the tree contains, which governed files
// each contributes, and which of those files are entry points. Nothing is
// discovered from the target, and nothing target-owned is executed.

import { lstatSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { extname, join, relative } from "node:path";

// Resolved relative to this file rather than as a bare specifier, so the
// compiler can only ever be the one installed beside the runner. The analyzer
// resolves the same installed copy, so this is the object it reads through.
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

// One dead-code exchange reports bounded results, for the same reason the lint
// and typecheck exchanges do.
const MAXIMUM_DEADCODE_RESULTS = 5000;

// The file types the sealed dead-code analyzer reads. They are the ones it
// parses on its own; a single-file component needs a compiler, and a compiler
// is target code, so a file of any other type is reported as uncovered.
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

// The findings this operation reports. Every one of them is about a file or an
// exported symbol nothing uses. The analyzer can also report dependency-graph
// facts; those belong to the operations that own the package graph, so this one
// neither asks for them nor answers with them.
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

// A selected path reaches the analyzer as a pattern. One carrying a pattern
// character would name something other than itself, so it is reported as
// uncovered rather than matched against whatever it happens to expand to.
const DEADCODE_PATTERN_CHARACTERS = /[*?[\]{}()!]/;

// Whether one contained path names something inside a contained directory.
function insideDirectory(directory, path) {
  return (
    directory === "." || path === directory || path.startsWith(`${directory}/`)
  );
}

// One package of the analyzed tree. Nothing about it may be left to the runner,
// so a file cannot arrive without a package, an entry point cannot arrive
// without being analyzed, and a package cannot name a tree the analysis does
// not contain.
function requireWorkspace(directory, workspace) {
  requireExactObject(workspace, "a dead-code workspace", [
    "root",
    "entry",
    "project",
  ]);
  const named = JSON.stringify(workspace.root);
  requireContainedPath(workspace.root);
  if (!insideDirectory(directory, workspace.root)) {
    fail(`the dead-code workspace ${named} is outside the analyzed directory`);
  }
  if (!Array.isArray(workspace.project) || workspace.project.length === 0) {
    fail(`the dead-code workspace ${named} selects no files`);
  }
  if (!Array.isArray(workspace.entry)) {
    fail(
      `the dead-code workspace ${named} declares entry points that are not an array`,
    );
  }
  const owned = new Set();
  for (const path of workspace.project) {
    requireContainedPath(path);
    if (!insideDirectory(workspace.root, path)) {
      fail(
        `the dead-code workspace ${named} selects ${JSON.stringify(path)}, which it does not contain`,
      );
    }
    owned.add(path);
  }
  for (const path of workspace.entry) {
    requireContainedPath(path);
    if (!owned.has(path)) {
      fail(
        `the dead-code workspace ${named} declares the entry point ${JSON.stringify(path)}, which it does not select`,
      );
    }
  }
}

// The packages one dead-code analysis covers.
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

// Where one path sits inside a directory that contains it. The protocol names
// every path the way the repository does; the analyzer names a package relative
// to the analyzed tree and a file relative to the package that owns it.
function containedName(directory, path) {
  if (directory === path) {
    return ".";
  }
  return directory === "." ? path : path.slice(directory.length + 1);
}

// The selected files one package contributes, and the reason for each one the
// analyzer cannot address. A refused file is reported rather than dropped, so
// Go sees exactly which governed source went unanalyzed.
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
    // The runtime names the file absolutely; the reported reason names it the
    // way the request did, so no host path crosses the protocol.
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

// The one configuration the analysis runs under. Code Polishy owns every part of
// it: the packages, the exact governed files each one contributes, and which of
// those files are entry points. Nothing is discovered, so a target knip.json,
// .knip.json, or package.json#knip is never read.
//
// Every analyzer plug-in is disabled by name. A plug-in exists to learn a
// framework's entry points from that framework's own configuration file, which
// it does by loading that file -- target code this operation must never
// execute. Code Polishy supplies the entry points instead, so the analyzer never
// needs one.
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
        .map((path) => containedName(workspace.root, path)),
      project: project.map((path) => containedName(workspace.root, path)),
    };
  }
  const configuration = { workspaces };
  for (const name of pluginNames) {
    configuration[name] = false;
  }
  return configuration;
}

// One unused export, member, or duplicate as a bounded fact: a target-relative
// path, a position, the exact analyzer issue type, and the symbol it named.
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

// Contain every source file the analyzer reads. It resolves imports itself and
// reads each resolved file through the compiler's own system object, so a
// selected file importing above the repository or through a link that lands
// outside it would otherwise pull another tree into the analysis and let what
// is written there decide whether target source is reachable. Reading is
// answered only for what is really inside the target tree or inside the
// bundle's own library declarations; anything else reads as absent, so a module
// outside the repository contributes nothing and the source that only it
// reaches is reported as unreachable rather than quietly kept alive.
function containAnalyzerReads() {
  const readFile = typescript.sys.readFile;
  typescript.sys.readFile = (path, encoding) =>
    containedProgramRead(path) ? readFile(path, encoding) : undefined;
}

// Report the governed source no entry point reaches and the exported symbols
// nothing uses. Go owns which of those is a failure; this reports only what the
// analyzer found under the configuration Go decided.
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
  // Both the generated configuration and anything the analyzer would cache live
  // in a scratch directory this process creates and deletes, so an analysis
  // leaves nothing behind in the repository it read or the directory it was
  // launched from.
  const scratch = mkdtempSync(join(tmpdir(), "code-polishy-deadcode-"));
  try {
    // Awaited inside the block so the scratch directory outlives every read of
    // the configuration written into it.
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
  // The analyzer reads its working directory and its configuration path from
  // the command line of the process that loaded it, so both are set before it
  // is imported, and it is imported only for this operation.
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

// Every setting the analyzer runs under, stated rather than defaulted, so no
// part of the run is decided by the analyzer's own conventions.
function analyzerOptions(directory, scratch) {
  return {
    cacheLocation: join(scratch, "cache"),
    cwd: directory,
    excludedIssueTypes: [],
    fixTypes: [],
    // Code Polishy already decided which files are governed, so the analyzer
    // must not apply a second selection of its own.
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
