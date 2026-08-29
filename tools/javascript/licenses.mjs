import { readdirSync } from "node:fs";
import { join } from "node:path";

import { fail, readTargetFile, truncate, unsupported } from "./protocol.mjs";

const MODULES_NAME = "node_modules";

const STORE_NAME = ".pnpm";
const MANIFEST_NAME = "package.json";

const MAXIMUM_PACKAGES = 20000;

function directoryEntries(absolute) {
  try {
    return readdirSync(absolute, { withFileTypes: true });
  } catch {
    return null;
  }
}

function text(value) {
  return typeof value === "string" ? value : "";
}

function hostedPackages(root, modules) {
  const hosted = [];
  for (const entry of directoryEntries(join(root, modules)) ?? []) {
    if (!entry.isDirectory()) {
      continue;
    }
    const path = join(modules, entry.name);
    if (!entry.name.startsWith("@")) {
      hosted.push(path);
      continue;
    }
    for (const scoped of directoryEntries(join(root, path)) ?? []) {
      if (scoped.isDirectory()) {
        hosted.push(join(path, scoped.name));
      }
    }
  }
  return hosted;
}

function readManifest(root, path, unsupportedPaths) {
  const source = readTargetFile(join(root, path), path, unsupportedPaths);
  if (source === null) {
    return null;
  }
  let manifest;
  try {
    manifest = JSON.parse(source);
  } catch (error) {
    unsupportedPaths.push(
      unsupported(path, `the manifest is not readable JSON: ${error.message}`),
    );
    return null;
  }
  if (manifest === null || typeof manifest !== "object") {
    unsupportedPaths.push(
      unsupported(path, "the manifest is not a JSON object"),
    );
    return null;
  }
  return manifest;
}

function declaredLicense(root, directory, unsupportedPaths) {
  const path = join(directory, MANIFEST_NAME);
  const manifest = readManifest(root, path, unsupportedPaths);
  if (manifest === null) {
    return null;
  }
  const name = text(manifest.name);
  const version = text(manifest.version);
  if (name === "" || version === "") {
    unsupportedPaths.push(
      unsupported(path, "the manifest names no exact installed release"),
    );
    return null;
  }
  return { name, version, license: truncate(text(manifest.license)) };
}

function reportRelease(installed, reported, seen) {
  const key = `${installed.name}@${installed.version}`;
  if (seen.has(key)) {
    return;
  }
  seen.add(key);
  reported.push(installed);
  if (reported.length > MAXIMUM_PACKAGES) {
    fail(
      `the licenses operation reports more than the ${MAXIMUM_PACKAGES} package limit`,
    );
  }
}

function unreadableTree(root, project) {
  return directoryEntries(join(root, project)) === null
    ? "the dependencies of this project are not installed, so no license metadata exists to read"
    : "the installed tree has no isolated pnpm store, so its license metadata cannot be attributed to exact releases";
}

export function licenses(request) {
  const project = join(request.directory, MODULES_NAME);
  const store = join(project, STORE_NAME);
  const entries = directoryEntries(join(request.root, store));
  if (entries === null) {
    const reason = unreadableTree(request.root, project);
    return { packages: [], unsupported: [unsupported(store, reason)] };
  }
  const reported = [];
  const seen = new Set();
  const unsupportedPaths = [];
  for (const entry of entries) {
    if (!entry.isDirectory()) {
      continue;
    }
    const modules = join(store, entry.name, MODULES_NAME);
    for (const directory of hostedPackages(request.root, modules)) {
      const installed = declaredLicense(
        request.root,
        directory,
        unsupportedPaths,
      );
      if (installed !== null) {
        reportRelease(installed, reported, seen);
      }
    }
  }
  return { packages: reported, unsupported: unsupportedPaths };
}
