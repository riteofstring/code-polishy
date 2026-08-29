# Supply-chain Policy

Dependency policy reduces accidental version drift, newly published-package
compromise, known vulnerabilities, and unreviewed code execution during setup
or CI.

## Immutable inputs

- Pin every direct registry dependency exactly.
- Treat Node `peerDependencies` as compatibility declarations, but test each
  peer through a matching exact `devDependency`.
- Pin the owning package manager with `packageManager` and commit its lockfile.
- Pin Git dependencies to full commits, GitHub Actions to full 40-character
  commits, and container images/actions to full SHA-256 digests.
- Require one exact installed Code Polishy release in `.code-polishy.lock.json`.
- Never download a moving policy, schema, scanner, or dependency branch in a
  gate.

The baseline allows `workspace:`, `file:`, and `link:` only for contained local
dependencies. Targets may remove protocols they do not need. Local paths and
symlinks must remain inside the target repository.

## Ecosystem coverage

The built-in engine currently provides:

- Node exact direct versions, exact package-manager pins, nearest-ancestor
  lockfile ownership, peer/dev consistency, pnpm lifecycle declarations,
  resolved `package-lock.json`/`pnpm-lock.yaml` release age, package-manager
  release age, and the pnpm native vulnerability audit;
- built-in pnpm lock consistency and resolved-source policy, read from
  `pnpm-lock.yaml` by the sealed JavaScript bundle (see below);
- built-in pnpm dependency licensing, read from the installed dependency tree by
  the same bundle and decided against the target's own allowed licenses;
- Go module/sum ownership, exact module versions, contained local replacements,
  `go mod tidy -diff`, module release age, and pinned `govulncheck`;
- Python PEP 621 runtime, optional, and dependency-group exact direct pins,
  `uv.lock` presence, and resolved `uv.lock` PyPI release age;
- standalone executable release age from exact checked-in version pins and
  fixed upstream metadata protocols for Go, Node, Go modules, npm, and GitHub
  Releases;
- conditional policy-owned OSV-Scanner coverage with structured exact findings
  for every supported dependency graph, in addition to native ecosystem
  vulnerability checks;
- full GitHub Action commit pins and container image digests;
- first-class Trivy/OpenVEX container-artifact scans with normalized findings,
  CycloneDX SBOMs, immutable evidence hashes, and hardened offline execution.

A pnpm project needs no lock-consistency provider: Code Polishy reads its
lockfile with the sealed JavaScript bundle and owns the answer itself. Every
other Node package manager and Python still require a target-native frozen
lock-consistency provider.
A pnpm project needs no license provider either, once it declares
`supplyChain.allowedLicenses`: the same bundle reads what its installed
dependencies declare. License coverage is host-honest: it reads the metadata a
normal frozen, script-disabled install materializes for the current host rather
than forcing foreign-platform optional packages into that tree.
The automatically activated OSV module supplies a repository-wide security
provider for supported dependency graphs. Yarn, Bun, private registries,
non-uv Python, licenses outside a pnpm project, repository-wide secrets,
provenance, and filesystem
scanning outside declared container artifacts require target-native providers
unless a later policy version adds a conditional module. Unsupported package
ecosystems must
provide `dependency-policy`, `lock-sync`, `release-age`, and `security` checks.
Unsupported coverage fails explicitly; it never becomes a silent pass.

If the engine does not recognize the ecosystem's manifest filename, declare
`custom-dependencies` on its owning module (or on `project.capabilities` for a
repository-wide toolchain). The same four-provider contract is then enforced
without requiring a core-engine change.

Every discovered Dockerfile or Containerfile requires either a first-class
`supplyChain.artifactSecurity.targets` declaration or a security provider
covering its module. Once first-class artifact targets are declared, a generic
provider is not accepted as proof that those artifacts were scanned. Literal base images must
use full SHA-256 digests; dynamic `ARG`/environment base-image substitution is
rejected because the resolved image cannot be proven from the checked-in file.

For package managers whose lock consistency or transitive release-age behavior
is not built in, add commands such as frozen lockfile validation and a
lock-aware release-age scanner. Local dependency policy and lock consistency
use `runOn: ["supply-chain"]`; registry freshness uses
`runOn: ["supply-chain-online"]`; vulnerability checks use
`runOn: ["security"]`.

## pnpm workspace and lock facts

For a directory that owns a `pnpm-lock.yaml`, Code Polishy reads that lock and
the manifests it resolves through the sealed JavaScript bundle, which parses it
with the same YAML parser pnpm does. The lock is also the workspace inventory:
pnpm writes one importer per workspace package.

- Lock consistency is built in, so a pnpm project declares no `lock-sync`
  provider. A `supplyChain.lockConsistency` finding names a dependency whose
  manifest declaration and lock specifier disagree, one the lock never resolved,
  and one the lock resolves that no manifest declares. `peerDependencies` are
  excluded because pnpm resolves no peer declaration into the lock; they are
  checked against exact `devDependencies` instead.
- `supplyChain.dependencySource` refuses every resolved package that is not a
  published registry release. A local directory is admitted only when
  `supplyChain.allowedDependencyProtocols` still allows `file:` or `link:`; a
  Git revision, a downloaded tarball, and a resolution the format does not
  describe are refused outright. The source is the one the lock names, not the
  fields it carries: a tarball URL stays a tarball when pnpm also records
  integrity over the downloaded bytes, because that integrity says they arrived
  unchanged and not that a registry published them.
- `supplyChain.lockCoverage` names a lock the reader could not read: another
  lockfile version, malformed YAML, an importer outside the repository, or a
  resolved key with no version. Missing coverage fails closed rather than
  reading as a project with nothing to resolve.
- Only exact registry releases reach the release-age, native-audit, OSV, and
  dependency-review lanes, so any other source is refused by name rather than
  aged or scanned as though it were published.

## Dependency licensing

A lockfile records what a target installs, never what those packages may be used
under, so the sealed bundle reads the license each installed release declares in
its own manifest and Go decides it. Reading is metadata only: no target code,
install script, or executable configuration runs, and no registry is contacted.

- `supplyChain.allowedLicenses` is the policy, as SPDX identifiers optionally
  qualified as `<license> WITH <exception>`. A repository that lists none
  declares no license policy, and none is enforced for it.
- `supplyChain.dependencyLicense` names a resolved release whose declared
  expression the policy does not admit, including one that declares no license
  and one written as something other than a readable SPDX expression. `OR`
  offers a choice, so allowing either side admits it; `AND` binds the target to
  both, so both must be allowed. A `WITH` exception changes what a license
  permits, so the qualified pair is what the policy has to allow.
- `supplyChain.licenseCoverage` names a resolved release the installed tree
  declares no metadata for, and a tree the reader could not read at all. The
  lane therefore requires the target's dependencies to be installed with its
  frozen package manager and lifecycle scripts disabled. A universal release,
  a host-compatible optional path, a release with any required lock context, or
  undecidable platform reachability still fails closed when its metadata is
  absent. A release is exempt from this one missing-metadata finding only when
  every exact lock context is optional and every path to it is excluded on the
  policy-owned Node host. An optional path may be excluded by the release's own
  valid `os`, `cpu`, or `libc` selectors or by an excluded optional ancestor;
  this covers universal transitive helpers that pnpm omits with their
  foreign-platform parent. Any shared compatible or required path still makes
  the release's installed metadata mandatory.
- Installed metadata is always enforced, including when a foreign-platform
  optional release happens to be materialized. Do not use `--force` merely to
  satisfy Code Polishy license coverage: that would not prove ordinary
  installation behavior. Complete foreign-platform evidence instead requires
  the ordinary gate on a compatible host.
- Platform selectors follow pnpm's host-admission semantics exactly. An
  explicit negative selector wins; a positive list requires a match; an
  all-negative list admits hosts it does not exclude; and `any` is universal
  only when it is the sole selector. Malformed or contradictory selector data
  remains undecidable and fails closed.
- A reviewed exception is a narrow, owned, expiring `exceptions` entry naming
  the check and the exact `<package>@<version>` subject, exactly like every
  other governed exception.

## Dependency and executable admission age

Every resolved registry release and every declared standalone third-party
executable must be at least 30 days old. The check reads the complete resolved
Go, npm, pnpm, and uv graphs rather than trusting only direct manifest
declarations. `supplyChain.releaseArtifacts` extends the same admission rule to
tools acquired as archives, binaries, runtime distributions, or independently
built Go commands. The delay creates a window for compromised maintainer
accounts, malicious publishing, and broken releases to be detected before
adoption.

- A release artifact names one exact checked-in `versionFile`. Its `source` is
  one of `go-toolchain`, `node-runtime`, `go-module`, `npm`, or
  `github-release`. Package and repository sources use a validated `locator`;
  GitHub release tags may add a fixed `tagPrefix`. Arbitrary metadata URLs are
  not accepted.
- Online checks resolve timestamps directly from the fixed upstream service.
  A missing pin, non-semantic or source-incompatible version, absent release,
  malformed response, oversized response, or failed request is a finding. A
  checked-in release date is never accepted as proof of age. Changing a
  declared version file selects the full online merge gate.
- Package-manager releases already observed through a manifest remain in that
  graph. For example, Code Polishy's pnpm archive pin must equal the
  `packageManager` release its JavaScript manifest already ages; it does not
  create a second artifact identity.

```json
{
  "supplyChain": {
    "releaseArtifacts": [
      {
        "name": "ruff",
        "versionFile": "tools/ruff-version.txt",
        "source": "github-release",
        "locator": "astral-sh/ruff"
      }
    ]
  }
}
```

- The shared 30-day hard minimum cannot be shortened; targets may raise it.
- `dependency-review --base REF` additionally warns when a new direct runtime
  or optional npm/pnpm/PyPI dependency is younger than the 90-day preference.
  That preference is review guidance, not a reason to retain a vulnerable
  version.
- Only `supplyChain.releaseAgeAssessments` can admit a release younger than the
  hard minimum. Each assessment matches one ecosystem, package, version, and
  scope; records either `security-fix` or `supported-release`; links HTTPS
  evidence; names an owner and reason; and expires no later than the date that
  exact release reaches the hard minimum. Standalone tools use ecosystem
  `artifact`, their configured name as the package, and their `versionFile` as
  the scope.
- Expired, changed, overlong, and unused release-age assessments fail the
  complete online profile. An unavailable metadata source remains a failure and
  defers the unused-assessment determination until observation is complete.
  General exceptions cannot waive release age.
- Environment variables do not override checked-in age policy. Missing or
  malformed registry metadata is a failure, not evidence of age.
- `GITHUB_TOKEN`, when present, authenticates release metadata requests only to
  the exact HTTPS `api.github.com` host. It is never sent to another registry or
  metadata service and does not change which release or policy is evaluated.
- A release-age assessment never suppresses a native-audit or OSV finding for
  the same package. Vulnerability enforcement takes precedence over age.

## Lifecycle scripts

Dependency lifecycle scripts execute third-party code on developer and CI
machines. A pnpm lock-owning workspace with dependencies anywhere beneath it
must declare `onlyBuiltDependencies` or `ignoredBuiltDependencies` at that
owner, even when the selected list is empty.

For pnpm, lifecycle ownership may be declared either in the lock owner's
`package.json` `pnpm` object or as a top-level field in its
`pnpm-workspace.yaml`/`pnpm-workspace.yml`. Declaring both forms, both lifecycle
fields, or both workspace filenames is an error rather than an ambiguous merge.

Normal bootstrap and lock checks should use frozen inputs and disable scripts.
That normal installation is also the pnpm license-coverage input; do not add
`--force` solely to materialize packages for another platform. When a native
build is required, keep the allowlist narrow and review it beside the dependency
diff.

For pinned pnpm versions that support native protections, Code Polishy also
requires them in the single `pnpm-workspace.yaml`/`.yml` owner. This includes a
native `minimumReleaseAge` at least as strict as the shared hard minimum and,
as each setting becomes available in the pinned pnpm version, strict missing-time
handling, `trustPolicy: no-downgrade`, lockfile re-verification, and exotic
transitive-source blocking. Native age exclusions must identify one exact
package version and match a current `releaseAgeAssessment`; broad package or
scope patterns fail.

Both settings are read from the workspace file by the sealed JavaScript tool
bundle, with the parser pnpm itself reads it with, and that file is found in the
governed tree rather than in the selected files: changing a manifest alone still
reads the settings that govern it. A workspace file the reader cannot read is a
missing-coverage finding, never a workspace that declared nothing.

## Vulnerabilities and security providers

- Go uses pinned `govulncheck` for reachable vulnerabilities.
- A pnpm project is audited by the pinned pnpm inside the sealed JavaScript
  bundle, at low severity or above. Unknown severities fail closed. The audit
  asks one policy-owned registry and installs nothing. pnpm reads its settings,
  its hooks, and its manifest from the directory it audits, so it is never
  pointed at the target: the governed lock is copied into a policy-owned
  directory holding nothing else, and that is what is audited. A target
  `.npmrc`, `.pnpmfile.cjs`, workspace settings file, manifest, or installed
  package is not there to be read, so none of them can redirect the audit,
  suppress a result, or run target code. Advisories are reported as identities,
  packages, severities, and exact affected releases; the severity threshold and
  every assessment stay in Go. pnpm's nonzero exit when advisories are present
  is a valid audit report; only a refused, unreadable, or malformed JSON report
  is an audit failure. A reported version that is no exact release is
  missing coverage, whether it is the only one the advisory named or one of
  several, so a partly readable advisory never passes as a decided one.
- No other package manager ships inside the bundle, so a Node manifest that
  names one declares its own `security` provider, exactly as it declares its own
  `lock-sync` provider.
- Policy-pinned OSV-Scanner runs as an independent source advisory lane whenever
  a supported dependency graph is detected. Its JSON is parsed centrally and
  all reported affected versions, including unknown-severity results, block.
- Python/uv must still declare module-scoped lock consistency; OSV supplies its
  default security coverage unless an exact governed module override applies.
- Scanner errors and unavailable advisory services fail.
- A vulnerability assessment records one advisory or alias, package, exact
  resolved version, lockfile scope, accepted severity ceiling, disposition,
  justification basis, impact, technical evidence, remediation tracker,
  accountable owner, independent approver, approval record, review date, and
  expiry. Ranges, wildcards, duplicate coordinates, and self-approval fail.
- Low and moderate findings are assessable. A high finding is assessable only as
  an exact `not-affected` decision; it is never a high risk acceptance. Low
  assessments may last at most 90 days from review; moderate assessments and
  high `not-affected` assessments at most 30 days. Critical, unknown-severity,
  and CISA Known Exploited Vulnerabilities remain blocking. When an assessment
  can match a CVE-addressable finding, the online profile refreshes CISA's
  catalog and fails closed if it cannot be validated.
- `not-affected` assessments require a `false-positive` or `unreachable` basis.
  `risk-accepted` assessments remain limited to low and moderate findings and
  require `mitigated` or `temporary-no-fix`. Scanner disagreement is
  conservative: a high `not-affected` assessment may cover matching low,
  moderate, or high reports, but a critical, unknown, or known-exploited report
  remains blocking.
- Applied assessments remain visible as `VULN-ACCEPTANCE` findings, including
  approver and expiry. Changed, expired, and unused assessments fail the
  complete online profile. Scanner failures and findings without a complete
  structured identity are never assessable.
- `approvedBy` and the approval URL are checked-in audit metadata. Enforce the
  actual human sign-off with protected review of `.code-polishy.json`, such as a
  CODEOWNERS rule and required approval from someone other than the owner.

Use target providers for repository secret scanning, SAST, signed provenance,
and license rules outside a pnpm project where relevant. Use the shared artifact-security module for
declared container archives, OS packages, image-config secrets and
misconfiguration, end-of-life detection, and SBOM generation. Do not add a
scanner that cannot prove it examined the applicable artifact.

Native `auditConfig.ignoreCves` and repository `osv-scanner.toml` configuration
are rejected because they can hide results before the shared
ownership/review/expiry contract is evaluated. So is a target-selected advisory
source: the registry the native audit asks is passed on the command line, and
the directory the audit runs in holds only the governed lock, so no target
configuration is layered under it in the first place.

Package-manager `overrides` and `pnpm.overrides` must have a matching
`dependencyOverridePolicies` entry containing the SHA-256 of the exact
canonical JSON override object plus reason, owner, review date, and expiry.
Run the static check once after introducing an ungoverned override to obtain the
observed canonical digest; adding governance does not suppress a vulnerability.

```json
{
  "supplyChain": {
    "releaseAgeAssessments": [
      {
        "id": "urgent-supported-release",
        "ecosystem": "pnpm",
        "package": "example-package",
        "version": "4.2.0",
        "scope": "pnpm-lock.yaml",
        "category": "security-fix",
        "evidence": "https://example.com/security/advisories/EXAMPLE-2026-1",
        "reason": "This exact release removes a vulnerability in the currently resolved version.",
        "owner": "security-team",
        "reviewed": "2026-08-14",
        "expires": "2026-09-04"
      }
    ],
    "vulnerabilityAssessments": [
      {
        "id": "example-not-reachable",
        "ecosystem": "pnpm",
        "advisory": "GHSA-xxxx-yyyy-zzzz",
        "package": "example-package",
        "affectedVersion": "1.2.3",
        "scope": "pnpm-lock.yaml",
        "severity": "low",
        "status": "not-affected",
        "basis": "unreachable",
        "reason": "The affected optional feature is disabled in every runtime.",
        "impact": "No untrusted input reaches the affected code path.",
        "evidence": "https://example.com/security/analysis/EXAMPLE-2026-1",
        "tracking": "https://example.com/issues/1234",
        "owner": "desktop-team",
        "approvedBy": "security-team",
        "approval": "https://example.com/reviews/5678",
        "reviewed": "2026-08-14",
        "expires": "2026-09-13"
      }
    ],
    "dependencyOverridePolicies": [
      {
        "id": "temporary-upstream-resolution",
        "ecosystem": "pnpm",
        "path": "package.json",
        "field": "pnpm.overrides",
        "contentSha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
        "reason": "Keeps the patched transitive release until the direct owner upgrades.",
        "owner": "dependency-team",
        "reviewed": "2026-08-13",
        "expires": "2026-11-13"
      }
    ]
  }
}
```

## Policy-owned JavaScript runtime

Code Polishy owns the JavaScript runtime its sealed tool bundle executes on.
Targets never supply it, and it is never resolved from `PATH`, a user cache, a
global installation, or target `node_modules`.

- `tools/node-version.txt` and `tools/pnpm-version.txt` pin exact versions.
  Node is governed through `supplyChain.releaseArtifacts`; pnpm is governed as
  the exact package-manager release in `tools/javascript/package.json`. Both
  therefore receive the same 30-day enforcement as the bundle graph.
- `tools/javascript_runtime_checksums.txt` holds the exact SHA-256 of every
  downloaded bootstrap archive and doubles as the supported host matrix. An
  unlisted archive is never downloaded, extracted, or executed.
- `tools/install-javascript-runtime.sh` downloads only from the Node
  distribution and npm registry origins, verifies each archive through
  `tools/verify-sha256.sh`, stages the result, and installs it with a
  same-directory rename. A rejected download leaves no partial runtime.
- No package manager runs during acquisition, so no dependency lifecycle script
  executes. The installer verifies the staged runtime by executing it under a
  closed environment with a scratch `HOME`, so user configuration cannot change
  what is installed.
- pnpm is the only supported package manager, so the `npm`, `npx`, and
  `corepack` binaries bundled with the Node distribution are removed rather
  than shipped unused, and the install fails if any other bundled package
  manager appears.
- `tools/javascript_runtime_binaries.txt` records the exact prebuilt binary
  artifacts inside the pinned pnpm release. These are integrity-pinned bytes of
  the reviewed tarball, not admitted bundle dependencies, and none is produced
  by an install script or downloaded afterwards. Installation fails when that
  set changes, so a pnpm bump cannot quietly alter the binary surface Code
  Polishy executes. The Node distribution must contribute no prebuilt binary at
  all beyond its own executable.

Updating either pin follows the dependency update procedure below and changes
the version, checksum, and binary inventories together.

## Sealed JavaScript tool bundle

`tools/javascript/` holds the one policy-owned JavaScript tool bundle: the
manifest, the frozen lock, the pnpm settings that make installing it sealed, and
the fixed runner entry point. Targets never install or configure these tools,
and they map one to one onto the capabilities Code Polishy owns — Prettier for
formatting, ESLint with the TypeScript parser and the React Hooks and JSX
accessibility plug-ins for lint and complexity, the TypeScript compiler for type
and syntax checking, and Knip for dead code. Every Knip plug-in is disabled,
because a plug-in learns a framework's entry points by loading that framework's
configuration file.

- `pnpm-workspace.yaml` is the single settings owner pnpm 11 reads. It disables
  lifecycle scripts, refuses any dependency that needs a build, refuses
  auto-installed peers and implicit hoisting, verifies store integrity, and
  applies the shared 30-day hard minimum, strict missing-time handling,
  `trustPolicy: no-downgrade`, lockfile re-verification, and exotic
  transitive-source blocking during resolution rather than only afterwards.
- `.npmrc` pins the npm registry as the only admitted source. Acquisition also
  runs with a scratch `HOME` and absent user and global npmrc paths, so no host
  configuration can add a registry, scope, proxy, or credential.
- `runner.mjs` is the one entry point Code Polishy launches, by absolute path,
  with the pinned Node runtime. It takes one bounded JSON request on stdin or
  as its exact `--request-json` argument and writes one JSON response on
  stdout, admits only the exact protocol version and the closed operation enum,
  and rejects an unknown field rather than ignoring it. It resolves every path
  from its own installed location, so a target's
  `node_modules`, the working directory, a user cache, or a global installation
  cannot supply a tool. It refuses to run under a Node other than the pinned
  one, under extra Node options, or with `NODE_OPTIONS`, `NODE_PATH`, or a
  debugger module injected. Its `provenance` operation reports the installed
  bundle digest, both runtime versions, and every tool version read from the
  package installed beside it, failing when that differs from what the bundle
  declares. Its `format` and `format-write` operations apply one sealed central
  Prettier configuration; they resolve no configuration of their own, so a
  target `.prettierrc`, `prettier.config.*`, `.editorconfig`, or
  `.prettierignore` is never read. Each analyzer is imported by a path relative
  to the runner itself, so Node never walks out of the bundle looking for one.
- `internal/javascript` is the one Go adapter that constructs sealed bundle
  operations. Its direct operations send one bounded JSON request and read one
  bounded JSON response in a disposable scratch directory and a from-nothing
  environment. Its native-audit operation instead renders the same closed
  request as a governed common-runner command, with an equally sealed disposable
  environment, so progress, timeout, resource wait, duration, logs, and
  receipts cover the registry work. Both paths reject an unknown response field
  or another protocol version rather than interpreting it, and cancellation or
  timeout kills the runner's whole process group. `code-polishy doctor` runs the
  `provenance` operation for a target that bears JavaScript or TypeScript and
  reports the answering bundle digest, both runtime versions, and every analyzer
  version; a missing, corrupted, or unanswering bundle is a finding, never a
  fallback to an ambient runtime or a target-installed tool.
- `tools/install-javascript-bundle.sh` performs one explicit network fetch of
  exactly the packages the lock names, then materializes the bundle offline
  from that store with scripts disabled. The staged tree is verified and
  inventoried before it replaces the installed one, so a rejected bundle leaves
  the previous one intact.
- `tools/javascript/bundle-manifest.mjs`, launched through
  `tools/javascript-bundle-manifest.sh` on Unix and the pinned Node directly on
  Windows, writes that inventory once onto the staged tree and verifies it
  afterwards. `bundle-manifest.json` records the
  digest of the checked-in source the bundle was installed from, the pinned Node
  and pnpm versions, the exact tool versions, the installed entry count, and one
  digest over every installed byte and symlink target. Verification recomputes
  that digest, so a bundle whose files changed, grew, shrank, whose links were
  retargeted, or that came from other checked-in source is rejected instead of
  executed. It also regenerates and compares the complete canonical manifest,
  so its provenance fields cannot change independently of the checked-in pins.
- `tools/verify-javascript-bundle-lock.sh` rejects a lock that is not version
  9.0, resolves anything without registry integrity, declares more than one
  importer, uses a non-exact direct specifier, or carries a resolution-rewriting
  key such as `overrides`, `patchedDependencies`, or `packageExtensions`.
- `tools/verify-javascript-bundle-tree.sh` rejects a materialized tree that
  carries or links to a prebuilt binary, still needs a build, skipped a package,
  hoisted implicitly, was installed by another package manager or registry, or
  resolves a package through a symlink that names an absolute path or climbs out
  of the bundle. The bundle admits no native add-on, so one tree is valid on
  every supported host.
- `scripts/check-javascript-bundle.sh` proves the checked-in bundle source and
  the installed tree agree, beyond the lock consistency Code Polishy now enforces
  on `tools/javascript/pnpm-lock.yaml` like any other pnpm project.
  It re-verifies the lock, proves the manifest needs no resolution the lock does
  not already contain, verifies the installed bundle against its own manifest,
  and reconciles `tools/javascript_bundle_inventory.txt` package by package
  against what pnpm reports as installed. Regenerate that inventory only with
  `--write-inventory`.
- The native Windows release builder installs a second graph from that same
  frozen lock and offline store using pnpm's portable hoisted linker, then
  dereferences its remaining links. It writes a fresh bundle manifest and runs
  provenance from the staged tree before archiving it. This keeps the source
  installation isolated while giving the link-free ZIP ordinary Node package
  resolution semantics after extraction.

Because `tools/javascript/pnpm-lock.yaml` is a governed lock, the ordinary
release-age, native audit, OSV, and dependency-review lanes apply to the bundle
with no exemption. A bundle dependency that is younger than the hard minimum
only because every older release carries an unfixed advisory needs a
`security-fix` release-age assessment expiring the day that exact release
reaches the minimum on its own.

## Dependency updates

1. Select the smallest official version that addresses the reason for change.
2. Produce the candidate lockfile without running lifecycle scripts.
3. Run `code-polishy dependency-review --base MERGE_TARGET` to inspect direct and
   transitive changes, publication age, native audit, and OSV results.
4. Confirm provenance and upstream support from primary sources.
5. Update exact direct pins and only the owning frozen lockfile.
6. Inspect lifecycle allowlists and transitive changes.
7. Run focused tests for affected modules.
8. Resolve the trusted merge target and run
   `code-polishy merge-gate --base MERGE_TARGET`; dependency-input changes
   normally select its complete full gate without a user level-selection
   question.
9. Run the online supply-chain gate.
10. Record any accepted finding with exact identity and severity, linked
    analysis and remediation, distinct owner and approver, approval record, and
    bounded expiry.

Avoid broad toolchain churn or unofficial forks merely to silence one advisory.

## Offline and online profiles

```sh
code-polishy supply-chain --offline
```

Offline mode checks local declarations and executes target `supply-chain`
providers: exact versions, frozen lock consistency, local-path containment,
lifecycle policy, workflow commits, and image digests.

```sh
code-polishy supply-chain
```

Online mode adds registry release-age lookups, Go tidy, native vulnerability
audits, and all target `supply-chain`, `supply-chain-online`, and `security`
providers. Each configured command runs at most once per profile invocation.
Release, merge, and dependency-update workflows should use the online profile.

```sh
code-polishy dependency-review --base origin/main
```

Dependency review compares the candidate manifests and complete supported lock
graphs with the merge base, prints a stable direct/transitive change table, and
runs the full online supply-chain profile against the candidate tree. It does
not install dependencies or execute dependency lifecycle scripts.

Repositories with dependency graphs must also run the online supply-chain
profile at least weekly so newly disclosed advisories are caught even when no
dependency update is open. A GitHub Actions repository proves this with a
weekly-or-faster `schedule` workflow containing `code-polishy supply-chain`
(without `--offline`) or `code-polishy gate`. Other CI systems declare a
`security-monitoring` provider in the `security` profile as their explicit
external scheduling contract. Missing recurring coverage is a non-suppressible
`policy.securityMonitoring` failure.

Built-in Go dependency subprocesses receive the same small operational
environment as other commands. If a private module needs additional variables
such as `GOPRIVATE`, list their names in `supplyChain.environment`; values stay
outside the checked-in config. The sealed JavaScript bundle is not one of them:
every Node and pnpm operation runs under an environment built from nothing, so
no variable named there reaches one, and a private registry, proxy, or
credential cannot be handed to it.
