# JavaScript dependency vulnerability assessments

Brace expansion reviewed on 2026-10-01; URI processing reviewed on 2026-10-08.
Assessment owner: Codex.

These assessments cover only `tools/javascript/pnpm-lock.yaml` and
`providers/javascript/pnpm-lock.yaml`, at the exact versions recorded in
`.code-polishy.json`. They establish non-exposure in the current sealed tooling
and JavaScript provider. They do not assert that the affected packages are free
of vulnerabilities or that other consumers are unaffected.

No early release admission is justified: the advisory prerequisites are absent,
so a fresh fix would add supply-chain exposure without a demonstrated security
benefit here. Retain the aged versions until the dated updates below.

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

## Unresolved advisories

The online review on 2026-10-08 also reports the following dependencies. They
have no vulnerability assessment in `.code-polishy.json` and remain release
blockers; the existing non-exposure decisions above do not cover them.

| Advisory                                                                 | Dependency                      | Lock scope           |
| ------------------------------------------------------------------------ | ------------------------------- | -------------------- |
| [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm) | `braces` 3.0.3                  | Tooling and provider |
| [GHSA-r4xh-jqrq-34v2](https://github.com/advisories/GHSA-r4xh-jqrq-34v2) | `smol-toml` 1.7.1               | Tooling and provider |
| [GHSA-68fv-2mgg-jv7q](https://github.com/advisories/GHSA-68fv-2mgg-jv7q) | `source-map-js` 1.2.1           | Provider             |
| [GHSA-rj75-hqrm-r3gf](https://github.com/advisories/GHSA-rj75-hqrm-r3gf) | `postcss-selector-parser` 7.1.5 | Provider             |
| [GO-2026-6629](https://pkg.go.dev/vuln/GO-2026-6629) / CVE-2026-56851    | `golang.org/x/text` 0.39.0      | `go.mod`             |

The `braces` advisory has no published fixed release. The provider's
`resolveGlob` in [`imports.mjs`](../../providers/javascript/imports.mjs) passes
source-selected glob patterns to `fast-glob` 3.3.3 with brace expansion enabled;
its task generator reaches `micromatch` 4.0.8 and `braces` 3.0.3. An unreachable
assessment is not justified for that path. Remediation needs an input-boundary
fix that preserves supported glob behavior and reports unsupported input, with
regression coverage for valid patterns and deeply nested braces.

For the selector parser, the official 7.1.6 fix was published on 2026-09-03 at
08:59:01.963 UTC and now meets the age minimum. The official Go fix is
`golang.org/x/text` 0.41.0, published on 2026-08-11 at 15:22:47 UTC according
to the [Go module proxy](https://proxy.golang.org/golang.org/x/text/@v/v0.41.0.info).
OSV reports the Go advisory with unknown severity, which is not assessable.
Review these exact dependency updates before installation.

`smol-toml` 1.9.0 and `source-map-js` 1.2.2 become eligible on 2026-10-22 at
17:15:35 UTC and 2026-10-30 at 14:08:10 UTC respectively, using their npm
publication timestamps. Investigate their enabled callers and advisory
prerequisites before choosing remediation or an exact assessment. No
non-exposure or early-admission decision has been made for either package.

## Verification and limits

The analysis uses the current repository call sites, exact resolved package
source, the linked upstream advisories, and publication timestamps from the
npm registry. Native audit and OSV must continue to report the matching
advisories; policy records them as assessed findings. Scan success alone does
not establish reachability. Review this document whenever the relevant callers,
configuration, or resolved versions change.
