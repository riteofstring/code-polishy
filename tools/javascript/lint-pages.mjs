import { fail, requireExactObject } from "./protocol.mjs";

const MAXIMUM_LINT_RESULTS = 5000;
const MAXIMUM_LINT_PAGE_BYTES = 4 * 1024 * 1024;

export function lintPage(request, analyze) {
  requireCursor(request);
  const result = { findings: [], comments: [], unsupported: [], next: null };
  let count = 0;
  let bytes = 0;
  for (
    let pathIndex = request.cursor.pathIndex;
    pathIndex < request.paths.length;
    pathIndex++
  ) {
    const file = analyze(request.paths[pathIndex]);
    const offset =
      pathIndex === request.cursor.pathIndex ? request.cursor.resultIndex : 0;
    for (const { kind, entry, resultIndex } of fileRecords(file, offset)) {
      const size = Buffer.byteLength(JSON.stringify(entry), "utf8") + 1;
      if (size > MAXIMUM_LINT_PAGE_BYTES)
        fail("one lint result exceeds the page byte limit");
      if (
        count === MAXIMUM_LINT_RESULTS ||
        bytes + size > MAXIMUM_LINT_PAGE_BYTES
      ) {
        result.next = { pathIndex, resultIndex };
        return result;
      }
      result[kind].push(entry);
      count++;
      bytes += size;
    }
  }
  return result;
}

function* fileRecords(file, offset) {
  let resultIndex = 0;
  for (const kind of ["findings", "comments", "unsupported"]) {
    for (const entry of file[kind]) {
      if (resultIndex >= offset) yield { kind, entry, resultIndex };
      resultIndex++;
    }
  }
  if (offset > resultIndex) fail("the lint cursor exceeds the file's results");
}

function requireCursor(request) {
  requireExactObject(request.cursor, "the lint cursor", [
    "pathIndex",
    "resultIndex",
  ]);
  for (const value of Object.values(request.cursor)) {
    if (!Number.isSafeInteger(value) || value < 0)
      fail("the lint cursor must contain nonnegative safe integers");
  }
  if (request.cursor.pathIndex >= Math.max(1, request.paths.length)) {
    fail("the lint cursor exceeds the selected paths");
  }
}
