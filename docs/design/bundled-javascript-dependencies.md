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
JSON and CSS remain opaque. A remaining import of a declared bundled name blocks
both the artifact and the source allowance.

## Guarantee

The check is a bounded static dependency check, not a proof that arbitrary
JavaScript cannot load a package. It guarantees that every loader form in the
supported model names a literal specifier and that each other use of a loader
name fails verification. Unsupported forms block the allowance; they never pass
silently as clean coverage.

Runtime artifacts support static imports and re-exports, literal dynamic
imports, and literal calls of `require`, `require.resolve`, any `.require`
member, and `getBuiltinModule`. Runtime imports of `module` or `node:module`
fail, which covers `createRequire` and the Module class reached through either
loader. The names `require` and `getBuiltinModule` are treated as loaders
regardless of binding. Every other reference fails: aliases, `call` or `apply`,
member reads, element access, shorthand properties, and destructuring or
assignment-pattern keys, whether bare, quoted, or computed from a literal.
Declarations, object-literal keys, `typeof`, `require.main`, `require.cache`,
type positions, and string values remain ordinary code. Keys computed at run
time, reflection APIs, reflection through `constructor` or prototypes, and code
evaluated from strings are outside the guarantee.

Declaration artifacts support imports, re-exports, import types, and
`reference` directives. TypeScript may satisfy a bare specifier with the package
itself or its `@types` counterpart, and a Node built-in with `@types/node`, so
each import in a TypeScript artifact counts against all identities the consumer
could need. Ambient module declarations supplied by other packages and consumer
path mappings are outside the guarantee.

Missing or malformed files, escaping or linked paths, other artifact types, and
unresolved local imports fail verification. Export patterns, `browser`,
`typesVersions`, `directories.bin`, and directory imports governed by a nested
`package.json` remain unsupported rather than silently losing or misresolving a
possible consumer entry. File, import, byte, and process limits bound analysis.
The reader does not execute repository code, build artifacts, or install
packages. Tests compare each supported and rejected form with Node execution or
TypeScript compilation of a consumer, including valid neighboring cases.

Repositories build before architecture verification and keep their ordinary
build and packed-consumer contracts. Reading a current artifact is not evidence
of build freshness or arbitrary JavaScript behavior. The check establishes the
declared dependency boundary of the parsed entry closure; it does not replace
those contracts. Verification is shared by native and provider imports and
cached only within one architecture analysis, without a new receipt or digest.
