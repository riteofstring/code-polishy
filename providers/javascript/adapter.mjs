import { createAnalysis, containTypeScriptReads } from "./context.mjs";
import { analyzeSource } from "./analysis.mjs";
import { typecheck } from "./typecheck.mjs";
import { deadcode } from "./deadcode.mjs";
import { discover } from "./discovery.mjs";

function validateRequest(request) {
  if (
    request.protocolVersion !== 4 ||
    request.tools?.length !== 1 ||
    request.tools[0].id !== "node" ||
    request.tools[0].name !== "node" ||
    request.tools[0].version !== process.versions.node
  )
    throw new Error("request requires the exact policy-owned Node runtime");
  validateScope(request);
}

function validateScope(request) {
  const arrays = [
    "files",
    "context",
    "inventory",
    "scopes",
    "diagnosticFiles",
    "writeFiles",
  ];
  if (
    !arrays.every((name) => Array.isArray(request[name])) ||
    !request.policy ||
    typeof request.provider !== "string" ||
    typeof request.complete !== "boolean"
  )
    throw new Error("request is missing its scope or effective policy");
}

export async function analyze(request) {
  validateRequest(request);
  if (request.operation === "discover") return discover(request);
  const analysis = createAnalysis(request);
  for (const path of request.files) analysis.read(path);
  const restore = containTypeScriptReads(analysis);
  try {
    if (request.capability === "typecheck") await typecheck(analysis);
    else if (request.capability === "dead-code") await deadcode(analysis);
    else await analyzeSource(analysis);
    return analysis.finish();
  } finally {
    restore();
  }
}
