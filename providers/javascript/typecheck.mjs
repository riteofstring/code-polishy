import { packageFor } from "./context.mjs";
import { compilerFor } from "./typescript-context.mjs";
import { join, relative } from "node:path";

import ts from "typescript";
import { createTypeScriptInferredChecker } from "@volar/kit";
import { URI } from "vscode-uri";
import {
  getAstroLanguagePlugin,
  addAstroTypes,
} from "@astrojs/language-server/dist/core/index.js";
import { create as typeScriptServices } from "@astrojs/language-server/dist/plugins/typescript/index.js";
import { create as astroServices } from "@astrojs/language-server/dist/plugins/astro.js";

import { TYPECHECK_OPTIONS } from "../../tools/javascript/policy.mjs";
import { adapterFor } from "./frameworks.mjs";
import { astroDeclaration, astroInstallation } from "./frameworks/astro.mjs";
import { projectConfiguration } from "./imports.mjs";

export async function typecheck(analysis) {
  const projects = new Map();
  for (const path of analysis.request.files) {
    try {
      adapterFor(analysis, path);
      const configuration = projectConfiguration(analysis, path);
      const owner = packageFor(analysis, path);
      const key = configuration?.path ?? owner?.root ?? ".";
      if (!projects.has(key))
        projects.set(key, { configuration, paths: [], owner });
      projects.get(key).paths.push(path);
    } catch (error) {
      analysis.unsupported(path, error.message);
    }
  }
  for (const project of projects.values())
    await checkProject(analysis, project);
}

async function checkProject(analysis, project) {
  try {
    const { checker, programDiagnostics, compilationFiles } = checkerFor(
      analysis,
      project,
    );
    await checker.check(join(analysis.root, project.paths[0]));
    const compiled = compilationFiles();
    validateCompilationOwnership(analysis, compiled);
    const included = new Set([...checker.getRootFileNames(), ...compiled]);
    const paths = new Set(project.paths);
    for (const absolute of included) {
      const path = relative(analysis.root, absolute).replaceAll("\\", "/");
      if (analysis.reportable.has(path) && analysis.owns(path)) paths.add(path);
    }
    for (const path of paths) {
      try {
        adapterFor(analysis, path);
        await checkFile(analysis, path, {
          checker,
          programDiagnostics,
          included,
        });
      } catch (error) {
        analysis.unsupported(path, error.message);
      }
    }
  } catch (error) {
    for (const path of project.paths) analysis.unsupported(path, error.message);
  }
}

function checkerFor(analysis, project) {
  const declaration = astroDeclaration(project.owner);
  const installation = declaration
    ? astroInstallation(analysis, project.paths[0])
    : null;
  const plugins = installation ? [getAstroLanguagePlugin()] : [];
  const services = typeScriptServices(compilerFor(analysis));
  let serviceContext;
  services.push({
    name: "provider-program-coverage",
    capabilities: {},
    create(context) {
      serviceContext = context;
      return {};
    },
  });
  if (installation) services.push(astroServices());
  if (installation) {
    for (const name of ["env.d.ts", "astro-jsx.d.ts"])
      analysis.read(
        join(installation.directory, name)
          .slice(analysis.root.length + 1)
          .replaceAll("\\", "/"),
      );
  }
  const { options, files } = projectInputs(analysis, project);
  analysis.note(compilationUnitNote(project, options, files.length));

  const checker = createTypeScriptInferredChecker(
    plugins,
    services,
    () => files,
    options,
    ({ project: checkerProject }) => {
      const host = checkerProject.typescript.languageServiceHost;
      bindGeneratedResolution(analysis, host);
      host.getCurrentDirectory = () =>
        join(
          analysis.root,
          project.configuration?.root ?? project.owner?.root ?? ".",
        );
      if (installation) {
        const [major, minor, patch] = installation.version
          .split(".")
          .map(Number);
        addAstroTypes(
          {
            directory: installation.directory,
            version: { major, minor, patch },
          },
          ts,
          host,
        );
      }
      const original = host.getCompilationSettings.bind(host);
      host.getCompilationSettings = () => ({
        ...original(),
        ...TYPECHECK_OPTIONS,
      });
    },
  );
  return {
    checker,
    compilationFiles() {
      return serviceContext
        .inject("typescript/languageService")
        .getProgram()
        .getSourceFiles()
        .map((file) => file.fileName);
    },
    programDiagnostics() {
      const program = serviceContext
        ?.inject("typescript/languageService")
        ?.getProgram();
      if (!program)
        throw new Error("type checker produced no compilation program");
      return [
        ...program.getOptionsDiagnostics(),
        ...program.getGlobalDiagnostics(),
      ];
    },
  };
}

async function checkFile(
  analysis,
  path,
  { checker, programDiagnostics, included },
) {
  const absolute = join(analysis.root, path);
  if (!included.has(absolute)) {
    analysis.unsupported(
      path,
      "the effective project configuration excludes this selected source",
    );
    return;
  }
  const diagnostics = await checker.check(absolute);
  const programProblems = programDiagnostics();
  if (programProblems.length)
    throw new Error(
      `project type checking is incomplete: ${ts.flattenDiagnosticMessageText(programProblems[0].messageText, " ")}`,
    );
  const script = checker.language.scripts.get(URI.file(absolute));
  if (!script?.snapshot || (path.endsWith(".astro") && !script.generated)) {
    analysis.unsupported(
      path,
      "the type checker produced no original-source analysis",
    );
    return;
  }
  for (const diagnostic of diagnostics)
    reportDiagnostic(analysis, path, diagnostic);
  analysis.analyzed(path);
}

function reportDiagnostic(analysis, path, diagnostic) {
  if (diagnostic.severity !== 1) return;
  const position = diagnostic.range?.start;
  if (
    !position ||
    (typeof diagnostic.code !== "string" && typeof diagnostic.code !== "number")
  )
    throw new Error("type diagnostic has no stable rule or source mapping");
  const source = analysis.read(path);
  const lines = source.split("\n");
  const prefix = lines.slice(0, position.line).join("\n");
  analysis.diagnostic(
    path,
    `type-${diagnostic.code}`,
    diagnostic.message,
    prefix.length + (position.line ? 1 : 0) + position.character,
  );
}

function compilationUnitNote(project, options, count) {
  return `Compilation unit ${project.configuration?.path ?? `${project.owner?.root ?? "."} (inferred)`}: ${count} root files; provider-required allowJs=true, checkJs=true, noEmit=true, noCheck=false; effective strict=${Boolean(options.strict)}, skipLibCheck=${Boolean(options.skipLibCheck)}`;
}

function projectInputs(analysis, project) {
  const parsed = project.configuration?.parsed;
  if (parsed?.projectReferences?.length)
    throw new Error(
      "project-reference type checking needs a provider that owns the referenced compilation units",
    );
  const options = {
    ...(parsed?.options ?? {
      module: ts.ModuleKind.ESNext,
      moduleResolution: ts.ModuleResolutionKind.Bundler,
      target: ts.ScriptTarget.ESNext,
      jsx: ts.JsxEmit.Preserve,
      strict: true,
      allowJs: true,
    }),
    ...TYPECHECK_OPTIONS,
    allowJs: true,
    checkJs: true,
  };
  const unit = analysis.unit(project.paths[0]);
  const inherited = [...analysis.classifications.values()]
    .filter(
      (file) =>
        file.sourcePackage && file.unit === unit.id && analysis.owns(file.path),
    )
    .map((file) => join(analysis.root, file.path));
  const files = [
    ...new Set([
      ...(parsed?.fileNames ??
        unit.members.map((path) => join(analysis.root, path))),
      ...inherited,
    ]),
  ];
  return { options, files };
}

function bindGeneratedResolution(analysis, host) {
  const original = host.resolveModuleNameLiterals?.bind(host);
  host.resolveModuleNameLiterals = (literals, containingFile, ...rest) => {
    const path = relative(analysis.root, containingFile).replaceAll("\\", "/");
    const owner = analysis.classifications.get(path)?.sourcePackage;
    return literals.map((literal) => {
      if (original && (!owner || literal.text.startsWith(".")))
        return original([literal], containingFile, ...rest)[0];
      const source =
        owner && !literal.text.startsWith(".")
          ? join(analysis.root, owner)
          : containingFile;
      return ts.resolveModuleName(
        literal.text,
        source,
        host.getCompilationSettings(),
        ts.sys,
        undefined,
        rest[0],
        ts.getModeForUsageLocation(
          rest[2],
          literal,
          host.getCompilationSettings(),
        ),
      );
    });
  };
}

function validateCompilationOwnership(analysis, files) {
  for (const absolute of files) {
    const path = relative(analysis.root, absolute).replaceAll("\\", "/");
    const source = analysis.classifications.get(path);
    if (source?.language === "typescript" && !analysis.owns(path))
      throw new Error(
        `compilation member belongs to another analyzer: ${path}`,
      );
  }
}
