// The one fixed entry point of the sealed, policy-owned JavaScript tool bundle.
//
// Code Polishy launches this exact file with the pinned Node runtime, passes one
// bounded JSON request either on stdin or as an exact --request-json argument,
// and reads one JSON response from stdout. Go decides policy; this process only
// reports facts. The protocol it answers under, and
// the reading it is allowed to do, live in protocol.mjs; the dead-code
// operation lives in deadcode.mjs, the import facts in imports.mjs, the pnpm
// workspace and lock facts in packages.mjs, the installed license metadata in
// licenses.mjs, and the native audit in audit.mjs.
//
// Nothing here is resolved from the invoking environment, the working
// directory, a target's node_modules, a user cache, or a global installation:
// every path is derived from this file's own installed location, and a runtime,
// launch option, or tool version other than the installed one is refused rather
// than tolerated.

import { writeFileSync } from "node:fs";
import { basename, dirname, extname, join } from "node:path";
// Resolved relative to this file rather than as a bare specifier, so the
// analyzer can only ever be the one installed beside the runner: Node never
// walks out of the bundle looking for it.
import prettier from "./node_modules/prettier/index.mjs";
import eslint from "./node_modules/eslint/lib/api.js";
import typescript from "./node_modules/typescript/lib/typescript.js";
import typescriptParser from "./node_modules/@typescript-eslint/parser/dist/index.js";
import reactHooks from "./node_modules/eslint-plugin-react-hooks/index.js";
import jsxAccessibility from "./node_modules/eslint-plugin-jsx-a11y/lib/index.js";

import { audit } from "./audit.mjs";
import { deadcode, requireWorkspaces } from "./deadcode.mjs";
import { imports } from "./imports.mjs";
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

// The closed operation enum: the exact request fields each operation admits,
// and the one function that answers it. Provenance is the operation the sealed
// bundle answers about itself; the others read the selected target files named
// in the request. Audit is the one operation that contacts a registry, and it
// is reached only when Go asks for it by name. Writing formatted files is its
// own operation rather than a flag, so only a caller that asked for it can get
// it.
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
  lint: { fields: ["root", "paths", "limits", "activation"], run: lint },
  typecheck: { fields: ["root", "paths", "project"], run: typecheck },
  deadcode: { fields: ["root", "directory", "workspaces"], run: deadcode },
  imports: { fields: ["root", "paths"], run: imports },
  packages: { fields: ["root", "directory"], run: packages },
  workspace: { fields: ["root", "paths"], run: workspace },
  licenses: { fields: ["root", "directory"], run: licenses },
  audit: { fields: ["root", "directory"], run: audit },
};
const BASE_REQUEST_FIELDS = ["protocolVersion", "operation"];
// One lint exchange reports bounded results. A selection that produces more
// than this has a systemic problem Go should be told about as a failed
// operation rather than handed a truncated answer that looks complete.
const MAXIMUM_LINT_RESULTS = 5000;
// One typecheck exchange reports bounded results for the same reason. A
// project that produces more than this is broken rather than merely failing.
const MAXIMUM_TYPECHECK_DIAGNOSTICS = 5000;
// How many files one project may pull into a program. A project larger than
// this is reported rather than checked, so no exchange grows past a bound.
const MAXIMUM_PROJECT_FILES = 20000;

// The sealed central formatting configuration. Code Polishy owns every one of
// these: prettier.format resolves no configuration of its own, and a target
// .prettierrc, prettier.config.*, .editorconfig, or .prettierignore is never
// read. Only the parser is derived from the file, and only from its name.
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

// The framework rules Code Polishy activates. Go decides whether a selection
// gets them; the runner decides nothing about React beyond running exactly
// these rules when it is told to.
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

// How each governed source file is parsed, by its name alone. A file type that
// is not here is reported rather than parsed under a guessed language.
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

// An inline configuration comment claims to decide what a policy rule does.
// The sealed configuration ignores every one of them, so each is reported
// instead: a Code Polishy exception is the only way to except anything.
const INLINE_DIRECTIVE = /\beslint-(?:disable|enable)\b/;

// The compiler settings Code Polishy decides whatever a project declares. A
// project owns its own language, library, and strictness facts, because those
// describe that codebase; it does not own whether a policy run emits, and it
// does not own whether it is checked at all. noCheck asks the compiler to parse
// a project and report nothing about its types, which would make an unchecked
// project read as a covered one, so it is decided here rather than declared.
// This run reports diagnostics and leaves nothing behind in the target tree.
const TYPECHECK_OPTIONS = {
  noEmit: true,
  noCheck: false,
  emitDeclarationOnly: false,
  composite: false,
  incremental: false,
  tsBuildInfoFile: undefined,
};

// Report every selected file the sealed configuration formats differently, and
// rewrite them when Go asked for the write operation. Go decides what a changed
// file means; this reports only which files the formatter changes.
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
      // filepath names the file only so Prettier can infer its parser; it is
      // the repository-relative path, so no absolute path reaches a result.
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

// The one configuration a lint operation runs under. Every rule in it was
// decided by Go: the budgets arrive as exact allowed maximums, and a framework
// rule set is present only when the request activates it. Nothing is read from
// the target, and an inline comment cannot add, weaken, or silence a rule.
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

function lintConfiguration(rules, language, extension) {
  return {
    // The configuration claims the file being linted by its own extension.
    // ESLint's default configuration covers only .js, .mjs, and .cjs, and a
    // pattern that matches everything is universal rather than claiming: a
    // governed TypeScript file would be reported as having no configuration.
    files: [`**/*${extension}`],
    plugins: { "react-hooks": reactHooks, "jsx-a11y": jsxAccessibility },
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
    rules,
  };
}

function inlineDirectives(path, source) {
  const found = [];
  const lines = source.split("\n");
  for (let index = 0; index < lines.length; index += 1) {
    if (INLINE_DIRECTIVE.test(lines[index])) {
      found.push({ path, line: index + 1 });
    }
  }
  return found;
}

// Sort one file's messages into what a rule decided and what it could not. A
// message no rule produced is a parse or configuration failure, so the file was
// never linted at all: that is missing coverage, not one complaint about it.
function collectLintMessages(path, messages, findings, unsupportedPaths) {
  for (const message of messages) {
    if (message.fatal || !message.ruleId) {
      unsupportedPaths.push(
        unsupported(path, `line ${message.line}: ${message.message}`),
      );
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
}

// Report every rule violation in the selection, every inline directive the
// sealed configuration refused to honor, and every file it could not decide.
// Go owns which of those is a failure and how severe it is.
function lint(request) {
  const linter = new eslint.Linter({ configType: "flat" });
  const rules = lintRules(request);
  const findings = [];
  const directives = [];
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
    directives.push(...inlineDirectives(path, source));
    let messages;
    try {
      messages = linter.verify(
        source,
        lintConfiguration(rules, language, extension),
        path,
      );
    } catch (error) {
      unsupportedPaths.push(unsupported(path, error.message));
      continue;
    }
    collectLintMessages(path, messages, findings, unsupportedPaths);
    if (findings.length + directives.length > MAXIMUM_LINT_RESULTS) {
      fail(
        `the lint operation produced more than the ${MAXIMUM_LINT_RESULTS} result limit`,
      );
    }
  }
  return { findings, directives, unsupported: unsupportedPaths };
}

// One bounded line of a TypeScript message. The compiler names files
// absolutely inside its own prose, so only the target-relative remainder is
// reported and a finding never carries a host path.
function diagnosticText(root, diagnostic) {
  const message = typescript.flattenDiagnosticMessageText(
    diagnostic.messageText,
    " ",
  );
  return truncate(message.replaceAll(`${root}/`, "").replaceAll(root, "."));
}

// The configuration host one project is parsed through. It reads only what is
// really inside the target tree, so an extension chain that leaves the
// repository — by naming a path above it or by pointing at a link that lands
// outside it — is refused rather than followed. A refused read is recorded only
// when the file exists, so a project extending something absent is reported as
// the missing file it is rather than as an escape.
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

// The host one project's program is built on. The compiler resolves imports,
// type roots, and links itself, and each of those is a path it would otherwise
// read wherever it landed: a relative specifier climbing out of the repository,
// a node_modules entry linked to another checkout, a type root above the target
// root. Every read is answered only for what is really inside the target tree
// or inside the bundle's own library declarations, so a program contains the
// target and the policy-owned TypeScript library and nothing else.
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
  // A policy run reports and writes nothing. Building a program emits nothing
  // on its own; this makes that true of the host it was built with as well.
  host.writeFile = () => {};
  return host;
}

// Everything about one project this operation refuses to analyze. A compiler
// plug-in is target code the compiler would load, a project reference is a
// build graph this operation does not resolve, and an extension chain leaving
// the repository is not contained configuration data. Each is reported so
// coverage fails closed rather than being read under a guess.
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

// Parse one contained project into exactly the files and settings it governs.
// A project is JSON/JSONC data read from inside the target tree; nothing here
// executes, and a project that cannot be read this way is reported instead.
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
  // A project's own configuration errors matter only when it was otherwise
  // admissible. Reporting them alongside a refusal would report the refusal's
  // consequences as if they were separate problems.
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

// One diagnostic as a bounded fact: a target-relative path, a position, the
// exact TypeScript code, and one line of message. A diagnostic with no file, or
// one in a bundled library declaration the target does not own, is attributed
// to the project that produced it rather than to a path outside the target.
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

// Which of the selected files the project's program actually contains. A
// selected file the program never saw was never checked, so reporting it as
// uncovered is how a project that governs less than Go asked about fails
// closed rather than reading as clean.
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

// Report every type and syntax diagnostic of one contained project, and which
// of the selected files that project covers. Go owns which diagnostic fails a
// check and what missing coverage means.
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

// The rule budgets are exact ESLint allowed maximums Go already decided. The
// runner neither defaults nor translates one: an absent or unusable budget is
// a protocol disagreement, not something to fill in.
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

// Framework rule activation is a closed pair of decisions, so a request can
// never name a rule, a plug-in, or a configuration of its own.
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

// The requested operation, from a request that is a JSON object declaring the
// exact protocol version. Nothing else about the request is trusted yet.
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
  // Exactly the fields this operation admits: an extra one is a protocol
  // disagreement, and a missing one is never filled in from anywhere.
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
