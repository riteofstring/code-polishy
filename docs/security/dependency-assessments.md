# JavaScript dependency vulnerability assessments

Reviewed on 2026-10-01. Assessment owner: Codex.

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

Retain `fast-uri` 3.1.6 in the JavaScript provider graph.

| Advisory                                                                 | Required application behavior                                                                                                          | Fixed release |
| ------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------- | ------------- |
| [GHSA-58mr-gqgx-xq4g](https://github.com/advisories/GHSA-58mr-gqgx-xq4g) | Use a malformed authority's parsed host for a security decision, then send the original URL to a client that resolves a different host | 3.1.7         |
| [GHSA-qw65-cvwx-89v3](https://github.com/advisories/GHSA-qw65-cvwx-89v3) | Supply an untrusted port component to URI serialization or its object-form callers                                                     | 3.1.7         |
| [GHSA-hrr3-gc8f-f4qj](https://github.com/advisories/GHSA-hrr3-gc8f-f4qj) | Make a case-sensitive host decision using scheme-relative input with encoded uppercase octets                                          | 3.1.8         |

The resolved dependency path is `@astrojs/language-server` 2.16.11 →
`volar-service-yaml` → `yaml-language-server` 1.23.0 → Ajv 8.20.0 →
`fast-uri`. Astro's `dist/plugins/yaml.js` registers the YAML service through
its full language-server plugin. The provider's
[`typecheck.mjs`](../../providers/javascript/typecheck.mjs) imports only
Astro's core, TypeScript service, and Astro service entry points, then constructs
its checker from those services. It does not import the full language-server
plugin or the YAML service. The affected URI APIs therefore receive no input
from this provider. The provider also does not use `fast-uri` to authorize
network destinations.

### Remediation

Update to 3.1.7 on or after 2026-10-02 at 11:06:42 UTC and remove the two
assessments for advisories fixed by that release. Reassess the remaining
host-case advisory against the exact new version. Update to 3.1.8 on or after
2026-10-15 at 07:36:26 UTC and remove the remaining assessment. Each current
assessment expires at the end of the corresponding UTC date. Reassess
immediately if the YAML service, full Astro language server, or a new URI
consumer is enabled.

## Verification and limits

The analysis uses the current repository call sites, exact resolved package
source, the linked upstream advisories, and publication timestamps from the
npm registry. Native audit and OSV must continue to report the matching
advisories; policy records them as assessed findings. Scan success alone does
not establish reachability. Review this document whenever the relevant callers,
configuration, or resolved versions change.
