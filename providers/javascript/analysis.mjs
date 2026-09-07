import { packageFor } from "./context.mjs";
import { extname } from "node:path";

import { Linter } from "eslint";
import tsParser from "@typescript-eslint/parser";
import astroParser from "astro-eslint-parser";
import astroPlugin from "eslint-plugin-astro";
import reactHooks from "eslint-plugin-react-hooks";
import jsxAccessibility from "eslint-plugin-jsx-a11y";
import prettier from "prettier";

import { FORMAT_OPTIONS, lintRules } from "../../tools/javascript/policy.mjs";
import { adapterFor } from "./frameworks.mjs";
import { functionMetrics } from "./metrics.mjs";
import { sourceFacts } from "./ast.mjs";
import { projectConfiguration, resolveImport } from "./imports.mjs";

export async function analyzeSource(analysis) {
  const capability = analysis.request.capability;
  const facts = { comments: [], functions: [], imports: [] };
  for (const path of analysis.request.files) {
    try {
      const adapter = adapterFor(analysis, path);
      if (capability === "format") await format(analysis, path, adapter);
      else analyzeFile(analysis, path, adapter, facts);
      analysis.response.coverage.analyzed.push(path);
    } catch (error) {
      analysis.unsupported(path, error.message);
    }
  }
  const field = {
    lint: "comments",
    complexity: "functions",
    architecture: "imports",
  }[capability];
  if (field) analysis.response.facts = { [field]: facts[field] };
}

async function format(analysis, path, adapter) {
  const source = analysis.read(path);
  const formatted = await prettier.format(source, {
    ...FORMAT_OPTIONS,
    filepath: path,
    ...adapter.formatter,
  });
  if (formatted === source) return;
  if (analysis.request.mode === "write")
    (analysis.response.edits ??= []).push({ path, content: formatted });
  else
    analysis.diagnostic(
      path,
      "format",
      "Source is not formatted according to the effective policy",
    );
}

function analyzeFile(analysis, path, adapter, facts) {
  const source = analysis.read(path);
  const { parsed, scripts } = adapter.parse(source, path);
  const collected = sourceFacts(analysis, path, parsed);
  collectMetricsAndScripts(analysis, path, { parsed, scripts, collected });
  const capability = analysis.request.capability;
  if (capability === "lint") {
    lint(analysis, path, source, 0, extname(path));
    for (const script of scripts) {
      const end = script.parsed.ast.range[1];
      lint(
        analysis,
        path,
        source.slice(script.offset, script.offset + end),
        script.offset,
        ".ts",
      );
    }
  } else if (capability === "architecture") {
    const configuration = projectConfiguration(analysis, path);
    collected.imports = collected.imports.flatMap((fact) =>
      resolveImport(analysis, fact, configuration),
    );
  } else if (capability === "complexity") {
    metricDiagnostics(analysis, path, collected.functions);
  } else throw new Error(`unsupported operation: ${capability}`);
  for (const key of Object.keys(facts)) facts[key].push(...collected[key]);
}

function lint(analysis, path, source, offset, extension) {
  const owner = packageFor(analysis, path);
  const dependencies = {
    ...owner?.data.dependencies,
    ...owner?.data.devDependencies,
  };
  const generated = analysis.classifications.get(path)?.generated;
  const activation = {
    reactHooks: Boolean(dependencies.react) && !generated,
    jsxAccessibility: Boolean(dependencies.react) || extension === ".astro",
  };
  const rules = lintRules({
    limits: { complexity: 1000, depth: 1000, parameters: 1000 },
    activation,
  });
  for (const key of ["complexity", "max-depth", "max-params"])
    delete rules[key];
  if (extension === ".astro") configureAstroRules(rules);
  const messages = lintMessages(source, extension, rules);
  for (const message of messages)
    reportLintMessage(analysis, path, source, offset, message);
}

function configureAstroRules(rules) {
  for (const key of Object.keys(rules))
    if (key.startsWith("jsx-a11y/")) {
      rules[`astro/${key}`] = rules[key];
      delete rules[key];
    }
  rules["astro/no-conflict-set-directives"] = "error";
}

function lintMessages(source, extension, rules) {
  const config = {
    files: ["**/*.*"],
    plugins: {
      "react-hooks": reactHooks,
      "jsx-a11y": jsxAccessibility,
      astro: astroPlugin,
    },
    linterOptions: {
      noInlineConfig: true,
      reportUnusedDisableDirectives: "off",
    },
    languageOptions: {
      parser: extension === ".astro" ? astroParser : tsParser,
      parserOptions: { parser: tsParser, ecmaFeatures: { jsx: true } },
      sourceType: extension === ".cjs" ? "commonjs" : "module",
    },
    rules,
  };
  return new Linter({ configType: "flat" }).verify(
    source,
    config,
    `source${extension}`,
  );
}

function reportLintMessage(analysis, path, source, offset, message) {
  if (message.fatal || !message.ruleId)
    throw new Error(`source cannot be fully linted: ${message.message}`);
  const lines = source.split("\n");
  const prefix = lines.slice(0, message.line - 1).join("\n");
  const position =
    offset + prefix.length + (message.line > 1 ? 1 : 0) + message.column - 1;
  analysis.diagnostic(path, message.ruleId, message.message, position);
}

function metricDiagnostics(analysis, path, functions) {
  const quality = analysis.request.policy.quality;
  const test = analysis.classifications.get(path)?.test;
  const maximum = test
    ? quality.complexity.typescriptTest
    : quality.complexity.typescript;
  for (const fact of functions) {
    if (fact.complexity < maximum) continue;
    analysis.response.findings.push({
      capability: "complexity",
      path,
      line: fact.line,
      column: fact.column,
      rule: "function-complexity",
      subject: fact.name,
      message: `Function complexity ${fact.complexity} must be below ${maximum}`,
    });
  }
}

function collectMetricsAndScripts(
  analysis,
  path,
  { parsed, scripts, collected },
) {
  if (analysis.request.capability === "complexity")
    collected.functions = functionMetrics(analysis, path, parsed);
  for (const script of scripts) {
    const nested = sourceFacts(analysis, path, script.parsed, script.offset);
    if (analysis.request.capability === "complexity")
      nested.functions = functionMetrics(
        analysis,
        path,
        script.parsed,
        script.offset,
      );
    for (const key of Object.keys(collected))
      collected[key].push(...nested[key]);
  }
}
