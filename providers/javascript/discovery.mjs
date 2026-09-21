import { basename, dirname, join } from "node:path";

export function discover(request) {
  const inputs = new Set(request.inventory.map((entry) => entry.path));
  const packages = new Map();
  const configurations = new Map();
  for (const entry of request.inventory) {
    if (basename(entry.path) === "package.json")
      packages.set(dirname(entry.path), entry.path);
    if (javaScriptConfiguration(entry.path)) {
      const root = dirname(entry.path);
      const candidates = configurations.get(root) ?? [];
      candidates.push(entry.path);
      configurations.set(root, candidates);
    }
  }
  const units = new Map();
  for (const entry of request.inventory) {
    if (!entry.source || entry.owner !== request.provider) continue;
    const unit = unitFor(entry, packages, configurations);
    if (!units.has(unit.id)) units.set(unit.id, unit);
    const current = units.get(unit.id);
    current.members.push(entry.path);
    if (entryPoint(current.packageRoot, entry.path, entry.test))
      current.entryFiles.push(entry.path);
  }
  const triggers = new Set([
    ...(request.selection?.paths ?? []),
    ...(request.selection?.deleted ?? []),
  ]);
  const scopes = [...units.values()].map((unit) => {
    const context = [...inputs].filter((candidate) =>
      unitMetadata(unit, candidate),
    );
    const direct = unit.members.filter((member) =>
      request.files.includes(member),
    );
    const metadataTriggered = [...triggers].some(
      (candidate) =>
        candidate === ".code-polishy.json" ||
        candidate === unit.manifest ||
        candidate === unit.configuration ||
        context.includes(candidate),
    );
    const selected = request.selection?.complete
      ? unit.members
      : direct.length
        ? direct
        : metadataTriggered
          ? unit.members
          : [];
    return {
      id: unit.id,
      language: unit.language,
      root: unit.root,
      members: unit.members.toSorted(),
      entryFiles: [...new Set(unit.entryFiles)].toSorted(),
      context: context.toSorted(),
      selected: selected.toSorted(),
      data: {
        packageRoot: unit.packageRoot,
        manifest: unit.manifest,
        workspaceRoot: unit.workspaceRoot,
        configuration: unit.configuration,
      },
    };
  });
  if (!scopes.length)
    throw new Error("static discovery found no provider-owned source scopes");
  return {
    protocolVersion: 4,
    status: "pass",
    evidence: ["static JavaScript and TypeScript scope discovery completed"],
    discovery: { scopes },
    inputs: [],
  };
}

function unitFor(entry, packages, configurations) {
  const effective = entry.context || entry.path;
  const manifest = nearest(packages, effective);
  const configuration = nearestConfiguration(configurations, effective);
  const packageRoot = dirname(manifest || ".");
  const root = configuration ? dirname(configuration) : packageRoot;
  let workspaceRoot = packageRoot;
  for (let directory = packageRoot; directory !== ".";) {
    directory = dirname(directory);
    if (packages.has(directory)) workspaceRoot = directory;
  }
  return {
    id: `${entry.language}:${manifest}:${configuration}`,
    language: entry.language,
    root,
    packageRoot,
    manifest,
    workspaceRoot,
    configuration,
    members: [],
    entryFiles: [],
  };
}

function nearest(values, file) {
  for (let directory = dirname(file); ; directory = dirname(directory)) {
    if (values.has(directory)) return values.get(directory);
    if (directory === ".") return "";
  }
}

function nearestConfiguration(values, file) {
  for (let directory = dirname(file); ; directory = dirname(directory)) {
    const candidates = values.get(directory) ?? [];
    for (const preferred of ["tsconfig.json", "jsconfig.json"]) {
      const candidate = join(directory, preferred).replaceAll("\\", "/");
      if (candidates.includes(candidate)) return candidate;
    }
    if (candidates.length === 1) return candidates[0];
    if (directory === ".") return "";
  }
}

function javaScriptConfiguration(path) {
  const name = basename(path);
  return name === "jsconfig.json" || /^tsconfig.*\.json$/.test(name);
}

function unitMetadata(unit, candidate) {
  const name = basename(candidate);
  const metadata =
    name === "package.json" ||
    name === "pnpm-workspace.yaml" ||
    javaScriptConfiguration(candidate) ||
    /^astro\.config\./.test(name);
  return (
    metadata &&
    (within(unit.root, dirname(candidate)) ||
      within(unit.packageRoot, dirname(candidate)))
  );
}

function within(root, candidate) {
  return root === "." || candidate === root || candidate.startsWith(`${root}/`);
}

function entryPoint(root, file, test) {
  if (test) return true;
  const directory = dirname(file);
  const name = basename(file).replace(/\.[^.]+$/, "");
  const conventional = ["cli", "index", "main"].includes(name);
  return (
    (directory === root && (conventional || name.endsWith(".config"))) ||
    (directory === join(root, "src").replaceAll("\\", "/") && conventional)
  );
}
