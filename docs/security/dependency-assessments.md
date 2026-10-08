# Dependency vulnerability assessments

Brace expansion reviewed on 2026-10-01; remaining findings reviewed on 2026-10-08.
Assessment owner: Codex.

The vulnerability assessments cover `tools/javascript/pnpm-lock.yaml` and
`providers/javascript/pnpm-lock.yaml`, at the exact versions recorded in
`.code-polishy.json`. They establish non-exposure in the current sealed tooling
and JavaScript provider. They do not assert that the affected packages are free
of vulnerabilities or that other consumers are unaffected.

The Go and selector-parser findings are resolved by aged official releases.
The retained dependencies have absent or blocked advisory prerequisites, so a
fresh fix would add supply-chain exposure without a demonstrated security benefit
here. No early release admission is needed. Retain those aged versions until the
dated updates below.

## Brace expansion

Retain `brace-expansion` 1.1.18 and 5.0.9 in both graphs. The advisories require
an attacker-controlled string to reach the brace expansion pattern argument:

| Advisory                                                                 | Vulnerable behavior                                                    | Fixed 1.x / 5.x releases |
| ------------------------------------------------------------------------ | ---------------------------------------------------------------------- | ------------------------ |
| [GHSA-6j4f-fj2g-mc7p](https://github.com/advisories/GHSA-6j4f-fj2g-mc7p) | `parseCommaParts()` recursion or argument expansion exhausts the stack | 1.1.19 / 5.0.10          |
| [GHSA-qhr7-859c-m2p7](https://github.com/advisories/GHSA-qhr7-859c-m2p7) | Deeply nested brace groups exhaust `expand_()`'s stack                 | 1.1.20 / 5.0.11          |
| [GHSA-q2hr-2g5m-vwhr](https://github.com/advisories/GHSA-q2hr-2g5m-vwhr) | The `{a},b}` rewrite consumes quadratic CPU time                       | 1.1.21 / 5.0.12          |

All three exploit the pattern, not the filename being matched. Existing
expansion-result limits do not prevent these parsing attacks; the assessment
rests on input authority and disabled call paths instead.

### Version 1.1.18

ESLint 9.39.5 and `@eslint/config-array` 0.21.2 reach `minimatch` 3.1.5,
which depends on this version. The tooling's `lintConfiguration` in
[`runner.mjs`](../../tools/javascript/runner.mjs) generates its file pattern
from a policy-owned extension allowlist. The provider's `lintMessages` in
[`analysis.mjs`](../../providers/javascript/analysis.mjs) uses `**/*.*`.
Repository-controlled source and paths are candidate text and filenames,
not configuration patterns. Both configurations disable inline configuration.

`eslint-plugin-jsx-a11y` 6.10.2 also uses `minimatch` through
`mayContainChildComponent`. The enabled label rule supplies fixed component
names; [`policy.mjs`](../../tools/javascript/policy.mjs) does not accept
repository-selected component patterns or rule options. A JSX component name
from source is the match candidate, not the pattern.

Knip 5.55.1 accepts workspace patterns but its `ConfigurationChief` and glob
utilities use `picomatch` and `fast-glob`. That path does not establish a call
to `brace-expansion`. Neither sealed analyzer loads target ESLint or Knip
configuration.

### Version 5.0.9

`@typescript-eslint/typescript-estree` 8.64.0 depends on `minimatch` 10.2.x,
which resolves this brace-expansion version. Its
`useProgramFromProjectService.js` calls `minimatch` from
`filePathMatchedBy` only for `allowDefaultProject` patterns. Both analyzer
configurations above omit `projectService` and `allowDefaultProject`.
Importing the parser can load the matcher; it does not enable this call path.

### Remediation

Update to 1.1.21 and 5.0.12 on or after 2026-10-14 at 21:59:29 UTC, when
both exact releases have aged for 30 days. The assessments expire at the end
of that UTC date. Reassess immediately if analyzer configuration becomes
repository-controlled, a rule starts accepting component patterns, or a
parser project service is enabled. Remove the exact assessments when their
affected versions leave the locks.

## URI processing

The JavaScript provider graph now resolves `fast-uri` 3.1.7. This official
release fixes the malformed-authority and port-injection advisories below;
their exact 3.1.6 assessments have been removed. The
[npm registry](https://registry.npmjs.org/fast-uri) records publication at
2026-09-02 11:06:41.962 UTC, so it meets the 30-day minimum. The lock changes
only this transitive dependency, without adding lifecycle scripts or overrides.
The remaining host-case advisory is assessed against 3.1.7.

| Advisory                                                                 | Required application behavior                                                                                                          | Fixed release |
| ------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- | ------------- |
| [GHSA-58mr-gqgx-xq4g](https://github.com/advisories/GHSA-58mr-gqgx-xq4g) | Use a malformed authority's parsed host for a security decision, then send the original URL to a client that resolves a different host | 3.1.7         |
| [GHSA-qw65-cvwx-89v3](https://github.com/advisories/GHSA-qw65-cvwx-89v3) | Supply an untrusted port component to URI serialization or its object-form callers                                                     | 3.1.7         |
| [GHSA-hrr3-gc8f-f4qj](https://github.com/advisories/GHSA-hrr3-gc8f-f4qj) | Make a case-sensitive host decision using scheme-relative input with encoded uppercase octets                                          | 3.1.8         |

The resolved dependency path is `@astrojs/language-server` 2.16.11 →
`volar-service-yaml` 0.0.71 → `yaml-language-server` 1.23.0 → Ajv 8.20.0 →
`fast-uri`. Astro's `dist/plugins/yaml.js` registers the YAML service through
its full language-server plugin. The provider's
[`typecheck.mjs`](../../providers/javascript/typecheck.mjs) imports only
Astro's core, TypeScript service, and Astro service entry points, then constructs
its checker from those services. It does not import the full language-server
plugin or the YAML service. The affected URI APIs therefore receive no input
from this provider. The provider also does not use `fast-uri` to authorize
network destinations. Reinspection of the integrity-verified resolved package
sources confirms that Astro's `dist/languageServerPlugin.js` registers the YAML
service, which reaches Ajv through `yamlSchemaService.js`; Ajv's
`dist/runtime/uri.js` imports `fast-uri`. None of those services is enabled by
the provider's checker construction.

### Remediation

Retain the 3.1.7 host-case assessment with its existing 2026-10-15 expiry.
The official 3.1.8 fix was published on 2026-09-15 at 07:36:25.444 UTC and is
still under 30 days old at this review. With no enabled call to the affected
API or host authorization decision, waiting does not expose this provider to
the advisory; admitting the fresh release would add avoidable supply-chain
risk. Update to 3.1.8 on or after 2026-10-15 at 07:36:26 UTC and remove the
remaining assessment, which expires at the end of that UTC date. Reassess
immediately if the YAML service, full Astro language server, or a new URI
consumer is enabled.

## Aged security updates

The provider lock resolves `postcss-selector-parser` 7.1.6 for
[GHSA-rj75-hqrm-r3gf](https://github.com/advisories/GHSA-rj75-hqrm-r3gf).
The fix replaces quadratic token-index work on long flat selectors. The
[npm registry](https://registry.npmjs.org/postcss-selector-parser) records its
publication on 2026-09-03 at 08:59:01.963 UTC. Only that transitive package changes;
its dependency edges are unchanged. No override or lifecycle script is introduced.

`golang.org/x/text` is pinned to 0.41.0 for
[GO-2026-6629](https://pkg.go.dev/vuln/GO-2026-6629) / CVE-2026-56851, which
affects crafted input to the `secure/precis` Nickname profile. Code Polishy's own
imports use `cases` and `unicode/norm`, but OSV's unknown-severity finding is
not eligible for an assessment. The aged official fix removes that finding.
The [Go module proxy](https://proxy.golang.org/golang.org/x/text/@v/v0.41.0.info)
records publication on 2026-08-11 at 15:22:47 UTC from `go.googlesource.com/text`.
Its module graph requires `golang.org/x/sync` 0.22.0, so that indirect pin also
changes. The [sync release](https://proxy.golang.org/golang.org/x/sync/@v/v0.22.0.info)
was published on 2026-07-01 at 17:29:34 UTC from `go.googlesource.com/sync`.
Both satisfy the age minimum; module checksums authenticate the downloads.

## Guarded glob expansion

[GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)
affects `braces` 3.0.3 in both graphs. Its iterative parser accepts deeply nested
brace and parenthesis nodes that overflow recursive compilation or expansion.
The ordinary pattern-length limit does not stop the attack. There is no published
fixed release at this review.

The provider's `resolveGlob` in
[`imports.mjs`](../../providers/javascript/imports.mjs) accepts source-selected
`import.meta.glob` patterns. Both analyzers also use Knip 5.55.1, whose workspace
discovery and package-script entries reach `fast-glob` 3.3.3. Its task generator
calls `micromatch` 4.0.8 brace expansion, which invokes the affected `braces` API.
These are reachable input paths; disabled plugins alone do not protect them.

[`glob-inputs.mjs`](../../tools/javascript/glob-inputs.mjs) now rejects more than
64 nested brace/parenthesis groups before expansion. Its linear scan respects
escaped characters, quotes, character classes, and matched closing delimiters.
The native `bindInheritedWorkspaces` and provider adapter bind Knip's own
`fast-glob` instance for the analysis, checking patterns and ignores at `glob`,
`sync`, and `globStream`. The provider's exact direct fast-glob dependency resolves
to that same instance, so its source-glob calls are guarded too. Inspection of
Knip's `util/map-workspaces.js`, `util/glob.js`, and `util/glob-core.js` confirms
these are its expansion entry points, including entries extracted from scripts
even when plugins are disabled. Methods are restored during cleanup.

The advisory's excessive-depth payload therefore cannot reach the recursive
walkers. Normal matching still uses the original library and pattern. Rejection
produces a native operational failure or provider incomplete coverage, never a
passing empty result. Regressions exercise real source globs, workspace globs,
package scripts, ordinary nested alternatives, and escaped literal paths.

Retain exact high-severity `not-affected` assessments on the basis that the
exploit prerequisite is now unreachable. They expire on 2026-10-22. Check for an
official fix by that date and reassess before renewal; adopt an aged fix when
available. Reassess immediately if Knip, fast-glob, braces, the shared guard,
or any expansion caller changes. See the
[boundary design](../design/javascript-glob-inputs.md) for the binding scope.

## TOML parsing

[GHSA-r4xh-jqrq-34v2](https://github.com/advisories/GHSA-r4xh-jqrq-34v2)
affects `smol-toml` 1.7.1 through Knip in both graphs. Crafted TOML keys make
`parseKey` repeatedly scan the remaining input, consuming quadratic CPU time.
The prerequisite is repository bytes reaching `smol-toml.parse`.

Knip's `util/fs.js` calls that API only through `loadTOML`; `util/loader.js`
selects it for a loaded `.toml` configuration. The native analyzer always passes
its generated JSON configuration explicitly. Its workspace manifest reads are
JSON, and optional `pnpm-workspace.yaml` reads use the YAML loader. The provider
replaces configuration initialization with its own in-memory policy and reads
package manifests as JSON. Both explicitly disable every Knip plugin.
`WorkspaceWorker.runPlugins` loads plugin configuration only for enabled plugins;
script-referenced configuration for a disabled plugin becomes an entry path,
not a call to its configuration loader. TOML parsing receives no target input.

Retain exact moderate-severity `not-affected` assessments in both locks until
2026-10-22. Update to official 1.9.0 on or after 17:15:35 UTC that day, 30 days
after its [npm publication](https://registry.npmjs.org/smol-toml), and remove the
assessments. With no enabled parse call, waiting avoids admitting fresh parser
code without exposing the analyzer to this advisory. Reassess if configuration
loading or plugin activation changes.

## Source maps

[GHSA-68fv-2mgg-jv7q](https://github.com/advisories/GHSA-68fv-2mgg-jv7q)
affects provider `source-map-js` 1.2.1. Huge line offsets in an indexed source map
cause excessive synchronous work in `SourceNode.fromStringWithSourceMap`.
The resolved path is `eslint-plugin-astro` 1.3.1 → PostCSS 8.5.26 → source-map-js.

In the resolved Astro plugin, PostCSS processing and parsing occur inside the
`no-unused-css-selector` rule's style transformation and selector analysis.
[`analysis.mjs`](../../providers/javascript/analysis.mjs) enables only the
policy-owned JSX accessibility variants and `astro/no-conflict-set-directives`;
it does not enable `astro/no-unused-css-selector`. It also disables inline
configuration and does not load target ESLint configuration. Loading the plugin
loads PostCSS, but does not execute that rule or deliver maps to the affected API.
The provider's CSS comment scanner uses `vscode-css-languageservice`, and its
compiler mappings use `@jridgewell/trace-mapping`, not source-map-js.

Retain an exact high-severity `not-affected` assessment until 2026-10-30. Update
to official 1.2.2 on or after 14:08:10 UTC that day, 30 days after its
[npm publication](https://registry.npmjs.org/source-map-js), and remove the
assessment. The disabled call path makes waiting preferable to fresh-code
admission. Reassess immediately if CSS rule activation, map processing, or
configuration authority changes.

## Verification and limits

The analysis uses the current repository call sites, exact resolved package
source, the linked upstream advisories, and publication timestamps from the
npm registry. Native audit and OSV must continue to report the matching
advisories; policy records them as assessed findings. Scan success alone does
not establish reachability. Review this document whenever the relevant callers,
configuration, or resolved versions change.
