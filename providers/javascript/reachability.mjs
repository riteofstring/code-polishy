import { join, relative, sep } from "node:path";

import { compileAstro } from "./compilers.mjs";
import { projectConfiguration } from "./imports.mjs";

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
  const prototype = PrincipalFactory.prototype;
  const original = prototype.createPrincipal;
  const configurations = new Map();
  for (const path of files) {
    const unit = analysis.unit(path);
    const configuration = projectConfiguration(analysis, path);
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
  return () => {
    prototype.createPrincipal = original;
  };
}
