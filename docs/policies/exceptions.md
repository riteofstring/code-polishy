# Exception Policy

An exception records bounded, temporary risk. It is not a second configuration
system and never means "ignore this category forever."

Every exception must include:

- a stable identifier;
- one exact check name;
- one exact path;
- one exact subject (for example `package@version` or imported target);
- a concrete reason and compensating control;
- an accountable owner;
- an ISO expiry date.

Expiry must be within 366 days of configuration. Shorter horizons are preferred
for active migrations and newly released dependencies.

Example:

```json
{
  "id": "legacy-publisher-extraction",
  "check": "quality.fileLength",
  "path": "src/publishing/legacy-publisher.ts",
  "subject": "1240",
  "reason": "Atomic-write extraction is tracked for the current milestone; new behavior is prohibited in this file.",
  "owner": "publishing",
  "expires": "2026-09-15"
}
```

The gate prints every applied exception. On the day after expiry, the exception
itself becomes a blocking `policy.exceptionExpired` finding, even if the originally
matched file is not in the current change.

## Rules

- Fix formatting, syntax, floating versions, missing lockfiles, and unpinned CI
  inputs instead of exempting them.
- Never exempt `*` paths or subjects for convenience.
- Configuration validity, tool prerequisites, and `policy.*` coverage findings
  cannot be exempted.
- Release age uses `supplyChain.releaseAgeAssessments`, never the general
  exception list. It matches one ecosystem, package, version, and lockfile
  scope; permits only a documented security fix or required supported release;
  and cannot outlive the date that release reaches the hard minimum.
- Architecture exceptions use this central expiring format and match the exact
  check, importing source path, and imported target module. There is no
  permanent module-local ignore list.
- Vulnerabilities use `supplyChain.vulnerabilityAssessments`, not the general
  exception list. They match ecosystem, advisory or alias, package, exact
  affected version, lockfile scope, and an approved severity ceiling. Low and
  moderate findings may be `risk-accepted`; a high finding may only be an exact
  `not-affected` decision with a `false-positive` or `unreachable` basis and a
  30-day maximum. Critical, unknown, and CISA-known-exploited findings remain
  blocking. Technical evidence, a tracker, distinct owner and approver, an
  approval record, and a severity-bounded expiry are mandatory. Accepted
  findings remain visible in reports.
- Dependency overrides use `supplyChain.dependencyOverridePolicies`, keyed to
  the exact canonical JSON hash of the governed override block. An override is
  not automatically a vulnerability waiver.
- Renewing an exception is a new risk decision. Update evidence and choose a new
  bounded date; do not mechanically extend it.
- Delete the exception in the same change that removes the debt.

Silent package-manager vulnerability ignores and repository OSV configuration
are rejected. Broad scanner ignore formats do not override this policy;
container not-affected statements use the strict OpenVEX contract described by
the artifact-security policy. Protect `.code-polishy.json` with CODEOWNERS and
required review because checked-in approver metadata cannot itself prove who
approved a change.
