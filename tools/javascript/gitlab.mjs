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
  state.set(path, "visited");
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
  const result = {
    root: request.root,
    governed: new Set(request.governedPaths),
    controls: new Set(),
    images: [],
    includes: [],
    unsupported: [],
  };
  const state = new Map();
  for (const root of roots) {
    result.controls.add(root);
    if (!result.governed.has(root)) {
      addUnsupported(
        result,
        root,
        "the root GitLab configuration is not a governed repository input",
      );
      continue;
    }
    if (state.has(root)) {
      addUnsupported(
        result,
        root,
        "the root GitLab configuration is already owned by another include",
      );
      continue;
    }
    inspectFile(result, state, root);
  }
  return {
    controls: [...result.controls].sort(),
    images: result.images,
    includes: result.includes,
    unsupported: result.unsupported,
  };
}
