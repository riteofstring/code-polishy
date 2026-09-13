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
    this.context = new Map(request.context.map((input) => [input.path, input]));
    this.inputs = new Map();
    this.units = new Map(request.units.map((unit) => [unit.id, unit]));
    this.reportable = new Set(request.diagnosticFiles);
    this.writable = new Set(request.writeFiles);
    this.classifications = new Map(
      request.policy.files.map((file) => [file.path, file]),
    );
    this.response = {
      protocolVersion: 3,
      status: "pass",
      evidence: [
        `${request.capability} completed using the installed JS/TS provider`,
      ],
      coverage: { analyzed: [], unsupported: [] },
      findings: [],
    };
  }

  readBytes(path) {
    const absolute = realpathSync(join(this.root, path));
    if (!contained(this.root, absolute))
      throw new Error(`input escapes project: ${path}`);
    const data = readFileSync(absolute);
    if (data.length > 16 * 1024 * 1024)
      throw new Error(`input exceeds 16 MiB: ${path}`);
    const sha256 = createHash("sha256").update(data).digest("hex");
    const prior = this.inputs.get(path) ?? this.context.get(path);
    if (prior && prior.sha256 !== sha256)
      throw new Error(`input changed: ${path}`);
    this.inputs.set(path, { path, sha256 });
    return data;
  }

  read(path) {
    const data = this.readBytes(path);
    const source = data.toString("utf8");
    if (!Buffer.from(source, "utf8").equals(data))
      throw new Error(`source is not valid UTF-8: ${path}`);
    return source;
  }

  unit(path) {
    const unit = this.units.get(this.classifications.get(path)?.unit);
    if (!unit) throw new Error(`source has no resolved analysis unit: ${path}`);
    return unit;
  }

  owns(path) {
    return this.classifications.get(path)?.owner === this.request.provider;
  }

  analyzed(path) {
    if (!this.response.coverage.analyzed.includes(path))
      this.response.coverage.analyzed.push(path);
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
      subject: boundedText(subject, 1024),
      message: boundedText(message, 4096),
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
      reason: boundedText(reason, 4096),
    });
  }

  note(message) {
    this.notes.add(boundedText(message, 1024));
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
  const unit = analysis.unit(path);
  if (!unit.manifest) return null;
  return {
    root: unit.packageRoot,
    manifest: unit.manifest,
    data: JSON.parse(analysis.read(unit.manifest)),
  };
}

export function resolutionPath(analysis, path) {
  return analysis.classifications.get(path)?.sourcePackage || path;
}

export function boundedText(value, maximum) {
  const bytes = Buffer.from(String(value), "utf8");
  let end = Math.min(maximum, bytes.length);
  while (end > 0 && end < bytes.length && (bytes[end] & 0xc0) === 0x80) end--;
  return bytes.subarray(0, end).toString("utf8");
}

export function createAnalysis(request) {
  return new Analysis(request);
}
