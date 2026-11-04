# Bundled JavaScript dependencies

Source imports alone do not establish an install-time dependency. A library can
inline a package into its published JavaScript while retaining it only in
`devDependencies` for builds. Marking that library's source as development-only
would misdescribe the production code. npm's `bundleDependencies` describes
shipping dependency directories, not inlining, and supplies no such evidence.

`scope.bundledDependencies` names an exact package manifest and exact dependency
names. Scope wildcards are intentionally unsupported: a newly added dependency
must receive its own declaration. Each name must occur only in that manifest's
`devDependencies`; runtime, optional, and peer declarations contradict the
claim. The declaration does not change source classification, dependency
direction, quality checks, supply-chain analysis, or other packages' dependency
authority.

The sealed TypeScript parser reads the package's existing built entry files.
It covers every exact `exports` target and explicit `main`, `module`, `types`,
`typings`, and `bin` entry. Without `exports`, the file Node selects for the
package directory also joins. Public type entries are the files TypeScript's
resolvers select for each library entry and for the package directory, so
preferred TypeScript sources, declaration companions, extensionless entries, and
the root `index.d.ts` fallback match consumers.
Runtime and type resolution stay distinct because consumers select different
files for them. Local runtime imports resolve with Node's own CommonJS resolver,
which reads package metadata without executing code, so extension order,
directory manifests, and index files match Node. ESM consumers accept only exact
paths, a subset of those results. Local imports from TypeScript artifacts use
the sealed TypeScript resolver under Node10, NodeNext ESM and CommonJS, and
Bundler resolution, and every file any of them selects joins the closure.
`reference path` directives name files directly. A resolved file outside the
package fails verification. Imports of the package's own name resolve into
the same closure: with `exports`, the subpath must be an exact export key whose
targets are already entries; without `exports`, the subpath resolves as a local
file from the package root.
Private declaration files outside that closure do not impose consumer needs.
Extensionless files are parsed as JavaScript because Node executes them; only
JSON and CSS remain opaque. A remaining import of a declared bundled name blocks
both the artifact and the source allowance.

## Guarantee

The check is a bounded static dependency check, not a proof that arbitrary
JavaScript cannot load a package. Behavior outside the model below is a
documented limitation, not a defect to patch case by case. It guarantees that
every loader form in the supported model names a literal specifier and that each
other use of a loader name fails verification. Unsupported forms block the
allowance; they never pass silently as clean coverage.

Runtime artifacts support static imports and re-exports, literal dynamic
imports, and literal calls of `require`, `require.resolve`, any `.require`
member, and `getBuiltinModule`. Runtime imports of `module` or `node:module`
fail, which covers `createRequire` and the Module class reached through either
loader. The names `require` and `getBuiltinModule` are treated as loaders
regardless of binding. Every other reference fails: aliases, `call` or `apply`,
member reads, element access, shorthand properties, destructuring or
assignment-pattern keys, and import or export specifier names. A literal key is
an identifier or a string literal, optionally parenthesized or written as a
computed property name. Declarations, object-literal keys, `typeof`,
`require.main`, `require.cache`, type positions, and string values remain
ordinary code. Keys produced by any other expression, such as concatenation or
comma and conditional expressions, are computed at run time and, with reflection
APIs, reflection through `constructor` or prototypes, and code evaluated from
strings, are outside the guarantee.

Declaration and TypeScript artifacts support imports, re-exports, import types,
and `reference` directives. TypeScript may satisfy a bare specifier with the
package itself or its `@types` counterpart, and a Node built-in with
`@types/node`, so each import in a TypeScript artifact counts against all
identities the consumer could need. Ambient module declarations supplied by
other packages and consumer path mappings are outside the guarantee.

Missing or malformed files, escaping or linked paths, other artifact types, and
unresolved local imports fail verification. Export patterns, `browser`,
`typesVersions`, and `directories.bin` remain unsupported rather than silently
losing a possible consumer entry. File, import, byte, and process limits bound
analysis. The reader does not execute repository code, build artifacts, or
install packages. Tests compare loader spellings and package layouts, including
self-imports, competing extensions, directory manifests, and entry mappings,
with Node execution or TypeScript compilation of an installed consumer,
including valid neighboring cases.

Repositories build before architecture verification and keep their ordinary
build and packed-consumer contracts. Reading a current artifact is not evidence
of build freshness or arbitrary JavaScript behavior. The check establishes the
declared dependency boundary of the parsed entry closure; it does not replace
those contracts. Verification is shared by native and provider imports and
cached only within one architecture analysis, without a new receipt or digest.
