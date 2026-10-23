import { join } from "node:path";
import {
  isAbsolute as isPosixAbsolute,
  normalize as normalizePosix,
} from "node:path/posix";
import { isAbsolute as isWindowsAbsolute } from "node:path/win32";

import yaml from "js-yaml";

import { fail, readTargetFile, unsupported } from "./protocol.mjs";

const ROOT_FILES = new Set([".gitlab-ci.yml", ".gitlab-ci.yaml"]);
const MAXIMUM_GITLAB_FILES = 4096;
const MAXIMUM_GITLAB_FACTS = 4096;
const MAXIMUM_VALUE_CHARACTERS = 4096;
const MAXIMUM_SCOPE_CHARACTERS = 1024;

const GLOBAL_KEYS = new Set([
  "after_script",
  "before_script",
  "cache",
  "default",
  "hooks",
  "id_tokens",
  "image",
  "include",
  "services",
  "stages",
  "variables",
  "workflow",
]);

function isMap(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function literal(value) {
  return (
    typeof value === "string" &&
    value !== "" &&
    value.length <= MAXIMUM_VALUE_CHARACTERS &&
    !/[\u0000\r\n]/.test(value) &&
    !value.includes("$") &&
    !value.includes("{{") &&
    !value.includes("}}")
  );
}

function reportedText(value) {
  return (
    typeof value === "string" &&
    value.length <= MAXIMUM_VALUE_CHARACTERS &&
    !/[\u0000\r\n]/.test(value)
  );
}

function normalizedLocalPath(value) {
  if (!literal(value) || value.includes("\\")) {
    return "";
  }
  const path = value.startsWith("/") ? value.slice(1) : value;
  return invalidLocalPath(path) ? "" : path;
}

function invalidLocalPath(path) {
  return (
    path === "" ||
    isPosixAbsolute(path) ||
    isWindowsAbsolute(path) ||
    normalizePosix(path) !== path ||
    path === "." ||
    path === ".." ||
    path.startsWith("../")
  );
}

function scope(kind, name, field) {
  const value = name === "" ? `${kind}:${field}` : `${kind}:${name}:${field}`;
  return value.length <= MAXIMUM_SCOPE_CHARACTERS ? value : "";
}

function addUnsupported(result, path, reason) {
  if (result.unsupported.length >= MAXIMUM_GITLAB_FACTS) {
    fail(
      `the gitlab operation reports more than the ${MAXIMUM_GITLAB_FACTS} unsupported declarations`,
    );
  }
  result.unsupported.push(unsupported(path, reason));
}

function addImage(result, path, imageScope, value) {
  if (result.images.length >= MAXIMUM_GITLAB_FACTS) {
    fail(
      `the gitlab operation reports more than the ${MAXIMUM_GITLAB_FACTS} image declarations`,
    );
  }
  if (imageScope === "" || !reportedText(value)) {
    addUnsupported(
      result,
      path,
      "an image declaration is not bounded literal text",
    );
    return;
  }
  result.images.push({ path, scope: imageScope, image: value });
}

function addInclude(result, include) {
  if (result.includes.length >= MAXIMUM_GITLAB_FACTS) {
    fail(
      `the gitlab operation reports more than the ${MAXIMUM_GITLAB_FACTS} include declarations`,
    );
  }
  result.includes.push(include);
}

function emptyInclude(path, kind) {
  return {
    path,
    kind,
    local: "",
    project: "",
    file: "",
    ref: "",
    remote: "",
    integrity: "",
    component: "",
    template: "",
  };
}

function imageName(result, path, imageScope, value) {
  if (typeof value === "string") {
    addImage(result, path, imageScope, value);
    return;
  }
  if (isMap(value) && typeof value.name === "string") {
    addImage(result, path, imageScope, value.name);
    return;
  }
  addUnsupported(
    result,
    path,
    `${imageScope} is not a supported image declaration`,
  );
}

function imageDeclarations(result, path, kind, name, value) {
  const imageScope = scope(kind, name, "image");
  if (imageScope === "") {
    addUnsupported(
      result,
      path,
      "the image declaration has an oversized scope name",
    );
    return;
  }
  imageName(result, path, imageScope, value);
}

function serviceDeclarations(result, path, kind, name, value) {
  const serviceScope = scope(kind, name, "service");
  if (serviceScope === "") {
    addUnsupported(
      result,
      path,
      "the service declaration has an oversized scope name",
    );
    return;
  }
  const entries = Array.isArray(value) ? value : [value];
  if (entries.length > MAXIMUM_GITLAB_FACTS) {
    addUnsupported(
      result,
      path,
      `services declares more than the ${MAXIMUM_GITLAB_FACTS} entries`,
    );
    return;
  }
  for (const entry of entries) {
    imageName(result, path, serviceScope, entry);
  }
}

function inspectImageFields(result, path, kind, name, value) {
  if (Object.hasOwn(value, "image")) {
    imageDeclarations(result, path, kind, name, value.image);
  }
  if (Object.hasOwn(value, "services")) {
    serviceDeclarations(result, path, kind, name, value.services);
  }
}

function inspectDefaultImages(result, path, document) {
  if (!Object.hasOwn(document, "default")) {
    return;
  }
  if (!isMap(document.default)) {
    addUnsupported(result, path, "default is not a YAML map");
    return;
  }
  inspectImageFields(result, path, "default", "", document.default);
}

function inspectJobImages(result, path, document) {
  for (const [name, value] of Object.entries(document)) {
    if (GLOBAL_KEYS.has(name) || !isMap(value)) {
      continue;
    }
    if (!reportedText(name)) {
      addUnsupported(result, path, "a job name is not bounded text");
      continue;
    }
    inspectImageFields(result, path, "job", name, value);
  }
}

function inspectImages(result, path, document) {
  inspectImageFields(result, path, "global", "", document);
  inspectDefaultImages(result, path, document);
  inspectJobImages(result, path, document);
}

function onlyKeys(entry, allowed) {
  return Object.keys(entry).every((key) => allowed.has(key));
}

function literalValues(value) {
  if (literal(value)) {
    return [value];
  }
  if (
    !Array.isArray(value) ||
    value.length === 0 ||
    value.length > MAXIMUM_GITLAB_FACTS ||
    value.some((entry) => !literal(entry))
  ) {
    return null;
  }
  return value;
}

function includeRulesAreSupported(entry) {
  return !Object.hasOwn(entry, "rules");
}

function inspectLocal(result, state, path, entry) {
  const local = normalizedLocalPath(entry.local);
  if (local === "") {
    addUnsupported(
      result,
      path,
      "include.local must be a literal contained path",
    );
    return;
  }
  if (
    !onlyKeys(entry, new Set(["local", "rules"])) ||
    !includeRulesAreSupported(entry)
  ) {
    addUnsupported(
      result,
      path,
      "conditional or unsupported local include fields cannot be statically resolved",
    );
  }
  if (inspectLocalFile(result, state, path, local)) {
    const include = emptyInclude(path, "local");
    include.local = local;
    addInclude(result, include);
  }
}

function inspectProject(result, path, entry) {
  if (!onlyKeys(entry, new Set(["project", "file", "ref"]))) {
    addUnsupported(result, path, "the project include has unsupported fields");
    return;
  }
  const files = literalValues(entry.file);
  if (
    !literal(entry.project) ||
    files === null ||
    (Object.hasOwn(entry, "ref") && !literal(entry.ref))
  ) {
    addUnsupported(
      result,
      path,
      "the project include must use literal project, file, and ref values",
    );
    return;
  }
  for (const file of files) {
    const include = emptyInclude(path, "project");
    include.project = entry.project;
    include.file = file;
    include.ref = Object.hasOwn(entry, "ref") ? entry.ref : "";
    addInclude(result, include);
  }
}

function inspectRemote(result, path, entry) {
  if (!onlyKeys(entry, new Set(["remote", "integrity"]))) {
    addUnsupported(result, path, "the remote include has unsupported fields");
    return;
  }
  if (
    !literal(entry.remote) ||
    (Object.hasOwn(entry, "integrity") && !literal(entry.integrity))
  ) {
    addUnsupported(
      result,
      path,
      "the remote include must use literal remote and integrity values",
    );
    return;
  }
  const include = emptyInclude(path, "remote");
  include.remote = entry.remote;
  include.integrity = Object.hasOwn(entry, "integrity") ? entry.integrity : "";
  addInclude(result, include);
}

function inspectComponent(result, path, entry) {
  if (!onlyKeys(entry, new Set(["component", "inputs"]))) {
    addUnsupported(
      result,
      path,
      "the component include has unsupported fields",
    );
    return;
  }
  if (!literal(entry.component)) {
    addUnsupported(
      result,
      path,
      "the component include must use a literal identity",
    );
    return;
  }
  const include = emptyInclude(path, "component");
  include.component = entry.component;
  addInclude(result, include);
}

function inspectTemplate(result, path, entry) {
  if (!onlyKeys(entry, new Set(["template", "inputs"]))) {
    addUnsupported(result, path, "the template include has unsupported fields");
    return;
  }
  if (!literal(entry.template)) {
    addUnsupported(
      result,
      path,
      "the template include must use a literal built-in template identity",
    );
    return;
  }
  const include = emptyInclude(path, "template");
  include.template = entry.template;
  addInclude(result, include);
}

function inspectIncludeEntry(result, state, path, entry) {
  if (!isMap(entry)) {
    addUnsupported(result, path, "an include entry is not a YAML map");
    return;
  }
  const kinds = ["local", "project", "remote", "component", "template"].filter(
    (kind) => Object.hasOwn(entry, kind),
  );
  if (kinds.length !== 1) {
    addUnsupported(
      result,
      path,
      "an include entry must declare exactly one supported source kind",
    );
    return;
  }
  switch (kinds[0]) {
    case "local":
      inspectLocal(result, state, path, entry);
      break;
    case "project":
      inspectProject(result, path, entry);
      break;
    case "remote":
      inspectRemote(result, path, entry);
      break;
    case "component":
      inspectComponent(result, path, entry);
      break;
    case "template":
      inspectTemplate(result, path, entry);
      break;
  }
}

function inspectIncludes(result, state, path, document) {
  if (!Object.hasOwn(document, "include")) {
    return;
  }
  const declared = document.include;
  const entries = Array.isArray(declared) ? declared : [declared];
  if (entries.length > MAXIMUM_GITLAB_FACTS) {
    addUnsupported(
      result,
      path,
      `include declares more than the ${MAXIMUM_GITLAB_FACTS} entries`,
    );
    return;
  }
  for (const entry of entries) {
    inspectIncludeEntry(result, state, path, entry);
  }
}

function inspectLocalFile(result, state, parent, path) {
  if (!result.governed.has(path)) {
    addUnsupported(
      result,
      parent,
      `local include ${JSON.stringify(path)} is not a governed repository input`,
    );
    return false;
  }
  result.controls.add(path);
  const previous = state.get(path);
  if (previous === "visiting") {
    addUnsupported(
      result,
      parent,
      `local include ${JSON.stringify(path)} creates an include cycle`,
    );
    return true;
  }
  if (previous === "visited") {
    addUnsupported(
      result,
      parent,
      `local include ${JSON.stringify(path)} is included more than once`,
    );
    return true;
  }
  inspectFile(result, state, path);
  return true;
}

function inspectFile(result, state, path) {
  if (state.size >= MAXIMUM_GITLAB_FILES) {
    fail(
      `the gitlab operation reads more than the ${MAXIMUM_GITLAB_FILES} files`,
    );
  }
  state.set(path, "visiting");
  const source = readTargetFile(
    join(result.root, path),
    path,
    result.unsupported,
  );
  if (source === null) {
    state.set(path, "visited");
    return;
  }
  let document;
  try {
    document = yaml.load(source, { filename: path, schema: yaml.JSON_SCHEMA });
  } catch (error) {
    addUnsupported(result, path, error.message);
    state.set(path, "visited");
    return;
  }
  if (!isMap(document)) {
    addUnsupported(result, path, "the GitLab configuration is not a YAML map");
    state.set(path, "visited");
    return;
  }
  inspectIncludes(result, state, path, document);
  inspectImages(result, path, document);
  inspectUnconditionalScripts(result, path, document);
  state.set(path, "visited");
}

function scriptLines(value) {
  const lines = typeof value === "string" ? [value] : value;
  return Array.isArray(lines) && lines.every((line) => typeof line === "string")
    ? lines
    : null;
}

function jobSetup(job, setup) {
  if (Object.hasOwn(job, "before_script")) {
    return job.before_script;
  }
  const inherited = job.inherit?.default;
  if (inherited === undefined || inherited === true) {
    return setup;
  }
  if (inherited === false) {
    return [];
  }
  if (
    !Array.isArray(inherited) ||
    inherited.some((key) => typeof key !== "string")
  ) {
    return null;
  }
  return inherited.includes("before_script") ? setup : [];
}

function inheritedVariable(job, variables, name) {
  const inherited = job.inherit?.variables;
  if (
    inherited === false ||
    (Array.isArray(inherited) && !inherited.includes(name))
  ) {
    return "";
  }
  if (
    inherited !== undefined &&
    inherited !== true &&
    !Array.isArray(inherited)
  ) {
    return null;
  }
  return isMap(variables) ? variables[name] : null;
}

function jobHasShellStartup(job, variables) {
  if (job.variables !== undefined && !isMap(job.variables)) {
    return true;
  }
  const matrix = jobMatrixVariables(job);
  if (matrix === null) {
    return true;
  }
  return ["BASH_ENV", "SHELLOPTS", "PS4"].some((name) => {
    const value = Object.hasOwn(job.variables ?? {}, name)
      ? job.variables[name]
      : inheritedVariable(job, variables, name);
    return matrix.some((row) => {
      const values = Object.hasOwn(row, name)
        ? matrixVariableValues(row[name])
        : [value];
      return values.some((value) => shellVariableMayExecute(name, value));
    });
  });
}

function matrixVariableValues(value) {
  const values = Array.isArray(value) ? value : [value];
  return values.length > 0 && values.every((item) => typeof item === "string")
    ? values
    : null;
}

function jobMatrixVariables(job) {
  const parallel = job.parallel;
  if (parallel === undefined || (Number.isInteger(parallel) && parallel > 0)) {
    return [{}];
  }
  const matrix = isMap(parallel) ? parallel.matrix : null;
  if (!Array.isArray(matrix) || matrix.length === 0) {
    return null;
  }
  return matrix.every(
    (row) =>
      isMap(row) &&
      Object.keys(row).length > 0 &&
      Object.values(row).every((value) => matrixVariableValues(value) !== null),
  )
    ? matrix
    : null;
}

function shellVariableMayExecute(name, value) {
  if (value === undefined) {
    return false;
  }
  const text = isMap(value) ? value.value : value;
  if (typeof text !== "string") {
    return true;
  }
  return name === "PS4" ? /[$`\\]/.test(text) : text !== "";
}

function jobScriptIssue(job, setup, variables) {
  const conditional = ["rules", "only", "except", "extends", "needs"].find(
    (key) => Object.hasOwn(job, key),
  );
  if (conditional) {
    return `job uses unsupported execution setting ${conditional}`;
  }
  if (jobHasShellStartup(job, variables)) {
    return "effective variables or parallel matrix contain unsupported BASH_ENV, SHELLOPTS, or PS4 settings";
  }
  if (![undefined, false].includes(job.allow_failure)) {
    return "job permits failure through allow_failure";
  }
  if (![undefined, "on_success", "always"].includes(job.when)) {
    return "job when setting does not establish unconditional execution";
  }
  return dedicatedScriptIssue(job, setup);
}

function dedicatedScriptIssue(job, setup) {
  const before = scriptLines(jobSetup(job, setup.value));
  if (before === null) {
    return "effective before_script is not a supported command list";
  }
  if (before.length !== 0) {
    const origin = Object.hasOwn(job, "before_script")
      ? "job declares"
      : `job inherits from ${setup.path}`;
    return `${origin} a nonempty before_script; move setup to an earlier job`;
  }
  const scripts = scriptLines(job.script);
  if (scripts?.length !== 1) {
    return "script must contain exactly one dedicated gate command";
  }
  return boundedJobScript(job) === ""
    ? "script must be nonempty, at most 65536 characters, and contain no NUL"
    : "";
}

function boundedJobScript(job) {
  const script = scriptLines(job.script)?.join("\n") ?? "";
  return script.length > 65536 || script.includes("\0") ? "" : script;
}

function inspectScriptDefaults(result, path, document) {
  if (document.variables !== undefined) {
    result.variables =
      isMap(result.variables) && isMap(document.variables)
        ? { ...result.variables, ...document.variables }
        : null;
  }
  if (Object.hasOwn(document, "before_script")) {
    result.beforeScript = { value: document.before_script, path };
  }
  if (
    isMap(document.default) &&
    Object.hasOwn(document.default, "before_script")
  ) {
    result.defaultBeforeScript = {
      value: document.default.before_script,
      path,
    };
  }
}

function workflowRestrictsExecution(workflow) {
  return (
    workflow !== undefined &&
    (!isMap(workflow) || Object.hasOwn(workflow, "rules"))
  );
}

function inspectUnconditionalScripts(result, path, document) {
  if (workflowRestrictsExecution(document.workflow)) {
    result.workflowIssue = `workflow in ${path} has unsupported execution rules`;
  }
  inspectScriptDefaults(result, path, document);
  for (const [name, job] of Object.entries(document)) {
    if (GLOBAL_KEYS.has(name) || name.startsWith(".") || !isMap(job)) {
      continue;
    }
    if (result.jobs.has(name)) {
      result.workflowIssue = `job ${name} is declared more than once across GitLab includes`;
    }
    result.jobs.add(name);
    if (result.scripts.length >= MAXIMUM_GITLAB_FACTS) {
      fail("the gitlab operation exceeds its script fact limit");
    }
    result.scripts.push({ path, name, job });
  }
}

function scriptFacts(result) {
  let reason = result.workflowIssue;
  if (result.unsupported.length > 0) {
    reason =
      "GitLab configuration has unsupported inputs; see supplyChain.gitLabCoverage";
  }
  const external = result.includes.find((include) => include.kind !== "local");
  if (external) {
    reason = `include in ${external.path} is not local; its execution settings cannot establish gate ownership`;
  }
  const setup = result.defaultBeforeScript ?? result.beforeScript;
  return result.scripts.map(({ path, name, job }) => ({
    path,
    job: name,
    script: boundedJobScript(job),
    reason: reason || jobScriptIssue(job, setup, result.variables),
  }));
}

function inspectRoot(request, root) {
  const result = {
    root: request.root,
    governed: new Set(request.governedPaths),
    controls: new Set([root]),
    images: [],
    includes: [],
    unsupported: [],
    scripts: [],
    beforeScript: { value: [], path: root },
    defaultBeforeScript: undefined,
    variables: {},
    jobs: new Set(),
    workflowIssue: "",
  };
  if (result.governed.has(root)) {
    inspectFile(result, new Map(), root);
  } else {
    addUnsupported(
      result,
      root,
      "the root GitLab configuration is not a governed repository input",
    );
  }
  return {
    controls: [...result.controls].sort(),
    images: result.images,
    includes: result.includes,
    unsupported: result.unsupported,
    scripts: scriptFacts(result),
  };
}

export function gitlab(request) {
  if (request.paths.length === 0 || request.paths.length > ROOT_FILES.size) {
    fail(
      "the gitlab request must name one or two root GitLab configuration files",
    );
  }
  const roots = new Set();
  for (const path of request.paths) {
    if (!ROOT_FILES.has(path) || roots.has(path)) {
      fail(
        "the gitlab request must name distinct root GitLab configuration files",
      );
    }
    roots.add(path);
  }
  const results = [...roots].map((root) => inspectRoot(request, root));
  const combined = {
    controls: [...new Set(results.flatMap((result) => result.controls))].sort(),
  };
  for (const field of ["images", "includes", "unsupported", "scripts"]) {
    combined[field] = results.flatMap((result) => result[field]);
  }
  if (
    Object.values(combined).some((facts) => facts.length > MAXIMUM_GITLAB_FACTS)
  ) {
    fail("the gitlab operation exceeds its combined fact limit");
  }
  return combined;
}
