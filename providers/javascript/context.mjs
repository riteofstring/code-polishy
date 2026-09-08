import { createHash } from "node:crypto";
import { readFileSync, realpathSync } from "node:fs";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

const providerRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

export function contained(root, absolute) {
  const path = relative(root, absolute);
  return path !== ".." && !path.startsWith(`..${sep}`) && !isAbsolute(path);
}

class Analysis {
  constructor(request) {
    this.request = request;
    this.notes = new Set();
    this.root = realpathSync(request.projectRoot);
    this.inputs = new Map(request.context.map((input) => [input.path, input]));
    this.classifications = new Map(
      request.policy.files.map((file) => [file.path, file]),
    );
    this.response = {
      protocolVersion: 2,
      status: "pass",
      evidence: [
        `${request.capability} completed using the installed JS/TS provider`,
      ],
      coverage: { analyzed: [], unsupported: [] },
      findings: [],
    };
  }

  read(path) {
    const absolute = realpathSync(join(this.root, path));
    if (!contained(this.root, absolute))
      throw new Error(`input escapes project: ${path}`);
    const data = readFileSync(absolute);
    if (data.length > 16 * 1024 * 1024)
      throw new Error(`input exceeds 16 MiB: ${path}`);
    const sha256 = createHash("sha256").update(data).digest("hex");
    const prior = this.inputs.get(path);
    if (prior && prior.sha256 !== sha256)
      throw new Error(`input changed: ${path}`);
    this.inputs.set(path, { path, sha256 });
    return data.toString("utf8");
  }

  location(path, offset) {
    const prefix = this.read(path).slice(0, offset);
    const lines = prefix.split("\n");
    return {
      line: lines.length,
      column: Buffer.byteLength(lines.at(-1), "utf8") + 1,
    };
  }

  diagnostic(path, rule, message, offset = 0, subject = rule) {
    this.response.findings.push({
      capability: this.request.capability,
      path,
      rule,
      subject,
      message: message.slice(0, 4096),
      ...this.location(path, offset),
    });
  }

  unsupported(path, reason) {
    if (this.response.coverage.unsupported.some((item) => item.path === path))
      return;
    this.response.coverage.analyzed = this.response.coverage.analyzed.filter(
      (item) => item !== path,
    );
    this.response.coverage.unsupported.push({
      path,
      reason: reason.slice(0, 4096),
    });
  }

  note(message) {
    this.notes.add(message.slice(0, 4096));
  }

  finish() {
    const notes = [...this.notes].sort();
    this.response.notes = notes.slice(0, 31);
    if (notes.length > 31)
      this.response.notes.push(
        `${notes.length - 31} additional provider context notes omitted from the bounded summary`,
      );
    if (this.response.coverage.unsupported.length)
      this.response.status = "incomplete";
    else if (this.response.findings.length) this.response.status = "findings";
    this.response.inputs = [...this.inputs.values()].sort((a, b) =>
      a.path.localeCompare(b.path),
    );
    return this.response;
  }
}

export function containTypeScriptReads(analysis) {
  const original = ts.sys.readFile;
  ts.sys.readFile = (absolute, encoding) => {
    let canonical;
    try {
      canonical = realpathSync(absolute);
    } catch {
      return undefined;
    }
    if (contained(analysis.root, canonical)) {
      const path = relative(analysis.root, canonical).split(sep).join("/");
      return analysis.read(path);
    }
    return contained(providerRoot, canonical)
      ? original(canonical, encoding)
      : undefined;
  };
  return () => {
    ts.sys.readFile = original;
  };
}

export function packageFor(analysis, path) {
  let directory = dirname(path);
  for (;;) {
    const manifest =
      directory === "." ? "package.json" : `${directory}/package.json`;
    if (analysis.inputs.has(manifest))
      return {
        root: directory,
        manifest,
        data: JSON.parse(analysis.read(manifest)),
      };
    if (directory === ".") return null;
    directory = dirname(directory);
  }
}

export function createAnalysis(request) {
  return new Analysis(request);
}
