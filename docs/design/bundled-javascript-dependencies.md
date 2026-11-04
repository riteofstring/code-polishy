# Bundled JavaScript dependencies

Source imports alone do not establish an install-time dependency. A library can
inline a package into its published JavaScript while retaining it only in
`devDependencies` for builds. Marking that library's source as development-only
would misdescribe the production code. npm's `bundleDependencies` describes
shipping dependency directories, not inlining, and supplies no such evidence.

`scope.bundledDependencies` names an exact package manifest and exact dependency
names. Scope wildcards are intentionally unsupported: a newly added dependency
must receive its own declaration. Each name must occur only in that manifest's
`devDependencies`; runtime, optional, and peer declarations contradict the claim.
The declaration does not change source classification, dependency direction,
quality checks, supply-chain analysis, or other packages' dependency authority.

The sealed TypeScript parser reads the package's existing built entry files.
It covers every exact `exports` target, explicit `main`, `module`, `types`,
`typings`, and `bin` entry, and existing declaration companions of JavaScript
and extensionless entries. Consumers also reach the root `index.js` when neither
`exports` nor `main` is declared, and the root `index.d.ts` when neither `types`
nor `typings` is declared, so existing fallback files join the closure.
It follows local runtime imports through actual JavaScript files, never their
TypeScript substitutes, trying an extensionless specifier as Node's CommonJS
resolver does: exact, `.js`, `.json`, `.node`, then the directory index. It
follows reachable public declarations separately.
Private declaration files outside that closure do not impose consumer needs.
Extensionless files are parsed as JavaScript because Node executes them; only
JSON and CSS remain opaque. Re-exports, literal dynamic imports, literal
`require`, import types, and declaration references participate. A remaining
import of a declared bundled name blocks both the artifact and the source
allowance.

Missing or malformed files, escaping or linked paths, computed loaders, other
artifact types, and unresolved local imports fail verification. Export patterns,
`browser`, `typesVersions`, `directories.bin`, and directory imports governed by
a nested `package.json` remain unsupported rather than silently losing or
misresolving a possible consumer entry. File, import, byte, and process limits
bound analysis.
The reader does not execute repository code, build artifacts, or install packages.

Repositories build before architecture verification and keep their ordinary
build and packed-consumer contracts. Reading a current artifact is not evidence
of build freshness or arbitrary JavaScript behavior. The check establishes the
declared dependency boundary of the parsed entry closure; it does not replace
those contracts. Verification is shared by native and provider imports and
cached only within one architecture analysis, without a new receipt or digest.
