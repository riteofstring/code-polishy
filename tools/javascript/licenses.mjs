// The license every installed dependency of one pnpm project declares, for the
// sealed, policy-owned JavaScript tool bundle.
//
// A lockfile records what a target installs, never what those packages are
// licensed under: that fact exists only in each installed package's own
// manifest. So this reads the project's own virtual store, where pnpm keeps one
// real directory per resolved release, and reports the exact expression each
// manifest wrote. Nothing here decides what an expression means: Go parses it
// and enforces the configured license policy.
//
// Reading is metadata only. A manifest is JSON data, so no target code, install
// script, or executable configuration runs, and no registry is contacted.

import { readdirSync } from "node:fs";
import { join } from "node:path";

import { fail, readTargetFile, truncate, unsupported } from "./protocol.mjs";

const MODULES_NAME = "node_modules";
// pnpm's virtual store: one directory per resolved release, holding that
// release's own files and links to everything it depends on.
const STORE_NAME = ".pnpm";
const MANIFEST_NAME = "package.json";
// One exchange reports bounded facts. A project with more installed releases
// than this is reported as a failed operation rather than handed back partial.
const MAXIMUM_PACKAGES = 20000;

// The entries of one directory, or null when it cannot be read at all. A
// directory this reader cannot open is missing coverage rather than an empty
// one, and the caller that asked for it decides what its absence means.
function directoryEntries(absolute) {
  try {
    return readdirSync(absolute, { withFileTypes: true });
  } catch {
    return null;
  }
}

// One reported field as the text it was written with. Anything else is absent
// rather than coerced, so a manifest that wrote a number never reports one.
function text(value) {
  return typeof value === "string" ? value : "";
}

// Every real package directory inside one store entry. pnpm links each
// dependency of the hosted release into the same directory, so the links are
// exactly what this skips; a scoped package sits one level further down.
function hostedPackages(root, modules) {
  const hosted = [];
  for (const entry of directoryEntries(join(root, modules)) ?? []) {
    // A symlink is a dependency of the hosted release, and the store entry
    // that really holds it reports it already.
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

// One installed manifest as data, or null when this reader cannot use it at
// all. Each refusal is recorded so missing coverage names the exact file.
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

// One installed release as the facts a license decision needs: which release it
// is, and the exact expression its own manifest declared. A license written as
// anything but one string reports as declaring none: a legacy object or array
// is not an SPDX expression, and inventing one from it would admit a package
// under a license nobody wrote.
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

// Report one installed release. The same release is stored once per peer
// resolution and every copy of it declares the same license, so it is reported
// once.
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

// Why an installed tree could not be read. A project whose dependencies are not
// installed and one installed without the isolated store this reader attributes
// metadata to exact releases with are different answers, so it gives both.
function unreadableTree(root, project) {
  return directoryEntries(join(root, project)) === null
    ? "the dependencies of this project are not installed, so no license metadata exists to read"
    : "the installed tree has no isolated pnpm store, so its license metadata cannot be attributed to exact releases";
}

// The declared license of every release installed for the pnpm project rooted
// at the requested directory.
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
