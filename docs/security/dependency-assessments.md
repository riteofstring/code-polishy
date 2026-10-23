# Dependency vulnerability assessments

Brace expansion and URI processing resolved with aged fixes on 2026-10-15; other JavaScript and Go module findings
reviewed on 2026-10-08; Go toolchain admission reviewed on 2026-10-21.
The withdrawn glob advisory, Zod configuration validation, and the aged TOML fix
reviewed on 2026-10-23 UTC.
Assessment owner: Codex.

The vulnerability assessments cover `tools/javascript/pnpm-lock.yaml` and
`providers/javascript/pnpm-lock.yaml`, at the exact versions recorded in
`.code-polishy.json`. They establish non-exposure in the current sealed tooling
and JavaScript provider. They do not assert that the affected packages are free
of vulnerabilities or that other consumers are unaffected.

The Go module and selector-parser findings are resolved by aged official releases.
The retained dependencies have absent or blocked advisory prerequisites, so a
fresh fix would add supply-chain exposure without a demonstrated security benefit
here. Retain those aged versions until the dated updates below. The separate Go
toolchain admission addresses certificate verification during online dependency admission.

## Go toolchain

Admit official Go 1.26.10, replacing 1.26.9, under the exact
`go-1-26-10-x509-security-fix` release-age assessment. This admits the fixed
compiler and carried SDK; it does not suppress a vulnerability. Go's
[release history](https://go.dev/doc/devel/release#go1.26.10) dates it to
2026-10-19. With the validator's conservative end-of-day bound, it reaches
30 days on 2026-11-19 at 00:00:00 UTC. The admission expires at the end of
2026-11-18 UTC, covering only that waiting period.

### Exposure and the cost of waiting

[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809), also identified as
CVE-2026-111943, concerns compromised certificate intermediates accepted by
`crypto/x509.Certificate.Verify` when the host's trust store still trusts them.
The attacker needs a certificate chain valid for the requested registry hostname
using one of those intermediates, and control of, or an interception position
on, the TLS connection. Ordinary
repository content alone does not supply those prerequisites. Updated host
trust stores can already reject the affected chains; that state cannot be
assumed for every supported installation.

The enabled online supply-chain path calls `lookupReleaseGroup` in
[`release_age.go`](../../internal/supplychain/release_age.go) and
`fetchArtifactDocument` in
[`release_artifacts.go`](../../internal/supplychain/release_artifacts.go).
Both send HTTPS requests through `registryClient`, whose default transport uses
Go TLS and certificate verification. Registry and upstream responses determine
release dates and hence whether dependency or tool versions meet admission age.
Those mutable responses have no separately pinned digest. A peer satisfying the
advisory prerequisites can therefore forge dates and undermine the waiting
period. This is a security-decision input, not merely an imported vulnerable API.

The native release downloader also uses HTTPS, but its independently pinned
index and archive SHA-256 checks remain separate authentication boundaries.
This assessment does not claim that forged TLS alone bypasses those checks, or
that exploitation has been observed in Code Polishy.

Waiting would retain that metadata-authentication risk for about 29 more days.
Neither fixed Go release, 1.26.10 or 1.27.3, is aged. Requiring users to repair
all platform trust stores cannot enforce the boundary across existing Linux,
macOS, and Windows installations. Disabling online metadata checks prevents
ordinary dependency admission and release verification. A private certificate
denylist or toolchain backport duplicates security-sensitive upstream behavior.
The official patch within the existing Go minor line is the smallest practical
correction, and retains the earlier Windows rooted-filesystem containment fix.

### Provenance and fresh-code risk

The archives come from `https://go.dev/dl/`; all five supported host checksums
were checked against the official [download metadata](https://go.dev/dl/?mode=json).
`tools/go_checksums.txt` pins those exact archives, and
`tools/windows_tool_checksums.txt` pins the same Windows ZIP. Installation
verifies the archive before extraction and executes no dependency lifecycle
scripts. Project module versions and `go.sum` remain unchanged.

The inspected [official comparison](https://github.com/golang/go/compare/go1.26.9...go1.26.10)
ends at `4e8c85f5319d999bfd7617fdc191631e5634e3c5`.
The [certificate correction](https://github.com/golang/go/commit/aa64342a9e13affffea3e989d67e574475af5a78)
adds eight distrusted public-key hashes and filters chains in Go verification
and the macOS and Windows verification adapters. The remaining changes update
release metadata and restore the upstream module-dependency consistency test.
The standard library's `golang.org/x/net` metadata advances from
`v0.47.1-0.20260925170545-1431957440b7` to
`v0.47.1-0.20261008201845-026a91c07857`; the comparison changes its module records
without additional vendored implementation changes.

A newly published official patch can still contain defects or a compromised build.
The source delta and checksum provenance narrow that uncertainty but do not
eliminate it. Rebuilding Go analyzers, scanning the new compiler's standard
library, and running the ordinary full gate and native CI constrain regression
risk. The admission accepts the remaining fresh-code risk because waiting
retains a demonstrated path from compromised TLS authority to dependency
admission decisions, with no comparably small practical mitigation.

Remove this admission when the exact release reaches 30 days or leaves the
pins. Reassess immediately if upstream provenance changes or a regression or
compromise is reported. Do not extend it to another toolchain version or weaken
vulnerability scanning.

## Brace expansion

Both JavaScript graphs now resolve `brace-expansion` 1.1.21 and 5.0.12.
These are the smallest releases in the existing major lines that address all
three assessed parsing advisories:

| Advisory                                                                 | Vulnerable behavior                                              | Fixed 1.x / 5.x releases |
| ------------------------------------------------------------------------ | ---------------------------------------------------------------- | ------------------------ |
| [GHSA-6j4f-fj2g-mc7p](https://github.com/advisories/GHSA-6j4f-fj2g-mc7p) | Recursive comma parsing or argument expansion exhausts the stack | 1.1.19 / 5.0.10          |
| [GHSA-qhr7-859c-m2p7](https://github.com/advisories/GHSA-qhr7-859c-m2p7) | Nested brace expansion exhausts the stack                        | 1.1.20 / 5.0.11          |
| [GHSA-q2hr-2g5m-vwhr](https://github.com/advisories/GHSA-q2hr-2g5m-vwhr) | Brace rewrite repeatedly rescans growing input                   | 1.1.21 / 5.0.12          |

The [npm registry](https://registry.npmjs.org/brace-expansion) records 1.1.21
at 2026-09-14 21:59:28.332 UTC and 5.0.12 at 21:59:00.288 UTC. Both meet
the ordinary 30-day minimum. No release-age admission is needed. The 12 exact
assessments for 1.1.18 and 5.0.9 have been removed with those versions from
`tools/javascript/pnpm-lock.yaml` and `providers/javascript/pnpm-lock.yaml`.

The prior non-exposure conclusion still matches the inspected callers: native
ESLint uses policy-owned extension patterns, the provider uses `**/*.*`, JSX
rules receive fixed component patterns, and neither parser enables
`projectService.allowDefaultProject`. Repository filenames are match candidates,
not brace-expansion patterns. Knip's repository-derived glob patterns use the
separate fast-glob/picomatch path and retain their existing bounded-input checks.
The update closes the dependency findings now that an aged fix is available;
it does not remove these input boundaries.

Registry-integrity-verified old and new archives show iterative comma parsing,
element-wise appends, and bounded nesting/rewrite counts. Existing dependency
requirements remain unchanged; neither release adds an install lifecycle script.
The 5.x maintainer-only script list adds `format:check`. Candidate locks change
only the two brace-expansion resolutions, without overrides, new dependencies,
or direct-pin changes. Frozen installation keeps lifecycle execution disabled.
Official provenance and age reduce admission uncertainty without proving the
absence of upstream compromise or regressions. Ordinary analyzer and provider
contracts remain the compatibility evidence.

## URI processing

The JavaScript provider graph retains `fast-uri` 3.1.8. This official
release retains the malformed-authority and port-injection fixes and corrects
the older host-case advisory. Assessments for those three fixed advisories were
removed. The
[npm registry](https://registry.npmjs.org/fast-uri) records publication at
2026-09-15 07:36:25.444 UTC, so it meets the 30-day minimum. The lock changes
only this transitive dependency, without adding lifecycle scripts or overrides.

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

The integrity-verified archive delta normalizes percent-encoded host octets
before folding the decoded reg-name to lowercase, then restores uppercase
escape digits. It also moves host normalization outside scheme-specific path
normalization. Neither release declares dependencies or install lifecycle
scripts. The update follows the original dated remediation plan now that the
fix is aged; that update needed no early admission.

Four additional advisories were published on 2026-10-23 between 21:36 and
21:41 UTC. They affect 3.1.8 and are assessed separately as not affected because
their APIs are unreachable through the provider's enabled services:

| Advisory                                                                                   | Required application behavior                                                                                            | Observed severity |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------ | ----------------- |
| [GHSA-wjr9-j795-jxcr](https://github.com/advisories/GHSA-wjr9-j795-jxcr) / CVE-2026-110180 | Use fast-uri's canonicalized HTTP-family host after applying host policy to a different representation                   | Moderate          |
| [GHSA-333x-w4r6-6crp](https://github.com/advisories/GHSA-333x-w4r6-6crp) / CVE-2026-110725 | Serialize or normalize untrusted input with `reference: 'suffix'`, then use the result as a URI                          | Low               |
| [GHSA-w74x-v6f9-j3xg](https://github.com/advisories/GHSA-w74x-v6f9-j3xg) / CVE-2026-110306 | Screen a raw scheme before fast-uri decodes it, then render the result or use it for authorization                       | Moderate          |
| [GHSA-xhq7-xmqj-769h](https://github.com/advisories/GHSA-xhq7-xmqj-769h) / CVE-2026-110095 | Normalize relative references before resolution, or use the altered references as resource, cache, or authorization keys | Low               |

The review reproduced all four parser behaviors using the 3.1.8 archive
authenticated against the provider lock's integrity. Those results confirm the
vulnerable version range; the assessment rests on the enabled service boundary.
The same integrity
check bound the inspected Astro, Volar YAML service, YAML language server, and
Ajv archives to their exact resolved versions. The enabled provider imports
Astro's `dist/core/index.js`, `dist/plugins/typescript/index.js`, and
`dist/plugins/astro.js`; its checker receives TypeScript services plus Astro
diagnostics. Astro's separate `dist/languageServerPlugin.js` imports and enables
`dist/plugins/yaml.js`, which reaches `volar-service-yaml`,
`yaml-language-server`, Ajv 8.20.0, and `fast-uri`. That full plugin is not used.
Repository source and type declarations remain data for the selected services
and cannot enable this omitted service. The provider does not use fast-uri
for network authorization, executable URL generation, or resource identity.

A temporary build from the frozen provider lock corroborated the source trace.
Its fast-uri entry point was replaced with a load counter and an unconditional
failure. All 39 existing provider boundary tests passed with zero loads,
including valid and defective TypeScript and Astro cases. A direct import then
triggered the counter and failure, confirming the probe was active. The
temporary dependency was restored afterward. This exercises the existing
provider contract without changing production dependencies or test sources.

All four advisories are fixed in the official 3.1.9 release, published on
2026-10-19 at 23:05:26.357 UTC. It reaches the 30-day minimum on 2026-11-18 at
23:05:26.357 UTC. The alternative fixed lines, 2.4.8 and 4.2.3, were also
published on October 19 and provide no aged replacement. Introducing fresh
parser code now would add supply-chain and regression risk without removing a
reachable vulnerability from the enabled provider. Retain the aged 3.1.8
resolution with four exact assessments owned by Codex, reviewed October 23,
and expiring November 19. On November 19, review and admit 3.1.9 or a later
aged fixed candidate, generate the minimal lock change without scripts, run
dependency review, install frozen with scripts disabled, verify the provider
boundary and security profile, and remove these assessments.

Enabling Astro's full language server, the YAML service, an Ajv 8 consumer, or
another fast-uri caller invalidates this conclusion and requires a new exposure
review. Changed versions or advisory severity and known exploitation remain
subject to the ordinary blocking policy.

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
was withdrawn on 2026-10-23 at 12:53:02 UTC because upstream no longer considers
it a practically exploitable vulnerability. Both exact `braces` 3.0.3
assessments have been removed because current scanners no longer report it.
The existing input-depth boundary remains: the iterative parser can construct
deeply nested brace and parenthesis nodes that overflow recursive compilation
or expansion, even within the ordinary pattern-length limit.

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

Reinspection on 2026-10-23 UTC confirms the same Knip 5.55.1 and fast-glob 3.3.3
callers and the same guard binding in both analyzers. There is no active
vulnerability acceptance for this withdrawn advisory. Review the input boundary
when Knip, fast-glob, braces, the shared guard, or an expansion caller changes.
See the
[boundary design](../design/javascript-glob-inputs.md) for the binding scope.

## Zod configuration validation

[GHSA-mrhx-c7xc-m3w5](https://github.com/advisories/GHSA-mrhx-c7xc-m3w5),
also CVE-2023-54404, reports memory exhaustion from oversized arrays whose
elements fail Zod validation. Its reviewed package range was updated on
2026-10-23 and includes the `zod` 3.25.76 resolved in both JavaScript locks.
The resolved v3 `ZodArray._parse` visits every element and accumulates issues;
a bounded local check produced 512 issues for 512 invalid elements. Package
presence is real, and this assessment does not dispute the vulnerable parser.

Both graphs resolve Zod through Knip 5.55.1 and its error formatter,
`zod-validation-error` 3.5.4. Knip's `ConfigurationChief.init` calls
`knipConfigurationSchema.parse`; `schema/plugins.js` accepts path arrays through
`z.array(z.string())`. The error formatter consumes validation errors rather
than parsing a repository-supplied collection. The relevant authority boundary
is the configuration passed to that schema:

- The native analyzer's
  [`requireWorkspaces` and `configurationFor`](../../tools/javascript/deadcode.mjs)
  validate contained string paths and limit selected files to 4096. They build
  workspace `entry` and `project` arrays from those paths and set each plugin
  flag to `false`. `analyze` writes that configuration to its private
  `policy-knip.json` and passes its exact path with `--config`. Knip therefore
  parses the generated file instead of target `knip.json` or `package.json#knip`.
- The provider's
  [`configurationFor`](../../providers/javascript/deadcode.mjs) builds arrays of
  escaped path strings and fixed plugin flags from resolved source units.
  [`bindKnip`](../../providers/javascript/reachability.mjs) replaces configuration
  initialization and passes only that object plus its trusted Astro compiler
  to the schema. Target Knip settings are never merged into it. The compiler's
  Zod function wrapper validates two string arguments, not an arbitrary array.

Repository bytes can influence path-string contents, but cannot introduce
invalid element types into these arrays. Plugin schemas receive fixed booleans.
The advisory's unbounded accumulation of per-element validation failures is
therefore unreachable through either integration. A temporary native-boundary
check placed 512 invalid elements in each of `entry`, `project`, and `ignore`
in both target configuration sources; analysis still reported the expected
unused source file with complete coverage. Existing native and provider
boundary suites exercise actual dead-code analysis with the resolved packages.

Retain the aged 3.25.76 under exact high-severity `not-affected/unreachable`
assessments owned by Codex, reviewed on 2026-10-23 and expiring on 2026-11-06.
The [npm registry](https://registry.npmjs.org/zod) dates the fixed 4.6.6 to
2026-10-11 at 16:18:23.878 UTC; it reaches the conservative 30-day admission
boundary on 2026-11-11 UTC. Knip currently requires Zod `^3.22.4`, so forcing
Zod 4 into this graph would also cross its declared compatibility boundary.
Neither fresh-code admission nor a major-version override is justified by an
unreachable prerequisite. Check for a compatible official fix by 2026-11-06
and adopt an aged compatible dependency graph when available. Reassess before
expiry or immediately if a Zod consumer, generated configuration, input
validation, or Knip configuration-loading path changes. Known exploitation
and unknown severity remain blocking.

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

Both locks now resolve official 1.9.0 and the exact TOML assessments are removed.
The [npm registry](https://registry.npmjs.org/smol-toml) records publication at
2026-09-22 17:15:34.756 UTC, so it meets the 30-day minimum. Registry-integrity
verification and inspection of the old and new package sources confirm that
`parseKey` advances through the current key instead of repeatedly searching the
remaining document. Neither release adds dependencies or installation scripts.

Only the smol-toml resolution changes in each generated lock; direct dependencies
and Knip's compatible range remain unchanged. Frozen installation keeps scripts
disabled. The aged fix removes the advisory without an early admission or a
replacement vulnerability assessment. The existing policy-owned configuration
boundary remains in place; reassess future advisories if configuration loading
or plugin activation changes.

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
