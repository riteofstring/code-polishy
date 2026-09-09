import { join, relative, sep } from "node:path";

import { compileAstro } from "./compilers.mjs";
import { projectConfiguration } from "./imports.mjs";
import { resolutionPath } from "./context.mjs";

export async function bindKnip(analysis, root, files, configuration) {
  const { ConfigurationChief } =
    await import("knip/dist/ConfigurationChief.js");
  const { partitionCompilers } = await import("knip/dist/compilers/index.js");
  const { knipConfigurationSchema } =
    await import("knip/dist/schema/configuration.js");
  const prototype = ConfigurationChief.prototype;
  const original = {
    init: prototype.init,
    find: prototype.findWorkspaceByFilePath,
  };
  const owners = new Map(
    files.map((path) => [
      join(analysis.root, path),
      relative(root, analysis.unit(path).packageRoot).split(sep).join("/") ||
        ".",
    ]),
  );
  const restoreCompilers = await bindCompilerOptions(analysis, files);
  prototype.init = async function () {
    const manifest = join(root, "package.json").split(sep).join("/");
    this.manifestPath = join(analysis.root, manifest);
    this.manifest = JSON.parse(analysis.read(manifest));
    this.manifest.workspaces = Object.keys(configuration.workspaces).filter(
      (name) => name !== ".",
    );
    this.rawConfig = { ...configuration, compilers: { astro: compileAstro } };
    this.config = this.normalize(
      knipConfigurationSchema.parse(partitionCompilers(this.rawConfig)),
    );
    await this.setWorkspaces();
  };
  prototype.findWorkspaceByFilePath = function (path) {
    const owner = owners.get(path);
    if (owner === undefined) return original.find.call(this, path);
    const workspace = this.workspacesByName.get(owner);
    if (!workspace) throw new Error("resolved source workspace is unavailable");
    return workspace;
  };
  return () => {
    prototype.init = original.init;
    prototype.findWorkspaceByFilePath = original.find;
    restoreCompilers();
  };
}

async function bindCompilerOptions(analysis, files) {
  const { PrincipalFactory } = await import("knip/dist/PrincipalFactory.js");
  const { ProjectPrincipal } = await import("knip/dist/ProjectPrincipal.js");
  const { createCustomModuleResolver } =
    await import("knip/dist/typescript/resolve-module-names.js");
  const prototype = PrincipalFactory.prototype;
  const original = prototype.createPrincipal;
  const initialize = ProjectPrincipal.prototype.init;
  const configurations = new Map();
  const units = new Map();
  for (const path of files) {
    const unit = analysis.unit(path);
    if (!units.has(unit.id))
      units.set(unit.id, projectConfiguration(analysis, path));
    const configuration = units.get(unit.id);
    if (unit.root === unit.packageRoot && configuration)
      configurations.set(
        join(analysis.root, unit.packageRoot),
        configuration.parsed.options,
      );
  }
  prototype.createPrincipal = function (options) {
    const compilerOptions = configurations.get(options.cwd);
    return original.call(
      this,
      compilerOptions
        ? { ...options, isFile: true, compilerOptions: { ...compilerOptions } }
        : options,
    );
  };
  ProjectPrincipal.prototype.init = function () {
    initialize.call(this);
    bindSourceResolution(analysis, this, units, createCustomModuleResolver);
  };
  return () => {
    prototype.createPrincipal = original;
    ProjectPrincipal.prototype.init = initialize;
  };
}

function bindSourceResolution(analysis, principal, units, createResolver) {
  const fallback = principal.backend.resolveModuleNames;
  const resolvers = new Map();
  const resolve = (names, absolute) => {
    const path = relative(analysis.root, absolute).split(sep).join("/");
    const unit = analysis.classifications.get(path)?.unit;
    if (!units.has(unit)) return fallback(names, absolute);
    if (!resolvers.has(unit))
      resolvers.set(
        unit,
        createResolver(
          { ...principal.compilerOptions, ...units.get(unit)?.parsed.options },
          [
            ...principal.syncCompilers.keys(),
            ...principal.asyncCompilers.keys(),
          ],
          principal.toSourceFilePath,
          false,
          principal.isSkipLibs,
        ),
      );
    return names.map(
      (name) =>
        resolvers.get(unit)(
          [name],
          name.startsWith(".")
            ? absolute
            : join(analysis.root, resolutionPath(analysis, path)),
        )[0],
    );
  };
  principal.backend.resolveModuleNames = resolve;
  principal.backend.compilerHost.resolveModuleNames = resolve;
  principal.backend.languageServiceHost.resolveModuleNames = resolve;
}
