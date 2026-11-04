# Bundled JavaScript dependencies

Source imports alone do not establish an install-time dependency. A library can
inline a package into its published JavaScript while retaining it only in
`devDependencies` for builds. Marking that library's source as development-only
would misdescribe the production code. npm's `bundleDependencies` describes
shipping dependency directories, not inlining, and supplies no such evidence.

`scope.bundledDependencies` names an exact package manifest, the package's
packed `.tgz` artifact, and exact dependency names. Scope wildcards are
intentionally unsupported: a newly added dependency must receive its own
declaration. Each name must occur only in that manifest's `devDependencies`;
runtime, optional, and peer declarations contradict the claim. The declaration
does not change source classification, dependency direction, quality checks,
supply-chain analysis, or other packages' dependency authority.

The declaration is the package owner's packaging fact, like the
`scope.development` classification of source. A valid declaration authorizes
the owning package's source imports of the named packages; that authority does
not depend on what verification finds. Verification looks for visible
contradictions in the packed artifact and reports them on the artifact. Whether
the shipped package works without those dependencies is established by the
package's own packed-consumer tests, not by Code Polishy.

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

Each entry keeps the kind its manifest field gives it; nothing infers a kind
from a file extension. Runtime entries are every exact `exports` target outside
a `types` condition and the `module` field; executable entries are `bin`
targets. Both join the runtime closure as exact packed files, as a policy
enumeration, because a consumer may select any of them. `main` and the
package's own name join through Node's entry selection: the files Node's
CommonJS and ESM resolvers select for the package from a consumer, and the file
Node selects for `main`, so extension search and directory `main` match Node.
Type entries are `types` and `typings` fields and `types` export conditions,
including TypeScript's versioned `types@<range>` conditions.
They go only to TypeScript, which resolves them with its own extension
substitution and directory rules; a declared type entry that no consumer
configuration resolves fails verification.

Runtime resolution belongs to Node. Runtime and type resolution stay distinct
because consumers select different files for them, and runtime references keep
the resolution mode of their syntax. The recognized `require` calls resolve with
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
  other resolver error, is unsupported and fails verification.

Entry targets containing `%` are unsupported because the policy enumeration
reads them as paths while Node decodes them as URLs. TypeScript sources cannot
run from an installed package, so reaching one at runtime fails verification.

The public declaration closure is what TypeScript itself builds. One sealed
TypeScript Program per consumer configuration (Node10, NodeNext ESM and
CommonJS, and Bundler) starts from a virtual consumer file importing every
runtime, `main`, and type entry and the package directory, so TypeScript
selects declaration companions, preferred TypeScript sources, and the root
`index.d.ts` fallback as consumers do. A controlled compiler
host serves only packed files, excluding nested `node_modules`, from the staged
installed package, so entry selection,
extension substitution, directory manifests, reference paths, and self-imports
resolve as they do for an installed consumer. Every file any Program loads joins
the closure. Each Program records its own outcome for every import of every
file it loads, and outcomes are combined only afterward, so success in one
configuration never cancels another configuration's requirement or failure: a
local result is traversed, an unresolved package import names that external
package and its `@types` counterpart, an unresolved built-in names
`@types/node`, and an unresolved relative import fails verification.
Private declaration files outside that closure do not impose consumer needs.
Extensionless files are parsed as JavaScript because Node executes them; only
JSON and CSS remain opaque. A remaining import of a declared bundled name is a
leak reported on the artifact.

## Scope of verification

Verification is static inspection of the shipped entry closure with the
existing parsers, Node's resolvers, and TypeScript's declaration traversal. An
empty result means that no dependency was statically detected; it is not a
proof that the published JavaScript cannot load a package at run time.

Runtime artifacts contribute static imports and re-exports, literal dynamic
imports, and calls with one string literal specifier of `require`,
`module.require`, or `require.resolve`, recognized by identifier and
dotted-property spelling once parentheses around the callee and its receiver
are removed. Those calls resolve in CommonJS mode and the others in ESM mode.
Declaration and TypeScript artifacts contribute imports, re-exports, import
types, and `reference` directives. TypeScript may satisfy a bare specifier with
the package itself or its `@types` counterpart, and a Node built-in with
`@types/node`, so each import in a TypeScript artifact counts against all
identities a consumer could need. Type-reference directives must name a type
package, and reference-path directives must name a relative file. URL,
absolute-path, and `#` import specifiers fail verification.

Loads that are not written as one of those forms are not detected: computed
specifiers, aliased or rebound loaders, `createRequire` and other Node module
APIs, reflection, runtime mutation, and evaluated code. Code Polishy does not
police them. A counterexample of that kind is a case for the package's own
consumer tests, not a new verification requirement. Ambient module declarations
supplied by other packages and consumer path mappings are likewise outside the
inspection.

Verification fails, with an `architecture.importCoverage` finding on the
artifact, when it cannot inspect the shipped closure: missing or malformed
archives, archives for another package, links or escaping paths in archives,
missing entries, unresolved local imports, artifacts other than JavaScript,
TypeScript, JSON, or CSS, invalid syntax, and the unsupported entry mappings
export patterns, `browser`, `typesVersions`, and `directories.bin`. File,
import, byte, and process limits bound analysis. The reader does not execute
repository code, build artifacts, or install packages.

## Packed-consumer contract

Behavioral validation belongs to the package's existing test suite. Build and
pack the package, install that exact archive into an isolated consumer without
development dependencies, exercise the supported public APIs, and compile
consumers in the TypeScript modes the package supports. Include a negative
fixture showing that an ordinary leaked dependency fails. These tests
demonstrate the scenarios they execute; they do not establish every possible
runtime behavior. Reading a packed archive is not evidence of build freshness.

Code Polishy's own tests cover packaging, resolution, and leak detection. Each
case is packed by the pinned package manager, verified from that archive, and
installed from the same archive for Node execution or for TypeScript
compilation under the four consumer configurations. Cases span the recognized
reference forms, specifier kinds including URL encodings whose meaning differs
between resolution modes, package layouts and nested scopes, publication
boundaries such as checkout-only `node_modules`, omitted symbolic links,
`files` and ignore-file exclusions, and manifest rewrites, and representative
tsc, esbuild, rollup, and webpack output. For a recognized form, the verifier
must report the dependency whenever the consumer needs it. A configuration
counts as needing a dependency when it fails without the dependency and
compiles with it; a case names any configuration that cannot consume the
package either way.

The feature is complete when declarations remain package-scoped, visible leaks
fail, these packed-consumer cases agree, and unrelated policy checks remain
intact. Verification is shared by native and provider imports and cached only
within one architecture analysis, without a new receipt or digest.
