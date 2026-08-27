// Package, workspace, and resolved-graph facts of one pnpm project, for the
// sealed, policy-owned JavaScript tool bundle.
//
// A pnpm lockfile is the exact record of what a target installs: which packages
// the workspace contains, what each of them declared, what every declaration
// resolved to, and where each resolved package came from. Its workspace file is
// the record of the settings pnpm resolves under, including the native
// supply-chain protections and the lifecycle-script policy. Go enforces pins,
// lock consistency, source policy, release age, lifecycle ownership, and
// vulnerabilities from those facts; nothing here decides any of them.
//
// Both files are YAML, so they are read with the same parser pnpm reads them
// with rather than approximated by scanning lines. Only the one lockfile format
// this bundle admits is interpreted: a lock written in another version is
// reported rather than read under the assumption that its shape did not change.

import { basename, join } from "node:path";
// Resolved relative to this file rather than as a bare specifier, so the parser
// can only ever be the one installed beside the runner.
import yaml from "./node_modules/js-yaml/dist/js-yaml.mjs";

import { fail, readTargetFile, unsupported } from "./protocol.mjs";

const LOCKFILE_NAME = "pnpm-lock.yaml";
// The two names pnpm reads a workspace's own settings from. This operation
// reads a settings file and nothing else, so it never becomes a way to hand
// arbitrary target YAML to the policy engine.
const WORKSPACE_NAMES = ["pnpm-workspace.yaml", "pnpm-workspace.yml"];
// The one lockfile format this bundle reads. pnpm changes what these sections
// mean between formats, so another version is reported rather than guessed.
const SUPPORTED_LOCKFILE_VERSION = "9.0";
// One exchange reports bounded facts. A project larger than any of these is
// reported as a failed operation rather than handed back truncated.
const MAXIMUM_IMPORTERS = 2000;
const MAXIMUM_PACKAGES = 20000;
const MAXIMUM_DEPENDENCIES = 40000;
const MAXIMUM_SETTINGS = 500;
const MAXIMUM_SETTING_VALUES = 5000;
const MAXIMUM_SNAPSHOT_EDGES = 100000;
// Malformed platform data is reported as unsupported coverage instead of being
// coerced into a platform exclusion. Keep those reports bounded just like the
// graph facts they accompany.
const MAXIMUM_PLATFORM_ERRORS = 1000;

// The only answers the package reader gives about whether an installed release
// should have license metadata on this host. Go owns the policy decision these
// facts support; the names stay closed so a newer reader cannot silently add a
// permissive state for an older policy engine.
const LICENSE_METADATA_REQUIRED = "required";
const LICENSE_METADATA_PLATFORM_EXCLUDED = "platform-excluded";
const LICENSE_METADATA_UNKNOWN = "unknown";
const LICENSE_METADATA_RANK = {
  [LICENSE_METADATA_PLATFORM_EXCLUDED]: 0,
  [LICENSE_METADATA_UNKNOWN]: 1,
  [LICENSE_METADATA_REQUIRED]: 2,
};

// The three dependency groups pnpm resolves into the lock. peerDependencies is
// deliberately absent: pnpm resolves no peer declaration into the lock, so
// comparing one against it would report every peer as drift. Go reads peer
// declarations from the manifest it already parses.
const SCOPES = [
  { field: "dependencies", scope: "runtime" },
  { field: "devDependencies", scope: "development" },
  { field: "optionalDependencies", scope: "optional" },
];

// A package key or a resolved alias, as "<name>@<rest>". The name may be
// scoped; the remainder is whatever pnpm resolved to, which is an exact version
// for a registry package and a URL, path, or revision for anything else.
const PACKAGE_KEY = /^((?:@[^/@]+\/)?[^@/]+)@(.+)$/;

// A section pnpm did not write is absent rather than empty, and a scalar where
// a map belongs is a lock this operation must not interpret.
function isMap(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function mapAt(value, key) {
  const found = value?.[key];
  return isMap(found) ? found : null;
}

function stringAt(value, key) {
  const found = value?.[key];
  return typeof found === "string" ? found : "";
}

// Whether a path built from lock content still names something inside the
// target tree. A lock may point anywhere; a reported fact may not.
function escapes(path) {
  return path === ".." || path.startsWith("../");
}

// The lockfile, parsed under the JSON schema so only the scalar types pnpm
// writes are admitted and no YAML tag can construct anything else.
function readLock(root, directory, unsupportedPaths) {
  const path = join(directory, LOCKFILE_NAME);
  const text = readTargetFile(join(root, path), path, unsupportedPaths);
  if (text === null) {
    return null;
  }
  let document;
  try {
    document = yaml.load(text, { filename: path, schema: yaml.JSON_SCHEMA });
  } catch (error) {
    unsupportedPaths.push(unsupported(path, error.message));
    return null;
  }
  if (!isMap(document)) {
    unsupportedPaths.push(unsupported(path, "the lockfile is not a YAML map"));
    return null;
  }
  const version = String(document.lockfileVersion ?? "");
  if (version !== SUPPORTED_LOCKFILE_VERSION) {
    unsupportedPaths.push(
      unsupported(
        path,
        `the lockfile declares version '${version}', not ${SUPPORTED_LOCKFILE_VERSION}`,
      ),
    );
    return null;
  }
  return { path, document };
}

// Where one resolved package came from, decided by the source the lock names
// rather than by the fields it happens to carry. A tarball resolution records
// the URL the target chose to download, and pnpm records integrity over those
// bytes too: that integrity says the download did not change in transit, not
// that a registry published it, so the URL is what the source is. Only a
// resolution naming no other source and recording integrity is a registry
// release; every other kind is reported as what it is so Go can refuse it by
// name.
function resolutionSource(resolution) {
  if (resolution === null) {
    return "unknown";
  }
  const type = stringAt(resolution, "type");
  if (type === "directory" || type === "git") {
    return type;
  }
  if (resolution.tarball !== undefined) {
    return "tarball";
  }
  if (typeof resolution.integrity === "string" && resolution.integrity !== "") {
    return "registry";
  }
  return "unknown";
}

// One malformed platform declaration is not a reason to reinterpret the
// release as absent on this host. It remains an explicit unsupported lock fact
// and its package fact becomes unknown, which keeps the later license decision
// fail-closed without handing unbounded parser diagnostics across the protocol.
function malformedPlatform(lock, packageKey, reason, unsupportedPaths) {
  if (unsupportedPaths.length >= MAXIMUM_PLATFORM_ERRORS) {
    fail(
      `the packages operation reports more than the ${MAXIMUM_PLATFORM_ERRORS} malformed platform declarations`,
    );
  }
  unsupportedPaths.push(
    unsupported(lock.path, `resolved package '${packageKey}' ${reason}`),
  );
}

// A package selector is either absent or the exact array of strings pnpm wrote.
// Scalars, nulls, empty strings, and a bare negation are not normalized: an
// optional package can be classified as excluded only from unambiguous lock
// metadata.
function platformSelector(lock, packageKey, entry, field, unsupportedPaths) {
  if (!isMap(entry) || !Object.hasOwn(entry, field)) {
    return [];
  }
  const selectors = entry[field];
  if (
    !Array.isArray(selectors) ||
    selectors.some(
      (selector) =>
        typeof selector !== "string" || selector === "" || selector === "!",
    )
  ) {
    malformedPlatform(
      lock,
      packageKey,
      `declares malformed ${field} selectors`,
      unsupportedPaths,
    );
    return null;
  }
  return selectors;
}

// pnpm/npm list semantics: one exact negative forbids the host, a positive
// list needs one exact match, and an all-negative list admits every value it
// does not explicitly forbid. "any" is special only when it is the sole entry,
// matching pnpm's platform checker rather than broadening a mixed selector.
function selectorMatches(value, selectors) {
  if (
    selectors.length === 0 ||
    (selectors.length === 1 && selectors[0] === "any")
  ) {
    return true;
  }
  let positiveMatch = false;
  for (const selector of selectors) {
    if (selector.startsWith("!")) {
      if (selector.slice(1) === value) {
        return false;
      }
      continue;
    }
    positiveMatch = positiveMatch || selector === value;
  }
  return (
    positiveMatch || selectors.every((selector) => selector.startsWith("!"))
  );
}

let libcRead = false;
let runtimeLibc = "";

function nodeRuntimeReport() {
  if (
    process.platform !== "linux" ||
    process.report === undefined ||
    typeof process.report.getReport !== "function"
  ) {
    return null;
  }
  try {
    return process.report.getReport();
  } catch {
    return null;
  }
}

function isMuslSharedObject(entry) {
  return (
    typeof entry === "string" &&
    (entry.includes("libc.musl-") || entry.includes("ld-musl-"))
  );
}

function reportedLibc(report) {
  if (
    typeof report?.header?.glibcVersionRuntime === "string" &&
    report.header.glibcVersionRuntime !== ""
  ) {
    return "glibc";
  }
  if (
    Array.isArray(report?.sharedObjects) &&
    report.sharedObjects.some(isMuslSharedObject)
  ) {
    return "musl";
  }
  return "";
}

// The policy-owned Node runtime knows whether it runs against glibc, and its
// report can identify musl without consulting a target file or starting a
// helper process. A result that cannot be established this way stays unknown.
function currentLibc() {
  if (libcRead) {
    return runtimeLibc;
  }
  libcRead = true;
  const report = nodeRuntimeReport();
  if (report !== null) {
    runtimeLibc = reportedLibc(report);
  }
  return runtimeLibc;
}

function platformSelectors(lock, packageKey, entry, unsupportedPaths) {
  const selectors = {
    os: platformSelector(lock, packageKey, entry, "os", unsupportedPaths),
    cpu: platformSelector(lock, packageKey, entry, "cpu", unsupportedPaths),
    libc: platformSelector(lock, packageKey, entry, "libc", unsupportedPaths),
  };
  return Object.values(selectors).some((value) => value === null)
    ? null
    : selectors;
}

function runtimePlatformMatches(selectors) {
  return (
    selectorMatches(process.platform, selectors.os) &&
    selectorMatches(process.arch, selectors.cpu)
  );
}

function needsLibcComparison(libc) {
  return !(
    process.platform !== "linux" ||
    libc.length === 0 ||
    (libc.length === 1 && libc[0] === "any")
  );
}

function libcLicenseMetadata(libc) {
  const detected = currentLibc();
  if (detected === "") {
    return LICENSE_METADATA_UNKNOWN;
  }
  return selectorMatches(detected, libc)
    ? LICENSE_METADATA_REQUIRED
    : LICENSE_METADATA_PLATFORM_EXCLUDED;
}

// Whether the release's selectors make installed metadata necessary on this
// host. This intentionally uses only the Node runtime that owns this bundle;
// it neither asks pnpm to resolve anything nor reads executable target config.
function platformLicenseMetadata(lock, packageKey, entry, unsupportedPaths) {
  const selectors = platformSelectors(
    lock,
    packageKey,
    entry,
    unsupportedPaths,
  );
  if (selectors === null) {
    return LICENSE_METADATA_UNKNOWN;
  }
  if (!runtimePlatformMatches(selectors)) {
    return LICENSE_METADATA_PLATFORM_EXCLUDED;
  }
  // libc is a Linux concern. On another OS pnpm has no libc family to compare,
  // so a valid os/cpu-compatible release remains host-required.
  if (!needsLibcComparison(selectors.libc)) {
    return LICENSE_METADATA_REQUIRED;
  }
  return libcLicenseMetadata(selectors.libc);
}

// A snapshot identifies one peer-resolution context of a package entry. The
// package key is the part before any peer suffix, and a malformed snapshot key
// cannot contribute evidence that its release is optional.
function snapshotPackageKey(key) {
  const identity = PACKAGE_KEY.exec(key);
  if (identity === null) {
    return "";
  }
  const peer = identity[2].indexOf("(");
  const version = peer < 0 ? identity[2] : identity[2].slice(0, peer);
  return version === "" ? "" : `${identity[1]}@${version}`;
}

// Indexing snapshots once keeps classification linear in the lock graph even
// when one exact release appears under many peer contexts.
function packageSnapshots(lock, unsupportedPaths) {
  if (lock.document.snapshots === undefined) {
    return { byKey: new Map() };
  }
  if (!isMap(lock.document.snapshots)) {
    malformedPlatform(lock, "snapshots", "is not a YAML map", unsupportedPaths);
    return { byKey: new Map() };
  }
  const byKey = new Map();
  for (const [key, value] of Object.entries(lock.document.snapshots)) {
    const packageKey = snapshotPackageKey(key);
    if (packageKey === "") {
      malformedPlatform(
        lock,
        key,
        "has a snapshot key that does not name a package release",
        unsupportedPaths,
      );
      continue;
    }
    const context = {
      key,
      packageKey,
      value,
      optional: snapshotOptional(lock, { key, value }, unsupportedPaths),
    };
    byKey.set(key, context);
  }
  return { byKey };
}

// A snapshot that says optional only as a boolean can prove that pnpm may have
// omitted it. Any other shape is unknown rather than truthy or falsey.
function snapshotOptional(lock, context, unsupportedPaths) {
  if (!isMap(context.value)) {
    malformedPlatform(
      lock,
      context.key,
      "has a snapshot that is not a YAML map",
      unsupportedPaths,
    );
    return null;
  }
  if (!Object.hasOwn(context.value, "optional")) {
    return false;
  }
  if (typeof context.value.optional !== "boolean") {
    malformedPlatform(
      lock,
      context.key,
      "has a snapshot with a non-boolean optional flag",
      unsupportedPaths,
    );
    return null;
  }
  return context.value.optional;
}

function moreConservativeMetadata(left, right) {
  return LICENSE_METADATA_RANK[left] >= LICENSE_METADATA_RANK[right]
    ? left
    : right;
}

// One optional occurrence is present only when both its parent path and its own
// platform selectors admit this host. An exclusion on either side is enough to
// prove absence; otherwise an undecidable side keeps the occurrence unknown.
function optionalOccurrenceMetadata(parent, platform) {
  if (
    parent === LICENSE_METADATA_PLATFORM_EXCLUDED ||
    platform === LICENSE_METADATA_PLATFORM_EXCLUDED
  ) {
    return LICENSE_METADATA_PLATFORM_EXCLUDED;
  }
  if (
    parent === LICENSE_METADATA_UNKNOWN ||
    platform === LICENSE_METADATA_UNKNOWN
  ) {
    return LICENSE_METADATA_UNKNOWN;
  }
  return LICENSE_METADATA_REQUIRED;
}

function contextOccurrenceMetadata(context, parent, platforms) {
  if (context.optional === false) {
    return LICENSE_METADATA_REQUIRED;
  }
  if (context.optional === null) {
    return LICENSE_METADATA_UNKNOWN;
  }
  return optionalOccurrenceMetadata(
    parent,
    platforms.get(context.packageKey) ?? LICENSE_METADATA_UNKNOWN,
  );
}

// A dependency value names its exact snapshot context, including any peer
// suffix. Aliases write the resolved package name into the value; ordinary
// dependencies inherit the declaration's name. Workspace links have no
// registry snapshot to classify.
function dependencySnapshotKey(name, version) {
  if (
    typeof version !== "string" ||
    version === "" ||
    version.startsWith("link:")
  ) {
    return "";
  }
  const peer = version.indexOf("(");
  const base = peer < 0 ? version : version.slice(0, peer);
  const suffix = peer < 0 ? "" : version.slice(peer);
  const alias = PACKAGE_KEY.exec(base);
  return alias === null
    ? `${name}@${version}`
    : `${alias[1]}@${alias[2]}${suffix}`;
}

function dependencyEntries(lock, ownerKey, owner, field, unsupportedPaths) {
  if (!isMap(owner) || !Object.hasOwn(owner, field)) {
    return [];
  }
  if (!isMap(owner[field])) {
    malformedPlatform(
      lock,
      ownerKey,
      `has ${field} that is not a YAML map`,
      unsupportedPaths,
    );
    return [];
  }
  return Object.entries(owner[field]);
}

function snapshotEdges(lock, snapshots, unsupportedPaths) {
  const edges = new Map();
  let count = 0;
  for (const context of snapshots.byKey.values()) {
    const targets = [];
    for (const field of ["dependencies", "optionalDependencies"]) {
      for (const [name, version] of dependencyEntries(
        lock,
        context.key,
        context.value,
        field,
        unsupportedPaths,
      )) {
        const target = dependencySnapshotKey(name, version);
        if (target === "") {
          malformedPlatform(
            lock,
            context.key,
            `has ${field} entry '${name}' with no exact snapshot version`,
            unsupportedPaths,
          );
          continue;
        }
        if (!snapshots.byKey.has(target)) {
          malformedPlatform(
            lock,
            context.key,
            `has ${field} entry '${name}' naming absent snapshot '${target}'`,
            unsupportedPaths,
          );
          continue;
        }
        targets.push(target);
        count += 1;
        if (count > MAXIMUM_SNAPSHOT_EDGES) {
          fail(
            `the packages operation traverses more than the ${MAXIMUM_SNAPSHOT_EDGES} snapshot dependency limit`,
          );
        }
      }
    }
    edges.set(context.key, targets);
  }
  return edges;
}

function importerRoot(lock, reference, snapshots, unsupportedPaths) {
  const version = stringAt(reference.resolution, "version");
  if (version.startsWith("link:")) {
    return null;
  }
  const target = dependencySnapshotKey(reference.name, version);
  if (target === "") {
    malformedPlatform(
      lock,
      reference.importer,
      `has ${reference.field} entry '${reference.name}' with no exact snapshot version`,
      unsupportedPaths,
    );
    return null;
  }
  const context = snapshots.byKey.get(target);
  return context === undefined
    ? null
    : { context, optional: reference.field === "optionalDependencies" };
}

function importerRoots(lock, snapshots, unsupportedPaths) {
  const roots = [];
  const importers = mapAt(lock.document, "importers") ?? {};
  for (const [importer, entry] of Object.entries(importers)) {
    for (const field of [
      "dependencies",
      "devDependencies",
      "optionalDependencies",
    ]) {
      for (const [name, resolution] of dependencyEntries(
        lock,
        importer,
        entry,
        field,
        unsupportedPaths,
      )) {
        const root = importerRoot(
          lock,
          { importer, field, name, resolution },
          snapshots,
          unsupportedPaths,
        );
        if (root !== null) {
          roots.push(root);
        }
      }
    }
  }
  return roots;
}

function updateContextMetadata(states, key, metadata, queue) {
  const previous = states.get(key);
  const next =
    previous === undefined
      ? metadata
      : moreConservativeMetadata(previous, metadata);
  if (next !== previous) {
    states.set(key, next);
    queue.push(key);
  }
}

function propagateContextMetadata(states, queue, edges, snapshots, platforms) {
  for (let position = 0; position < queue.length; position += 1) {
    const parentKey = queue[position];
    const parent = states.get(parentKey);
    for (const target of edges.get(parentKey) ?? []) {
      const context = snapshots.byKey.get(target);
      updateContextMetadata(
        states,
        target,
        contextOccurrenceMetadata(context, parent, platforms),
        queue,
      );
    }
  }
}

function seedDeclaredContexts(snapshots, states, queue) {
  for (const context of snapshots.byKey.values()) {
    if (context.optional === false) {
      updateContextMetadata(
        states,
        context.key,
        LICENSE_METADATA_REQUIRED,
        queue,
      );
    } else if (context.optional === null) {
      updateContextMetadata(
        states,
        context.key,
        LICENSE_METADATA_UNKNOWN,
        queue,
      );
    }
  }
}

function seedImporterContexts(roots, platforms, states, queue) {
  for (const root of roots) {
    const metadata = root.optional
      ? contextOccurrenceMetadata(
          root.context,
          LICENSE_METADATA_REQUIRED,
          platforms,
        )
      : LICENSE_METADATA_REQUIRED;
    updateContextMetadata(states, root.context.key, metadata, queue);
  }
}

function seedUnreachedContexts(snapshots, platforms, states) {
  const queue = [];
  for (const context of snapshots.byKey.values()) {
    if (!states.has(context.key)) {
      updateContextMetadata(
        states,
        context.key,
        contextOccurrenceMetadata(
          context,
          LICENSE_METADATA_REQUIRED,
          platforms,
        ),
        queue,
      );
    }
  }
  return queue;
}

function packageContextMetadata(snapshots, states) {
  const byPackage = new Map();
  for (const context of snapshots.byKey.values()) {
    const metadata = states.get(context.key) ?? LICENSE_METADATA_UNKNOWN;
    const previous = byPackage.get(context.packageKey);
    byPackage.set(
      context.packageKey,
      previous === undefined
        ? metadata
        : moreConservativeMetadata(previous, metadata),
    );
  }
  return byPackage;
}

// A normal install materializes a package only along a host-compatible path
// from an importer. pnpm propagates optional state into transitive snapshots,
// so a universal child of an excluded optional package is excluded too. A
// required or compatible path to the same context still dominates, as does a
// required peer context for the same exact release.
function licenseMetadataByPackage(lock, entries, snapshots, unsupportedPaths) {
  const platforms = new Map();
  for (const [packageKey, entry] of Object.entries(entries)) {
    platforms.set(
      packageKey,
      platformLicenseMetadata(lock, packageKey, entry, unsupportedPaths),
    );
  }
  const edges = snapshotEdges(lock, snapshots, unsupportedPaths);
  const states = new Map();
  const queue = [];
  seedDeclaredContexts(snapshots, states, queue);
  seedImporterContexts(
    importerRoots(lock, snapshots, unsupportedPaths),
    platforms,
    states,
    queue,
  );
  propagateContextMetadata(states, queue, edges, snapshots, platforms);

  // Synthetic and partially pruned fixtures can carry optional snapshots that
  // no importer or parent reaches. Preserve the previous conservative local
  // decision for those contexts, then let it flow through their descendants.
  const fallbackQueue = seedUnreachedContexts(snapshots, platforms, states);
  propagateContextMetadata(states, fallbackQueue, edges, snapshots, platforms);
  return packageContextMetadata(snapshots, states);
}

// Every package the lock resolves, as a name, whatever it resolved to, and the
// kind of source it came from. A key this format does not describe is reported
// rather than split on a guess.
function resolvedPackages(lock, unsupportedPaths) {
  const entries = mapAt(lock.document, "packages") ?? {};
  const snapshots = packageSnapshots(lock, unsupportedPaths);
  const metadata = licenseMetadataByPackage(
    lock,
    entries,
    snapshots,
    unsupportedPaths,
  );
  const result = [];
  for (const [key, value] of Object.entries(entries)) {
    const identity = PACKAGE_KEY.exec(key);
    if (identity === null) {
      unsupportedPaths.push(
        unsupported(lock.path, `resolved package key '${key}' has no version`),
      );
      continue;
    }
    result.push({
      name: identity[1],
      version: identity[2],
      source: resolutionSource(mapAt(value, "resolution")),
      licenseMetadata: metadata.get(key) ?? LICENSE_METADATA_REQUIRED,
    });
    if (result.length > MAXIMUM_PACKAGES) {
      fail(
        `the packages operation resolves more than the ${MAXIMUM_PACKAGES} package limit`,
      );
    }
  }
  return result;
}

// What one declaration in an importer resolved to. pnpm records a workspace or
// linked dependency as a path, an aliased dependency under the name it really
// resolved, and everything else as the version alone. The peer suffix a version
// may carry names how that package's own peers resolved; the package it
// identifies is the same one either way, so it is not part of the identity.
function dependencyResolution(name, version, importer, lock, unsupportedPaths) {
  const empty = { resolvedName: "", resolvedVersion: "", link: "" };
  if (version === "") {
    return empty;
  }
  if (version.startsWith("link:")) {
    const target = join(importer, version.slice(5));
    if (escapes(target)) {
      unsupportedPaths.push(
        unsupported(
          lock.path,
          `'${name}' links to '${version.slice(5)}', which is outside the repository`,
        ),
      );
      return empty;
    }
    return { ...empty, link: target };
  }
  const peer = version.indexOf("(");
  const base = peer < 0 ? version : version.slice(0, peer);
  const alias = PACKAGE_KEY.exec(base);
  return alias === null
    ? { resolvedName: name, resolvedVersion: base, link: "" }
    : { resolvedName: alias[1], resolvedVersion: alias[2], link: "" };
}

// One importer's declared dependencies, read from its own manifest. A manifest
// that cannot be read contributes no declarations, so everything the lock
// resolved for it is reported as resolved but undeclared rather than as clean.
function declaredDependencies(root, manifest, unsupportedPaths) {
  const text = readTargetFile(join(root, manifest), manifest, unsupportedPaths);
  if (text === null) {
    return {};
  }
  try {
    return JSON.parse(text);
  } catch (error) {
    unsupportedPaths.push(unsupported(manifest, error.message));
    return {};
  }
}

// One importer's declarations, from both sides at once: what its manifest
// declares and what the lock resolved for it. A declaration missing from either
// side is reported with that side empty, because a lock resolving something no
// manifest asked for and a manifest asking for something the lock never
// resolved are both drift Go decides on.
function importerDependencies(
  request,
  importer,
  entry,
  lock,
  unsupportedPaths,
) {
  const manifest = join(importer, "package.json");
  const declared = declaredDependencies(
    request.root,
    manifest,
    unsupportedPaths,
  );
  const dependencies = [];
  for (const { field, scope } of SCOPES) {
    const locked = mapAt(entry, field) ?? {};
    const written = mapAt(declared, field) ?? {};
    const names = new Set([...Object.keys(written), ...Object.keys(locked)]);
    for (const name of [...names].sort()) {
      const resolution = mapAt(locked, name);
      dependencies.push({
        name,
        scope,
        declared: typeof written[name] === "string" ? written[name] : "",
        specifier: stringAt(resolution, "specifier"),
        ...dependencyResolution(
          name,
          stringAt(resolution, "version"),
          importer,
          lock,
          unsupportedPaths,
        ),
      });
    }
  }
  return { path: importer, manifest, dependencies };
}

// Every package of the workspace the lock covers. pnpm writes one importer per
// workspace package, so the lock is also the workspace inventory: a package it
// omits is a package the target never installed.
function lockImporters(request, lock, unsupportedPaths) {
  const entries = mapAt(lock.document, "importers");
  if (entries === null) {
    unsupportedPaths.push(
      unsupported(lock.path, "the lockfile declares no importers"),
    );
    return [];
  }
  const keys = Object.keys(entries).sort();
  if (keys.length > MAXIMUM_IMPORTERS) {
    fail(
      `the packages operation covers more than the ${MAXIMUM_IMPORTERS} workspace package limit`,
    );
  }
  const importers = [];
  let dependencies = 0;
  for (const key of keys) {
    const importer = join(request.directory, key);
    if (escapes(importer)) {
      unsupportedPaths.push(
        unsupported(lock.path, `importer '${key}' is outside the repository`),
      );
      continue;
    }
    const reported = importerDependencies(
      request,
      importer,
      entries[key],
      lock,
      unsupportedPaths,
    );
    dependencies += reported.dependencies.length;
    if (dependencies > MAXIMUM_DEPENDENCIES) {
      fail(
        `the packages operation covers more than the ${MAXIMUM_DEPENDENCIES} declared dependency limit`,
      );
    }
    importers.push(reported);
  }
  return importers;
}

// Report the workspace, its declarations, and the resolved graph of one pnpm
// project. Go owns every decision that follows: which declaration must be
// exact, whether the lock still matches the manifests, which sources are
// admissible, how old a release may be, and what a vulnerability means.
export function packages(request) {
  const unsupportedPaths = [];
  const lock = readLock(request.root, request.directory, unsupportedPaths);
  if (lock === null) {
    return {
      lockfileVersion: "",
      importers: [],
      packages: [],
      unsupported: unsupportedPaths,
    };
  }
  return {
    lockfileVersion: SUPPORTED_LOCKFILE_VERSION,
    importers: lockImporters(request, lock, unsupportedPaths),
    packages: resolvedPackages(lock, unsupportedPaths),
    unsupported: unsupportedPaths,
  };
}

// Every scalar one setting is written with, in document order. pnpm writes a
// setting as one scalar, as a sequence of them, or as a map whose values are,
// and a policy decision compares the text either way, so each leaf crosses the
// protocol as the text it was written as. A setting written with no scalar at
// all reports none, which is exactly how an empty allowlist reads.
function settingValues(value, values) {
  if (value === null || value === undefined) {
    return values;
  }
  if (Array.isArray(value) || isMap(value)) {
    for (const item of Object.values(value)) {
      settingValues(item, values);
    }
    return values;
  }
  values.push(String(value));
  if (values.length > MAXIMUM_SETTING_VALUES) {
    fail(
      `the workspace operation reports more than the ${MAXIMUM_SETTING_VALUES} setting value limit`,
    );
  }
  return values;
}

// What one workspace file declares, under the JSON schema so only the scalar
// types pnpm writes are admitted and no YAML tag can construct anything else.
// A file that declares nothing is settings this run has, not coverage it lacks.
function workspaceSettings(root, path, unsupportedPaths) {
  const text = readTargetFile(join(root, path), path, unsupportedPaths);
  if (text === null) {
    return null;
  }
  let document;
  try {
    document = yaml.load(text, { filename: path, schema: yaml.JSON_SCHEMA });
  } catch (error) {
    unsupportedPaths.push(unsupported(path, error.message));
    return null;
  }
  if (document === null || document === undefined) {
    return [];
  }
  if (!isMap(document)) {
    unsupportedPaths.push(
      unsupported(path, "the workspace file is not a YAML map"),
    );
    return null;
  }
  const names = Object.keys(document);
  if (names.length > MAXIMUM_SETTINGS) {
    fail(
      `the workspace operation covers more than the ${MAXIMUM_SETTINGS} setting limit`,
    );
  }
  return names.map((name) => ({
    name,
    values: settingValues(document[name], []),
  }));
}

// Report what each named pnpm workspace file declares. Go owns which file may
// own a workspace's settings, which of them a pinned pnpm version must carry,
// and what every declared value has to be; this operation only says what is
// written.
export function workspace(request) {
  const unsupportedPaths = [];
  const files = [];
  for (const path of request.paths) {
    if (!WORKSPACE_NAMES.includes(basename(path))) {
      fail(
        `the workspace operation reads ${WORKSPACE_NAMES.join(" or ")}, not '${path}'`,
      );
    }
    const settings = workspaceSettings(request.root, path, unsupportedPaths);
    if (settings !== null) {
      files.push({ path, settings });
    }
  }
  return { files, unsupported: unsupportedPaths };
}
