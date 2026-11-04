# Bundled JavaScript dependencies

Source imports alone do not establish an install-time dependency. A library can
inline a package into its published JavaScript while retaining it only in
`devDependencies` for builds. Marking that library's source as development-only
would misdescribe the production code. npm's `bundleDependencies` describes
shipping dependency directories, not inlining, and supplies no such evidence.

`scope.bundledDependencies` names an exact package manifest, the package's
packed `.tgz` artifact, and exact dependency names. Scope wildcards are intentionally unsupported: a newly added dependency
must receive its own declaration. Each name must occur only in that manifest's
`devDependencies`; runtime, optional, and peer declarations contradict the
claim. The declaration does not change source classification, dependency
direction, quality checks, supply-chain analysis, or other packages' dependency
authority.

The claim concerns the published package, and a development checkout contains
files that never ship: installed dependencies, symbolic links, ignored files,
and files outside `files`. The package manager's own pack output is therefore
the only filesystem verification reads; Code Polishy does not reimplement
packing rules. The normal build and pack workflow produces the declared archive
before verification; Code Polishy never builds, packs, installs, or runs
lifecycle scripts. The archive must be a bounded gzip tarball whose entries are
regular files or directories under `package/` with contained, unique paths, and
its packed manifest must name the declared package. It is extracted as an
installed package, at `node_modules/<name>` in a temporary directory, and both
the runtime and declaration resolvers operate only there, so a file present only in the checkout cannot satisfy an artifact
import. The source manifest still decides whether each name is a development
dependency; the packed manifest, including any packing rewrite, decides the
entries.

The sealed TypeScript parser reads the packed entry files. Every exact `exports`
target under every condition and every explicit `module`, `types`, `typings`,
and `bin` entry joins as a policy enumeration, because a consumer may select any
of them. `main` and the package's own name join through Node's entry selection:
the files Node's CommonJS and ESM resolvers select for the package from a
consumer, and the file Node selects for `main`, so extension search and
directory `main` match Node. Public type entries are the files TypeScript's
resolvers select for each library entry and for the package directory, so
preferred TypeScript sources, declaration companions, extensionless entries, and
the root `index.d.ts` fallback match consumers.

Runtime resolution belongs to Node. Runtime and type resolution stay distinct
because consumers select different files for them, and runtime references keep
the resolution mode of their syntax. CommonJS loader calls resolve with
`createRequire(importer).resolve`; static imports, re-exports, and dynamic
imports, including dynamic imports in CommonJS files, resolve with
`import.meta.resolve(specifier, importer)`, which the pinned Node provides
under `--experimental-import-meta-resolve`. The sealed launcher passes exactly
that option and the runner refuses any other launch, because without it Node
ignores the importer and would resolve from the runner. Both resolvers read
package metadata without executing code and run from the importing file inside
the staged package, so extension search, directory manifests, URL decoding,
package scopes, nested manifests, self-references, and conditional exports
follow Node. ESM resolution does not establish existence, so every resolved
target must be an existing regular file. Each reference then has one outcome:

- A target inside the packed package is a local artifact and is traversed.
- A Node built-in names no package.
- A bare name other than the package's own that Node cannot find, or that
  resolves outside the staged package, is an external package dependency.
  A not-found error that Node attributes to a manifest inside the staged
  package, such as a nested scope whose export target is missing, is a broken
  local reference, not an external one.
- Anything else, including any failed relative or own-name reference and any
  other resolver error, is unsupported and blocks the allowance.

Entry targets containing `%` are unsupported because the policy enumeration
reads them as paths while Node decodes them as URLs. TypeScript sources cannot
run from an installed package, so reaching one at runtime fails verification.

The public declaration closure is what TypeScript itself builds. One sealed
TypeScript Program per consumer resolution mode (Node10, NodeNext ESM and
CommonJS, and Bundler) starts from the type entries and a virtual consumer file
importing every library entry and the package directory. A controlled compiler
host serves only packed files, excluding nested `node_modules`, from the staged
installed package, so entry selection,
extension substitution, directory manifests, reference paths, and self-imports
resolve as they do for an installed consumer. Every file any Program loads joins
the closure; an unresolved relative import fails verification, and an
unresolved package import names that external package.
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

Every reference keeps its category, resolution origin, and specifier kind
through resolution and ends in exactly one outcome: a resolved local artifact,
an identified external package, or an unsupported result. The categories are
CommonJS loader calls, ESM static and dynamic imports, TypeScript module
imports, type-reference directives, and reference-path directives.

- Module references resolve from their own file with the resolver of their
  category: Node's CommonJS resolver for loader calls, Node's ESM resolver for
  runtime imports, and TypeScript for type artifacts. Relative and package
  references both go through that resolver before any package reference counts
  as external; Node built-ins name no package, except
  `@types/node` and the matching package identities for TypeScript artifacts.
  URL, absolute-path, `#` import, and other specifiers are unsupported.
- Type-reference directives must name a type package, which counts with its
  `@types` counterpart. They never resolve as module paths.
- Reference-path directives must name a relative file.

Runtime artifacts support static imports and re-exports and literal dynamic
imports. Every occurrence of the loader names `require` and `getBuiltinModule`,
as an identifier or a string literal, is classified into one category,
regardless of binding. The sealed typescript-eslint parser and its scope
manager decide whether an identifier is a value read, a binding write, or only a
name, so declarations, defaults, and shorthand properties follow the language
rather than a local table. Parentheses do not survive parsing, optional chains
are unwrapped, and a computed property name with a literal is a key like any
other.

- Loader calls: a call of `require`, `require.resolve`, `module.require`, or a
  `getBuiltinModule` binding or member, named by an identifier or literal key,
  with a string literal specifier. Resolution origin comes from bindings, not
  spelling: `require` calls and `require.resolve` must use the free `require`
  binding, and `module.require` the free `module` binding. Locally bound or
  shadowed loaders, other receivers such as `module.parent`, assignments to the
  free `require` or `module`, and `with` scopes have unknown origins and fail. `getBuiltinModule` must name
  a Node built-in. Runtime imports of `module` or `node:module` fail, which
  covers `createRequire` and the Module class. A non-literal specifier fails.
- Ordinary uses: declarations, labels, bindings that reuse the loader name, such
  as `import { getBuiltinModule }`, object
  literal keys, `typeof`, `require.main` and `require.cache` reads, type
  positions, and string values that are not keys.
- Unsupported uses: every other occurrence, including aliases, `call`, `apply`,
  or construction, member reads that are not called, renamed destructuring and
  assignment-pattern keys, shorthand property values, and import or export
  specifier names that rename a loader. These fail verification.

Keys produced by any other expression, such as concatenation or comma and
conditional expressions, are computed at run time and, with reflection APIs,
reflection through `constructor` or prototypes, and code evaluated from strings,
are outside the guarantee. Some harmless code is rejected conservatively, such
as an unused alias or a value exported under a loader name.

Declaration and TypeScript artifacts support imports, re-exports, import types,
and `reference` directives. TypeScript may satisfy a bare specifier with the
package itself or its `@types` counterpart, and a Node built-in with
`@types/node`, so each import in a TypeScript artifact counts against all
identities the consumer could need. Ambient module declarations supplied by
other packages and consumer path mappings are outside the guarantee.

Missing or malformed archives and files, other artifact types, and unresolved
local imports fail verification. Export patterns, `browser`,
`typesVersions`, and `directories.bin` remain unsupported rather than silently
losing a possible consumer entry. File, import, byte, and process limits bound
analysis. The reader does not execute repository code, build artifacts, or
install packages. Tests cover a matrix of reference forms, resolution origins,
and specifier kinds, including URL encodings whose meaning differs between
resolution modes, each loader category, wrapper combinations, package
layouts, publication boundaries such as checkout-only `node_modules`, omitted
symbolic links, `files` and ignore-file exclusions, and manifest rewrites, and
representative tsc, esbuild, rollup, and webpack output. Each case is packed by
the pinned package manager, verified from that archive, and installed from the
same archive for Node execution or TypeScript compilation, and state the
expected verifier outcome for each case, including conservative rejections.

The feature is complete when every documented category agrees with consumers in
those tests. A finding must name the documented promise it violates; support
for JavaScript outside the model is a separate feature decision.

Repositories build and pack before architecture verification and keep their
ordinary build and packed-consumer contracts. Reading a packed archive is not
evidence of build freshness or arbitrary JavaScript behavior. The check
establishes the declared dependency boundary of the shipped entry closure; it
does not replace those contracts. Verification is shared by native and provider imports and
cached only within one architecture analysis, without a new receipt or digest.
