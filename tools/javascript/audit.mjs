import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { fail, readTargetFile, truncate, unsupported } from "./protocol.mjs";

const REGISTRY = "https://registry.npmjs.org/";

const LOCKFILE_NAME = "pnpm-lock.yaml";

const MAXIMUM_ADVISORIES = 2000;
const MAXIMUM_ADVISORY_VERSIONS = 500;

const MAXIMUM_REPORT_BYTES = 16 * 1024 * 1024;

const AUDIT_NETWORK_ARGUMENTS = [
  "--fetch-retries=4",
  "--fetch-retry-factor=2",
  "--fetch-retry-mintimeout=1000",
  "--fetch-retry-maxtimeout=5000",
  "--fetch-timeout=60000",
];

const bundleDirectory = dirname(fileURLToPath(import.meta.url));

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
      ...AUDIT_NETWORK_ARGUMENTS,
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

function auditGovernedLock(lock) {
  const directory = mkdtempSync(join(tmpdir(), "code-polishy-audit-"));
  try {
    writeFileSync(join(directory, LOCKFILE_NAME), lock);
    return auditReport(directory);
  } finally {
    rmSync(directory, { force: true, recursive: true });
  }
}

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

function text(value) {
  return typeof value === "string" ? value : "";
}

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
