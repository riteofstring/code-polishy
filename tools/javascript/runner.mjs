import { writeFileSync } from "node:fs";
import { basename, dirname, extname, join } from "node:path";

import prettier from "./node_modules/prettier/index.mjs";
import eslint from "./node_modules/eslint/lib/api.js";
import typescript from "./node_modules/typescript/lib/typescript.js";
import typescriptParser from "./node_modules/@typescript-eslint/parser/dist/index.js";
import reactHooks from "./node_modules/eslint-plugin-react-hooks/index.js";
import jsxAccessibility from "./node_modules/eslint-plugin-jsx-a11y/lib/index.js";
import yaml from "./node_modules/js-yaml/dist/js-yaml.mjs";

import { audit } from "./audit.mjs";
import { deadcode, requireWorkspaces } from "./deadcode.mjs";
import { imports } from "./imports.mjs";
import { gitlab } from "./gitlab.mjs";
import { licenses } from "./licenses.mjs";
import { packages, workspace } from "./packages.mjs";
import {
  MAXIMUM_REQUEST_BYTES,
  PROTOCOL_VERSION,
  containedProgramRead,
  containedRead,
  fail,
  insideRoot,
  provenance,
  readTargetFile,
  requireContainedPath,
  requireContainedPaths,
  requireContainedRoot,
  requireExactObject,
  requireSealedLaunch,
  respond,
  truncate,
  unsupported,
} from "./protocol.mjs";

const OPERATIONS = {
  provenance: { fields: [], run: provenance },
  format: {
    fields: ["root", "paths"],
    run: (request) => format(request, false),
  },
  "format-write": {
    fields: ["root", "paths"],
    run: (request) => format(request, true),
  },
  parse: { fields: ["root", "paths"], run: parse },
  lint: { fields: ["root", "paths", "limits", "activation"], run: lint },
  typecheck: { fields: ["root", "paths", "project"], run: typecheck },
  deadcode: { fields: ["root", "directory", "workspaces"], run: deadcode },
  imports: { fields: ["root", "paths"], run: imports },
  gitlab: { fields: ["root", "paths", "governedPaths"], run: gitlab },
  packages: { fields: ["root", "directory"], run: packages },
  workspace: { fields: ["root", "paths"], run: workspace },
  licenses: { fields: ["root", "directory"], run: licenses },
  audit: { fields: ["root", "directory"], run: audit },
};
const BASE_REQUEST_FIELDS = ["protocolVersion", "operation"];

const MAXIMUM_LINT_RESULTS = 5000;

const MAXIMUM_TYPECHECK_DIAGNOSTICS = 5000;

const MAXIMUM_PROJECT_FILES = 20000;

const MAXIMUM_GITLAB_GOVERNED_PATHS = 20000;

const FORMAT_OPTIONS = {
  arrowParens: "always",
  bracketSameLine: false,
  bracketSpacing: true,
  embeddedLanguageFormatting: "auto",
  endOfLine: "lf",
  htmlWhitespaceSensitivity: "css",
  jsxSingleQuote: false,
  objectWrap: "preserve",
  printWidth: 80,
  proseWrap: "preserve",
  quoteProps: "as-needed",
  semi: true,
  singleAttributePerLine: false,
  singleQuote: false,
  tabWidth: 2,
  trailingComma: "all",
  useTabs: false,
};

const REACT_HOOKS_RULES = [
  "react-hooks/rules-of-hooks",
  "react-hooks/exhaustive-deps",
];
const JSX_ACCESSIBILITY_RULES = [
  "jsx-a11y/alt-text",
  "jsx-a11y/anchor-has-content",
  "jsx-a11y/anchor-is-valid",
  "jsx-a11y/aria-props",
  "jsx-a11y/aria-role",
  "jsx-a11y/click-events-have-key-events",
  "jsx-a11y/interactive-supports-focus",
  "jsx-a11y/label-has-associated-control",
  "jsx-a11y/no-static-element-interactions",
  "jsx-a11y/tabindex-no-positive",
];

const LINT_LANGUAGES = {
  ".cjs": { sourceType: "commonjs", jsx: false, typescript: false },
  ".js": { sourceType: "module", jsx: true, typescript: false },
  ".jsx": { sourceType: "module", jsx: true, typescript: false },
  ".mjs": { sourceType: "module", jsx: false, typescript: false },
  ".cts": { sourceType: "commonjs", jsx: false, typescript: true },
  ".mts": { sourceType: "module", jsx: false, typescript: true },
  ".ts": { sourceType: "module", jsx: false, typescript: true },
  ".tsx": { sourceType: "module", jsx: true, typescript: true },
};

const TYPECHECK_OPTIONS = {
  noEmit: true,
  noCheck: false,
  emitDeclarationOnly: false,
  composite: false,
  incremental: false,
  tsBuildInfoFile: undefined,
};

async function format(request, write) {
  const changed = [];
  const unsupportedPaths = [];
  for (const path of request.paths) {
    const absolute = join(request.root, path);
    const source = readTargetFile(absolute, path, unsupportedPaths);
    if (source === null) {
      continue;
    }
    let formatted;
    try {
      formatted = await prettier.format(source, {
        ...FORMAT_OPTIONS,
        filepath: path,
      });
    } catch (error) {
      unsupportedPaths.push(unsupported(path, error.message));
      continue;
    }
    if (formatted === source) {
      continue;
    }
    if (write) {
      try {
        writeFileSync(absolute, formatted);
      } catch (error) {
        unsupportedPaths.push(
          unsupported(
            path,
            `the file could not be rewritten: ${error.message}`,
          ),
        );
        continue;
      }
    }
    changed.push(path);
  }
  return { changed, unsupported: unsupportedPaths };
}

function parse(request) {
  const covered = [];
  const unsupportedPaths = [];
  for (const path of request.paths) {
    const source = readTargetFile(
      join(request.root, path),
      path,
      unsupportedPaths,
    );
    if (source === null) {
      continue;
    }
    try {
      switch (extname(path).toLowerCase()) {
        case ".json":
          JSON.parse(source);
          break;
        case ".jsonc":
          parseJsonc(path, source);
          break;
        case ".yaml":
        case ".yml":
          yaml.loadAll(source, undefined, {
            filename: path,
            schema: yaml.JSON_SCHEMA,
          });
          break;
        default:
          throw new Error(
            "the policy-owned data parser does not support this file extension",
          );
      }
      covered.push(path);
    } catch (error) {
      unsupportedPaths.push(unsupported(path, error.message));
    }
  }
  return { covered, unsupported: unsupportedPaths };
}

function parseJsonc(path, source) {
  const document = typescript.parseJsonText(path, source);
  if (document.parseDiagnostics.length === 0) {
    return;
  }
  throw new Error(
    typescript.flattenDiagnosticMessageText(
      document.parseDiagnostics[0].messageText,
      " ",
    ),
  );
}

function lintRules(request) {
  const rules = {
    complexity: ["error", request.limits.complexity],
    "max-depth": ["error", request.limits.depth],
    "max-params": ["error", request.limits.parameters],
  };
  const activated = [
    ...(request.activation.reactHooks ? REACT_HOOKS_RULES : []),
    ...(request.activation.jsxAccessibility ? JSX_ACCESSIBILITY_RULES : []),
  ];
  for (const rule of activated) {
    rules[rule] = "error";
  }
  return rules;
}

function lintConfiguration({
  rules,
  language,
  extension,
  path,
  comments,
  commentLocations,
}) {
  return {
    files: [`**/*${extension}`],
    plugins: {
      facts: sourceCommentFactsPlugin(path, comments, commentLocations),
      "react-hooks": reactHooks,
      "jsx-a11y": jsxAccessibility,
    },
    linterOptions: {
      noInlineConfig: true,
      reportUnusedDisableDirectives: "off",
    },
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: language.sourceType,
      ...(language.typescript ? { parser: typescriptParser } : {}),
      parserOptions: { ecmaFeatures: { jsx: language.jsx } },
    },
    rules: { "facts/comments": "error", ...rules },
  };
}

function sourceCommentFactsPlugin(path, facts, commentLocations) {
  return {
    rules: {
      comments: {
        meta: {
          schema: [],
        },
        create(context) {
          const sourceCode = context.sourceCode;
          return {
            Program() {
              const firstCode = sourceCode.getFirstToken(sourceCode.ast);
              const comments = sourceCode.getAllComments();
              for (const [index, comment] of comments.entries()) {
                const raw = sourceCode.text.slice(
                  comment.range[0],
                  comment.range[1],
                );
                const bounded = boundedCommentBytes(raw);
                facts.push({
                  path,
                  kind: comment.type,
                  raw: bounded.raw,
                  complete: bounded.complete,
                  line: comment.loc.start.line,
                  column: comment.loc.start.column + 1,
                  beforeCode:
                    firstCode === null || comment.range[0] < firstCode.range[0],
                  preamble:
                    index === 0 &&
                    sourceCode.text.slice(0, comment.range[0]).trim() === "",
                  byteZero: comment.range[0] === 0,
                });
                commentLocations.push({
                  line: comment.loc.start.line,
                  column: comment.loc.start.column + 1,
                  endLine: comment.loc.end.line,
                  endColumn: comment.loc.end.column + 1,
                });
              }
              if (
                !comments.some((comment) => comment.type === "Shebang") &&
                sourceCode.text.startsWith("#!")
              ) {
                facts.push(sourcePreambleShebangFact(path, sourceCode.text));
              }
            },
          };
        },
      },
    },
  };
}

function boundedCommentBytes(raw) {
  const bounded = truncate(raw);
  return { raw: bounded, complete: bounded === raw };
}

function sourcePreambleShebangFact(path, source) {
  const end = sourceLineEnd(source);
  const bounded = boundedCommentBytes(source.slice(0, end));
  return {
    path,
    kind: "Shebang",
    raw: bounded.raw,
    complete: bounded.complete,
    line: 1,
    column: 1,
    beforeCode: true,
    preamble: true,
    byteZero: true,
  };
}

function sourceLineEnd(source) {
  const carriageReturn = source.indexOf("\r");
  const lineFeed = source.indexOf("\n");
  if (carriageReturn < 0) {
    return lineFeed < 0 ? source.length : lineFeed;
  }
  return lineFeed < 0 ? carriageReturn : Math.min(carriageReturn, lineFeed);
}

function collectLintMessages(
  path,
  messages,
  findings,
  unsupportedPaths,
  commentLocations,
) {
  const undecidable = messages.find(
    (message) =>
      message.fatal ||
      (!message.ruleId &&
        !commentLocations.some(
          (location) =>
            message.line === location.line &&
            message.column === location.column &&
            message.endLine === location.endLine &&
            message.endColumn === location.endColumn,
        )),
  );
  if (undecidable !== undefined) {
    unsupportedPaths.push(
      unsupported(
        path,
        `line ${undecidable.line ?? 0}: ${undecidable.message}`,
      ),
    );
    return false;
  }
  for (const message of messages) {
    if (!message.ruleId) {
      continue;
    }
    findings.push({
      path,
      line: message.line ?? 0,
      column: message.column ?? 0,
      rule: message.ruleId,
      message: truncate(message.message),
    });
  }
  return true;
}

function lint(request) {
  const linter = new eslint.Linter({ configType: "flat" });
  const rules = lintRules(request);
  const findings = [];
  const comments = [];
  const unsupportedPaths = [];
  for (const path of request.paths) {
    const extension = extname(path).toLowerCase();
    const language = LINT_LANGUAGES[extension];
    if (language === undefined) {
      unsupportedPaths.push(
        unsupported(path, "the policy-owned linter does not analyze this file"),
      );
      continue;
    }
    const source = readTargetFile(
      join(request.root, path),
      path,
      unsupportedPaths,
    );
    if (source === null) {
      continue;
    }
    const fileComments = [];
    const fileCommentLocations = [];
    let messages;
    try {
      messages = linter.verify(
        source,
        lintConfiguration({
          rules,
          language,
          extension,
          path,
          comments: fileComments,
          commentLocations: fileCommentLocations,
        }),
        path,
      );
    } catch (error) {
      unsupportedPaths.push(unsupported(path, error.message));
      continue;
    }
    if (
      !collectLintMessages(
        path,
        messages,
        findings,
        unsupportedPaths,
        fileCommentLocations,
      )
    ) {
      continue;
    }
    comments.push(...fileComments);
    if (findings.length + comments.length > MAXIMUM_LINT_RESULTS) {
      fail(
        `the lint operation produced more than the ${MAXIMUM_LINT_RESULTS} result limit`,
      );
    }
  }
  return { findings, comments, unsupported: unsupportedPaths };
}

function diagnosticText(root, diagnostic) {
  const message = typescript.flattenDiagnosticMessageText(
    diagnostic.messageText,
    " ",
  );
  return truncate(message.replaceAll(`${root}/`, "").replaceAll(root, "."));
}

function containedConfigurationHost(uncontained) {
  return {
    useCaseSensitiveFileNames: true,
    fileExists: (path) =>
      typescript.sys.fileExists(path) && containedRead(path),
    readDirectory: (path, extensions, exclude, include, depth) =>
      containedRead(path)
        ? typescript.sys.readDirectory(
            path,
            extensions,
            exclude,
            include,
            depth,
          )
        : [],
    readFile: (path) => {
      if (containedRead(path)) {
        return typescript.sys.readFile(path);
      }
      if (typescript.sys.fileExists(path)) {
        uncontained.push(path);
      }
      return undefined;
    },
  };
}

function containedProgramHost(options) {
  const host = typescript.createCompilerHost(options);
  const readFile = host.readFile;
  const readDirectory = host.readDirectory;
  host.fileExists = (path) =>
    typescript.sys.fileExists(path) && containedProgramRead(path);
  host.directoryExists = (path) =>
    typescript.sys.directoryExists(path) && containedProgramRead(path);
  host.getDirectories = (path) =>
    containedProgramRead(path) ? typescript.sys.getDirectories(path) : [];
  host.readDirectory = (path, extensions, exclude, include, depth) =>
    containedProgramRead(path)
      ? readDirectory(path, extensions, exclude, include, depth)
      : [];
  host.readFile = (path) =>
    containedProgramRead(path) ? readFile(path) : undefined;

  host.writeFile = () => {};
  return host;
}

function projectRefusals(config, options, uncontained) {
  const reasons = [];
  if (Array.isArray(config.references) && config.references.length > 0) {
    reasons.push(
      "the project declares references, which the policy-owned type checker does not resolve",
    );
  }
  if (options.plugins !== undefined) {
    reasons.push(
      "the project declares compiler plug-ins, which would load target code",
    );
  }
  if (uncontained.length > 0) {
    reasons.push("the project extends configuration outside the repository");
  }
  return reasons;
}

function projectProgramInput(request, unsupportedPaths) {
  const absolute = join(request.root, request.project);
  const text = readTargetFile(absolute, request.project, unsupportedPaths);
  if (text === null) {
    return null;
  }
  const json = typescript.parseConfigFileTextToJson(absolute, text);
  if (json.error !== undefined) {
    unsupportedPaths.push(
      unsupported(request.project, diagnosticText(request.root, json.error)),
    );
    return null;
  }
  const uncontained = [];
  const parsed = typescript.parseJsonConfigFileContent(
    json.config,
    containedConfigurationHost(uncontained),
    dirname(absolute),
    {},
    absolute,
  );
  const refusals = projectRefusals(json.config, parsed.options, uncontained);

  if (refusals.length === 0) {
    for (const error of parsed.errors) {
      refusals.push(diagnosticText(request.root, error));
    }
  }
  if (parsed.fileNames.length > MAXIMUM_PROJECT_FILES) {
    refusals.push(
      `the project includes ${parsed.fileNames.length} files, more than the ${MAXIMUM_PROJECT_FILES} limit`,
    );
  }
  for (const reason of refusals) {
    unsupportedPaths.push(unsupported(request.project, reason));
  }
  return refusals.length > 0
    ? null
    : {
        rootNames: parsed.fileNames,
        options: { ...parsed.options, ...TYPECHECK_OPTIONS },
      };
}

function diagnosticEntry(root, project, diagnostic) {
  const message = diagnosticText(root, diagnostic);
  const file = diagnostic.file;
  const path = file === undefined ? null : insideRoot(root, file.fileName);
  if (path === null) {
    const source = file === undefined ? "" : `${basename(file.fileName)}: `;
    return {
      path: project,
      line: 0,
      column: 0,
      code: diagnostic.code,
      message: truncate(source + message),
    };
  }
  const position =
    diagnostic.start === undefined
      ? null
      : file.getLineAndCharacterOfPosition(diagnostic.start);
  return {
    path,
    line: position === null ? 0 : position.line + 1,
    column: position === null ? 0 : position.character + 1,
    code: diagnostic.code,
    message,
  };
}

function coveredFiles(request, program) {
  const checked = new Set();
  for (const file of program.getSourceFiles()) {
    const path = insideRoot(request.root, file.fileName);
    if (path !== null) {
      checked.add(path);
    }
  }
  return request.paths.filter((path) => checked.has(path));
}

function typecheck(request) {
  const unsupportedPaths = [];
  const input = projectProgramInput(request, unsupportedPaths);
  if (input === null) {
    return { diagnostics: [], covered: [], unsupported: unsupportedPaths };
  }
  const program = typescript.createProgram({
    ...input,
    host: containedProgramHost(input.options),
  });
  const diagnostics = [];
  for (const diagnostic of typescript.getPreEmitDiagnostics(program)) {
    diagnostics.push(
      diagnosticEntry(request.root, request.project, diagnostic),
    );
    if (diagnostics.length > MAXIMUM_TYPECHECK_DIAGNOSTICS) {
      fail(
        `the typecheck operation produced more than the ${MAXIMUM_TYPECHECK_DIAGNOSTICS} result limit`,
      );
    }
  }
  return {
    diagnostics,
    covered: coveredFiles(request, program),
    unsupported: unsupportedPaths,
  };
}

async function readRequestText() {
  const chunks = [];
  let size = 0;
  for await (const chunk of process.stdin) {
    size += chunk.length;
    if (size > MAXIMUM_REQUEST_BYTES) {
      fail(`the request exceeds the ${MAXIMUM_REQUEST_BYTES} byte limit`);
    }
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString("utf8");
}

function argumentRequestText() {
  const arguments_ = process.argv.slice(2);
  if (arguments_.length === 0) {
    return null;
  }
  if (arguments_.length !== 2 || arguments_[0] !== "--request-json") {
    fail("the runner accepts only one --request-json argument");
  }
  if (Buffer.byteLength(arguments_[1], "utf8") > MAXIMUM_REQUEST_BYTES) {
    fail(`the request exceeds the ${MAXIMUM_REQUEST_BYTES} byte limit`);
  }
  return arguments_[1];
}

function requireLimits(limits) {
  requireExactObject(limits, "the lint budget", [
    "complexity",
    "depth",
    "parameters",
  ]);
  for (const [name, value] of Object.entries(limits)) {
    if (!Number.isInteger(value) || value < 1) {
      fail(
        `the lint budget declares '${name}' as ${JSON.stringify(value)}, not a positive whole number`,
      );
    }
  }
}

function requireActivation(activation) {
  requireExactObject(activation, "the lint activation", [
    "reactHooks",
    "jsxAccessibility",
  ]);
  for (const [name, value] of Object.entries(activation)) {
    if (typeof value !== "boolean") {
      fail(
        `the lint activation declares '${name}' as ${JSON.stringify(value)}, not a boolean`,
      );
    }
  }
}

function requireGitLabGovernedPaths(paths) {
  if (!Array.isArray(paths)) {
    fail("the gitlab governed paths are not an array");
  }
  if (paths.length > MAXIMUM_GITLAB_GOVERNED_PATHS) {
    fail(
      `the gitlab request declares ${paths.length} governed paths, more than the ${MAXIMUM_GITLAB_GOVERNED_PATHS} limit`,
    );
  }
  const seen = new Set();
  for (const path of paths) {
    requireContainedPath(path);
    if (seen.has(path)) {
      fail(`the gitlab request repeats governed path ${JSON.stringify(path)}`);
    }
    seen.add(path);
  }
}

function decodeOperation(text) {
  let request;
  try {
    request = JSON.parse(text);
  } catch (error) {
    fail(`the request is not valid JSON: ${error.message}`);
  }
  if (
    request === null ||
    typeof request !== "object" ||
    Array.isArray(request)
  ) {
    fail("the request is not a JSON object");
  }
  for (const field of BASE_REQUEST_FIELDS) {
    if (!(field in request)) {
      fail(`the request is missing field '${field}'`);
    }
  }
  if (request.protocolVersion !== PROTOCOL_VERSION) {
    fail(
      `the request declares protocol version ${JSON.stringify(request.protocolVersion)}, not ${PROTOCOL_VERSION}`,
    );
  }
  if (!Object.hasOwn(OPERATIONS, request.operation)) {
    fail(
      `the request declares unsupported operation ${JSON.stringify(request.operation)}`,
    );
  }
  return request;
}

function decodeRequest(text) {
  const request = decodeOperation(text);

  const admitted = BASE_REQUEST_FIELDS.concat(
    OPERATIONS[request.operation].fields,
  );
  requireExactObject(request, "the request", admitted);
  if (admitted.includes("root")) {
    requireContainedRoot(request.root);
  }
  if (admitted.includes("paths")) {
    requireContainedPaths(request.paths);
  }
  if (admitted.includes("limits")) {
    requireLimits(request.limits);
    requireActivation(request.activation);
  }
  if (admitted.includes("project")) {
    requireContainedPath(request.project);
  }
  if (admitted.includes("directory")) {
    requireContainedPath(request.directory);
  }
  if (admitted.includes("workspaces")) {
    requireWorkspaces(request.directory, request.workspaces);
  }
  if (admitted.includes("governedPaths")) {
    requireGitLabGovernedPaths(request.governedPaths);
  }
  return request;
}

requireSealedLaunch();
const argumentRequest = argumentRequestText();
const request = decodeRequest(argumentRequest ?? (await readRequestText()));
respond({
  protocolVersion: PROTOCOL_VERSION,
  operation: request.operation,
  result: await OPERATIONS[request.operation].run(request),
});
