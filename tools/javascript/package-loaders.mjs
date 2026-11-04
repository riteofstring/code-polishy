import { isBuiltin } from "node:module";

import { parseForESLint } from "@typescript-eslint/parser";

const LOADER_NAMES = new Set(["require", "getBuiltinModule"]);
const REQUIRE_PROPERTIES = new Set(["main", "cache"]);
const ORIGIN_NAMES = new Set(["require", "module"]);
const ORDINARY = "ordinary";
const UNSUPPORTED = "unsupported";

function parsedScript(path, text) {
  const options = { filePath: `${path}.js`, range: true };
  try {
    return parseForESLint(text, { ...options, sourceType: "module" });
  } catch {
    return parseForESLint(text, {
      ...options,
      sourceType: "script",
      ecmaFeatures: { globalReturn: true },
    });
  }
}

function linkParents(node, parent, visitorKeys) {
  node.parent = parent;
  for (const key of visitorKeys[node.type] ?? []) {
    const value = node[key];
    for (const child of Array.isArray(value) ? value : [value])
      if (child && typeof child.type === "string")
        linkParents(child, node, visitorKeys);
  }
}

function valueReferences(scopeManager) {
  const references = new Map();
  for (const scope of scopeManager.scopes)
    for (const reference of scope.references)
      if (reference.isValueReference)
        references.set(reference.identifier, reference);
  return references;
}

function freeIdentifier(references, node, name) {
  return (
    node.type === "Identifier" &&
    node.name === name &&
    references.get(node)?.resolved === null
  );
}

function rejectUnknownOrigins(scopeManager) {
  for (const scope of scopeManager.scopes) {
    if (scope.type === "with")
      throw new Error(
        "published artifact uses a with statement, so its loader origins are unknown",
      );
    for (const reference of scope.references)
      if (
        ORIGIN_NAMES.has(reference.identifier.name) &&
        reference.isWrite() &&
        reference.resolved === null
      )
        throw new Error(
          `published artifact reassigns ${reference.identifier.name}, so its loader origin is unknown`,
        );
  }
}

function chainOuter(node) {
  return node.parent?.type === "ChainExpression" ? node.parent : node;
}

function calledBy(node) {
  const outer = chainOuter(node);
  const parent = outer.parent;
  return parent?.type === "CallExpression" && parent.callee === outer
    ? parent
    : null;
}

function loaderText(node) {
  if (node.type === "Identifier") return node.name;
  if (node.type === "Literal" && typeof node.value === "string")
    return node.value;
  if (node.type === "TemplateLiteral" && node.expressions.length === 0)
    return node.quasis[0].value.cooked;
  return null;
}

const KEY_ROLES = {
  MemberExpression: (parent, key) => {
    if (parent.property !== key) return null;
    return parent.computed === (key.type !== "Identifier")
      ? (calledBy(parent) ?? UNSUPPORTED)
      : null;
  },
  Property: (parent, key) => {
    if (parent.key !== key) return null;
    return parent.parent.type === "ObjectPattern" && !parent.shorthand
      ? UNSUPPORTED
      : ORDINARY;
  },
  ImportSpecifier: (parent, key) => {
    if (parent.imported !== key) return null;
    return parent.local.name === loaderText(key) ? ORDINARY : UNSUPPORTED;
  },
  ExportSpecifier: () => UNSUPPORTED,
};

function keyRole(key) {
  return KEY_ROLES[key.parent.type]?.(key.parent, key) ?? null;
}

function requireMemberRole(identifier, parent, free) {
  if (
    identifier.name !== "require" ||
    parent.type !== "MemberExpression" ||
    parent.object !== chainOuter(identifier) ||
    parent.computed
  )
    return UNSUPPORTED;
  if (REQUIRE_PROPERTIES.has(parent.property.name)) return ORDINARY;
  return parent.property.name === "resolve" && free
    ? (calledBy(parent) ?? UNSUPPORTED)
    : UNSUPPORTED;
}

function readRole(identifier, references) {
  const free = freeIdentifier(references, identifier, "require");
  const call = calledBy(identifier);
  if (call) return identifier.name === "require" && !free ? UNSUPPORTED : call;
  const parent = chainOuter(identifier).parent;
  if (parent.type === "UnaryExpression" && parent.operator === "typeof")
    return ORDINARY;
  return requireMemberRole(identifier, parent, free);
}

function loaderRole(node, references) {
  if (!LOADER_NAMES.has(loaderText(node))) return ORDINARY;
  const reference = node.type === "Identifier" ? references.get(node) : null;
  if (reference)
    return reference.isRead() ? readRole(node, references) : ORDINARY;
  return keyRole(node) ?? ORDINARY;
}

function calleeName(callee) {
  if (callee.type === "Identifier") return callee.name;
  return callee.computed ? loaderText(callee.property) : callee.property.name;
}

function requireLoaderPolicy(callee, specifier, references) {
  const name = calleeName(callee);
  if (name === "getBuiltinModule" && !isBuiltin(specifier))
    throw new Error("getBuiltinModule must name a Node built-in");
  const receiver = callee.type === "MemberExpression" ? callee.object : null;
  if (
    name === "require" &&
    receiver !== null &&
    !freeIdentifier(references, receiver, "module")
  )
    throw new Error(
      "a require member call on a receiver other than the module binding has an unknown resolution origin",
    );
}

function loaderCall(call, references) {
  const [argument] = call.arguments;
  if (argument?.type !== "Literal" || typeof argument.value !== "string")
    throw new Error("published artifact has a computed loader call");
  requireLoaderPolicy(
    call.callee.type === "ChainExpression"
      ? call.callee.expression
      : call.callee,
    argument.value,
    references,
  );
  return {
    text: argument.value,
    position: argument.range[0],
    kind: "runtime",
  };
}

export function loaderSpecifiers(path, text) {
  const { ast, scopeManager, visitorKeys } = parsedScript(path, text);
  linkParents(ast, null, visitorKeys);
  rejectUnknownOrigins(scopeManager);
  const references = valueReferences(scopeManager);
  const specifiers = [];
  const visit = (node) => {
    const role = loaderRole(node, references);
    if (role === UNSUPPORTED)
      throw new Error(
        "published artifact uses require or getBuiltinModule outside a supported loader call",
      );
    if (role !== ORDINARY) specifiers.push(loaderCall(role, references));
    for (const key of visitorKeys[node.type] ?? []) {
      const value = node[key];
      for (const child of Array.isArray(value) ? value : [value])
        if (child && typeof child.type === "string") visit(child);
    }
  };
  visit(ast);
  return specifiers;
}
