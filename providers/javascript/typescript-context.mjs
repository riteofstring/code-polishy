import { basename, dirname, join, relative, sep } from "node:path";

import ts from "typescript";

export function compilerFor(analysis) {
  return {
    ...ts,
    createLanguageService(host, _registry, mode) {
      const documents = ts.createDocumentRegistry(
        ts.sys.useCaseSensitiveFileNames,
        host.getCurrentDirectory(),
      );
      for (const name of ["acquireDocumentWithKey", "updateDocumentWithKey"]) {
        const original = documents[name];
        documents[name] = (...args) => {
          inheritModuleFormat(analysis, host, args[0], args[7]);
          return original(...args);
        };
      }
      return ts.createLanguageService(host, documents, mode);
    },
  };
}

function inheritModuleFormat(analysis, host, absolute, options) {
  const path = relative(analysis.root, absolute).split(sep).join("/");
  const owner = analysis.classifications.get(path)?.sourcePackage;
  if (!owner) return;
  options.impliedNodeFormat = ts.getImpliedNodeFormatForFile(
    join(analysis.root, dirname(owner), basename(path)),
    undefined,
    ts.sys,
    host.getCompilationSettings(),
  );
}
