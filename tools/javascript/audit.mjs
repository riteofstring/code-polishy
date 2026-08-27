// The native vulnerability audit of one pnpm project, for the sealed,
// policy-owned JavaScript tool bundle.
//
// This is the one operation permitted to contact a registry, and the only one
// that runs another executable. The executable is the pinned pnpm installed
// beside this bundle, launched by the pinned Node already running this file:
// nothing is resolved from PATH, a target's node_modules, a user cache, or a
// global installation.
//
// pnpm reads its settings, its hooks, and its manifest from the directory it is
// pointed at, so it is never pointed at the target. The governed lockfile is
// copied into a policy-owned scratch directory holding nothing else, and that
// is the directory audited: a target .npmrc, .pnpmfile.cjs, workspace settings
// file, manifest, or installed package is not there to be read, and the audit
// installs nothing, so no lifecycle script and no target code runs.
//
// pnpm answers with the registry's own advisory objects. Those never cross the
// protocol: each one becomes an identity, a package, a severity, a title, and
// the exact installed versions it was reported against. Go decides the severity
// threshold, the assessments, the expiries, and how these reconcile with the
// independent OSV lane.

import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { fail, readTargetFile, truncate, unsupported } from "./protocol.mjs";

// The one registry a policy audit asks about a target's dependencies. It is
// passed on the command line, where npm configuration layering puts it above
// anything a target writes, so the advisory source is policy owned rather than
// target selected.
const REGISTRY = "https://registry.npmjs.org/";
// The one file a native audit needs, and the only target file it is given.
const LOCKFILE_NAME = "pnpm-lock.yaml";
// One exchange reports bounded facts. A report larger than either of these is a
// failed operation rather than a truncated answer.
const MAXIMUM_ADVISORIES = 2000;
const MAXIMUM_ADVISORY_VERSIONS = 500;
// The report itself is a registry answer, not a policy result, so it is bounded
// before it is ever parsed.
const MAXIMUM_REPORT_BYTES = 16 * 1024 * 1024;

const bundleDirectory = dirname(fileURLToPath(import.meta.url));

// The pinned pnpm, at the one installed path that holds it for this host. The
// bundle is one tree for every supported host and the runtimes sit beside it,
// so the host tuple is the only part of this path that varies.
function pinnedPNPM() {
  return join(
    bundleDirectory,
    "..",
    `${process.platform}-${process.arch}`,
    "pnpm",
    "bin",
    "pnpm.cjs",
  );
}

// One run of the pinned pnpm over the policy-owned directory holding the
// governed lock. The exit code is deliberately not consulted: pnpm exits
// non-zero exactly when it found something, which is the ordinary result, and a
// failure shows up as a report this reader cannot use.
function runPinnedPNPM(directory) {
  const result = spawnSync(
    process.execPath,
    [
      pinnedPNPM(),
      "audit",
      "--json",
      "--dir",
      directory,
      "--registry",
      REGISTRY,
    ],
    { cwd: directory, encoding: "utf8", maxBuffer: MAXIMUM_REPORT_BYTES },
  );
  if (result.error !== undefined) {
    fail(`the pinned pnpm could not be run: ${truncate(result.error.message)}`);
  }
  if (result.signal !== null) {
    fail(`the pinned pnpm was killed by ${result.signal}`);
  }
  return result;
}

// The one directory a native audit is pointed at: a policy-owned scratch tree
// holding exactly the governed lock text and nothing else. It is deleted as
// soon as the audit answers, so nothing the run leaves behind survives it.
function auditGovernedLock(lock) {
  const directory = mkdtempSync(join(tmpdir(), "code-polishy-audit-"));
  try {
    writeFileSync(join(directory, LOCKFILE_NAME), lock);
    return auditReport(directory);
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
}

// What the pinned pnpm reported, as a report this reader can use. pnpm writes
// its own refusals into the same JSON, and a refused audit is a failed
// operation rather than a project with no advisories against it.
function auditReport(directory) {
  const result = runPinnedPNPM(directory);
  let report;
  try {
    report = JSON.parse(result.stdout);
  } catch (error) {
    fail(
      `the pinned pnpm did not report an audit: ${truncate(result.stderr || result.stdout || error.message)}`,
    );
  }
  if (report === null || typeof report !== "object") {
    fail("the pinned pnpm reported an audit that is not a JSON object");
  }
  if (report.error !== undefined) {
    fail(
      `the pinned pnpm refused the audit: ${truncate(JSON.stringify(report.error))}`,
    );
  }
  return report;
}

// The exact installed versions one advisory was reported against. pnpm builds
// them from the lockfile it audited, so they are versions the target resolves;
// Go decides whether each one is an exact release.
function advisoryVersions(advisory) {
  if (!Array.isArray(advisory.findings)) {
    return null;
  }
  const versions = [];
  for (const finding of advisory.findings) {
    const version = finding?.version;
    if (typeof version !== "string" || version === "") {
      return null;
    }
    if (!versions.includes(version)) {
      versions.push(version);
    }
  }
  return versions.length > 0 && versions.length <= MAXIMUM_ADVISORY_VERSIONS
    ? versions
    : null;
}

// One reported field as the text it was written with. Anything else is absent
// rather than coerced, so a missing name never becomes the string "undefined".
function text(value) {
  return typeof value === "string" ? value : "";
}

// One advisory as the facts a policy decision needs. The GitHub advisory
// identity is preferred because it is the identity the independent OSV lane
// reports; the registry's own identifier stays as an alias so an assessment
// written against either one still names this advisory.
function advisoryFacts(key, advisory) {
  const packageName = text(advisory.module_name);
  const severity = text(advisory.severity);
  const versions = advisoryVersions(advisory);
  if (packageName === "" || severity === "" || versions === null) {
    return null;
  }
  const github = text(advisory.github_advisory_id);
  const registry = `npm:${key}`;
  return {
    id: github === "" ? registry : github,
    aliases: github === "" ? [] : [registry],
    package: packageName,
    severity,
    title: truncate(text(advisory.title)),
    versions,
  };
}

// Every advisory one report carries, bounded before any of it is read. A report
// without an advisory map is one this reader cannot use at all, which is a
// failed operation rather than a project with nothing reported against it.
function reportedAdvisories(report) {
  const advisories = report.advisories;
  if (
    advisories === null ||
    typeof advisories !== "object" ||
    Array.isArray(advisories)
  ) {
    fail("the pinned pnpm reported no advisory map");
  }
  const entries = Object.entries(advisories);
  if (entries.length > MAXIMUM_ADVISORIES) {
    fail(
      `the audit reported ${entries.length} advisories, more than the ${MAXIMUM_ADVISORIES} limit`,
    );
  }
  return entries;
}

// The native audit of the pnpm project rooted at the requested directory. The
// project is represented by its lockfile alone, read the same way every other
// lock decision reads it, so the audit is over exactly the governed lock data.
export function audit(request) {
  const path = join(request.directory, LOCKFILE_NAME);
  const unsupportedPaths = [];
  const lock = readTargetFile(join(request.root, path), path, unsupportedPaths);
  if (lock === null) {
    return { advisories: [], unsupported: unsupportedPaths };
  }
  const entries = reportedAdvisories(auditGovernedLock(lock));
  const reported = [];
  for (const [key, advisory] of entries) {
    const facts =
      advisory !== null && typeof advisory === "object"
        ? advisoryFacts(key, advisory)
        : null;
    if (facts === null) {
      // Reported rather than dropped: an advisory this reader cannot use is
      // missing coverage, never a package with nothing against it.
      unsupportedPaths.push(
        unsupported(
          path,
          `advisory '${key}' omitted the package, severity, or exact affected versions this reader requires`,
        ),
      );
      continue;
    }
    reported.push(facts);
  }
  return { advisories: reported, unsupported: unsupportedPaths };
}
