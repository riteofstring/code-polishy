import { packageFor } from "../context.mjs";
import { basename, dirname, join } from "node:path";
import { existsSync } from "node:fs";

import ts from "typescript";
import astroParser from "astro-eslint-parser";
import tsParser from "@typescript-eslint/parser";
import * as prettierPlugin from "prettier-plugin-astro";

import { cssComments } from "../css.mjs";
import { parseJavaScript, walk } from "../ast.mjs";

export const astro = {
  matches: (path) => path.endsWith(".astro"),
  formatter: { parser: "astro", plugins: [prettierPlugin] },
  parse(source, path) {
    const parsed = astroParser.parseForESLint(source, {
      filePath: path,
      parser: tsParser,
      loc: true,
      range: true,
      tokens: true,
      comment: true,
      ecmaVersion: "latest",
      sourceType: "module",
    });
    const scripts = [];
    const comments = [];
    const references = [];
    walk(parsed.ast, parsed.visitorKeys, (node) => {
      if (elementNamed(node, "style")) {
        comments.push(...styleComments(node, source));
        return;
      }
      if (!elementNamed(node, "script")) return;
      if (!executableScript(node)) return;
      const reference = scriptReference(node);
      if (reference) {
        references.push(reference);
        return false;
      }
      for (const child of node.children) {
        if (child.type !== "AstroRawText" && child.type !== "JSXText")
          throw new Error("embedded script source has an unsupported mapping");
        const start = child.range[0];
        scripts.push({
          parsed: parseJavaScript(
            source.slice(start, child.range[1]),
            `${path}.ts`,
          ),
          offset: start,
        });
      }
    });
    return { parsed, scripts, comments, references };
  },
};

function scriptReference(node) {
  const attributes = node.openingElement.attributes;
  const source = attributes.find((attribute) => attribute.name?.name === "src");
  if (attributes.some((attribute) => attribute.type === "JSXSpreadAttribute"))
    return {
      problem: "spread script attributes cannot establish source dependencies",
    };
  if (!source) return null;
  const external = externalScriptSource(source.value);
  if (external) return { external, offset: source.value.range[0] };
  if (typeof source.value?.value !== "string" || !source.value.value)
    return { problem: "script src must be a nonempty static string" };
  if (attributes.length !== 1 || !source.value.value.startsWith("."))
    return {
      problem:
        "unprocessed or external script src has no verified module target",
    };
  return {
    specifier: source.value.value,
    kind: "runtime",
    offset: source.value.range[0],
  };
}

function externalScriptSource(attribute) {
  const value =
    attribute?.type === "JSXExpressionContainer"
      ? attribute.expression
      : attribute;
  if (typeof value?.value === "string")
    return externalURL(value.value) ? value.value : "";
  if (value?.type !== "TemplateLiteral") return "";
  return externalTemplateSource(value);
}

function externalTemplateSource(value) {
  const prefix = value.quasis[0]?.value.cooked;
  if (!prefix) return "";
  if (!value.expressions.length) return externalURL(prefix) ? prefix : "";
  if (!/^(?:https?:)?\/\/[^/?#\s\\]+[/?#]/i.test(prefix)) return "";
  return externalURL(prefix) ? prefix : "";
}

function externalURL(value) {
  if (!/^(?:https?:)?\/\//i.test(value) || /[\s\\]/.test(value)) return false;
  try {
    const url = new URL(value.startsWith("//") ? `https:${value}` : value);
    return ["http:", "https:"].includes(url.protocol) && Boolean(url.hostname);
  } catch {
    return false;
  }
}

export function astroInstallation(analysis, path) {
  const owner = packageFor(analysis, path);
  const declared = astroDeclaration(owner);
  if (!declared)
    throw new Error("framework source has no Astro dependency declaration");
  let root = owner.root;
  for (;;) {
    const manifest = join(root, "node_modules/astro/package.json").replaceAll(
      "\\",
      "/",
    );
    if (existsSync(join(analysis.root, manifest))) {
      const data = JSON.parse(analysis.read(manifest));
      if (data.version !== declared)
        throw new Error(
          "installed Astro does not match the exact project dependency",
        );
      return {
        directory: join(analysis.root, dirname(manifest)),
        version: data.version,
        root: owner.root,
      };
    }
    if (root === ".") throw new Error("project Astro types are not installed");
    root = dirname(root);
  }
}

function executableScript(node) {
  const type = node.openingElement.attributes.find(
    (attribute) => attribute.name?.name === "type",
  );
  if (
    type?.value?.value &&
    !["module", "text/javascript", "application/javascript"].includes(
      type.value.value,
    )
  )
    return false;
  if (type?.value && typeof type.value.value !== "string")
    throw new Error("script type cannot be established statically");
  return true;
}

export function frameworkEntryPoint(analysis, owner, name) {
  if (owner.data.dependencies?.astro || owner.data.devDependencies?.astro) {
    const sourceRoot = frameworkSourceRoot(analysis, owner.root);
    if (name.startsWith(`${sourceRoot}/pages/`)) return true;
    if (
      [
        `${sourceRoot}/middleware.ts`,
        `${sourceRoot}/middleware.js`,
        `${sourceRoot}/actions/index.ts`,
        `${sourceRoot}/content.config.ts`,
        "src/content/config.ts",
      ].includes(name)
    )
      return true;
  }
  return false;
}

function frameworkSourceRoot(analysis, root) {
  const configPath = [
    ...new Set([...analysis.context.keys(), ...analysis.inputs.keys()]),
  ].find(
    (path) =>
      dirname(path) === root &&
      /^astro\.config\.[cm]?[jt]s$/.test(basename(path)),
  );
  if (!configPath) return "src";
  const source = ts.createSourceFile(
    configPath,
    analysis.read(configPath),
    ts.ScriptTarget.Latest,
    true,
  );
  let result = "src";
  const visit = (node) => {
    if (
      ts.isPropertyAssignment(node) &&
      node.name.getText(source) === "srcDir"
    ) {
      if (!ts.isStringLiteral(node.initializer))
        throw new Error("framework srcDir is not a static string");
      result = node.initializer.text.replace(/^\.\//, "").replace(/\/$/, "");
      if (result.startsWith("/") || result.split("/").includes(".."))
        throw new Error("framework srcDir escapes the package");
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return result;
}

export function astroDeclaration(owner) {
  return owner?.data.dependencies?.astro ?? owner?.data.devDependencies?.astro;
}

function styleComments(node, source) {
  const language = node.openingElement.attributes.find(
    (attribute) => attribute.name?.name === "lang",
  );
  if (language && language.value?.value !== "css")
    throw new Error("embedded stylesheet language is not supported");
  const comments = [];
  for (const child of node.children) {
    if (!["AstroRawText", "JSXText"].includes(child.type))
      throw new Error("embedded stylesheet has an unsupported source mapping");
    comments.push(
      ...cssComments(
        source.slice(child.range[0], child.range[1]),
        child.range[0],
      ),
    );
  }
  return comments;
}

function elementNamed(node, name) {
  return node.type === "JSXElement" && node.openingElement.name?.name === name;
}
