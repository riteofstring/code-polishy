# Security Policy

Security is an architectural property supported by scanners. A clean
vulnerability audit does not compensate for leaked credentials, excessive
authority, unsafe parsing, partial authorization, or an agent that can escape
its intended boundary.

## Trust boundaries

For every external input and privileged operation, document:

- who controls the input;
- what schema and size are accepted;
- where it is normalized and validated;
- what authority the operation receives;
- what filesystem, network, process, database, or provider boundary it crosses;
- what evidence proves the boundary from inside the execution environment.

Validate before side effects. Normalize paths and then prove they remain under
an allowed root. Avoid shell interpretation when an argument array works. Do
not treat issue text, documents, generated content, provider output, or model
output as trusted instructions or executable source.

## Credentials and authority

- Never print, commit, copy into artifacts, or forward credentials to child
  processes that do not need them.
- Prefer short-lived, repository-scoped tokens and read-only permissions.
- GitHub workflows declare minimal `permissions`; default to `contents: read`.
- Keep scanners away from the Docker socket and developer credential stores.
- Use temporary application data for tests and recordings.
- Destructive operations resolve and display exact targets before mutation and
  refuse broad roots, symlink escapes, and unresolved variables.
- Production adapters receive only the capability their port requires.

Secrets scanning belongs in a configured `security` provider. Scan committed
history in release or scheduled workflows where practical, not only the working
tree.

## External processes and generated code

- Use argument arrays rather than constructing shell strings.
- Keep large executable programs out of string literals; normal source can be
  formatted, linted, typechecked, reviewed, and scanned.
- Generated code has one authoritative input and is never edited manually.
- Sandbox untrusted execution with an allowlisted environment, bounded files,
  bounded network, resource limits, and no inherited credentials.
- Test containment from inside the sandbox. A prose instruction to remain in a
  boundary is not containment.

## Failure behavior

- Fail closed for authorization, integrity, publication, schema, and policy
  failures.
- Return specific failures; do not turn internal invalid data into a plausible
  success response.
- Retry only failures known to be transient and safe to repeat.
- Make idempotency explicit for retried mutations.
- Avoid broad catches and keyword heuristics for security-relevant decisions.
- Log enough structured metadata to investigate without recording secrets or
  sensitive content.

## Dependency and build security

The [Supply-chain Policy](supply-chain.md) requires immutable dependency and CI
inputs, a release-age delay, lifecycle-script allowlists, and vulnerability
audits. Native and OSV dependency scans run online at least weekly, not only
when lockfiles change. A low or moderate finding may be dispositioned only
through the exact, independently approved, expiring vulnerability-assessment
contract. A high finding may only be recorded as an exact `not-affected`
decision with a `false-positive` or `unreachable` basis; it is never a high
risk acceptance. Severity alone is not a rationale: record applicability,
impact, compensating controls, technical evidence, and remediation tracking.
Critical, unknown-severity, and known-exploited findings are never accepted.
Higher-risk repositories should add:

- secret scanning;
- SAST appropriate to the languages and frameworks;
- license policy;
- CycloneDX or SPDX SBOM generation;
- signed provenance and artifact checksums;
- container image, OS package, misconfiguration, and end-of-life checks;
- isolated reproducible builds without build-time network access where
  feasible.

Container findings remain visible when a precise OpenVEX assessment marks them
not affected. Code Polishy compares the same immutable archive with and without
VEX enforcement, rejects suppression of secrets/misconfiguration/EOL findings,
and rejects changed or unused statements. Every unmatched HIGH or CRITICAL
finding blocks release. See [Artifact Security](../artifact-security.md).

## Security tests

Test the mechanism, not the presence of its source text:

- malformed, oversized, adversarial, and ambiguous input;
- path traversal and symlink escape;
- missing, expired, and under-scoped credentials;
- interrupted and repeated mutations;
- authorization at every externally reachable operation;
- redaction in logs, reports, recordings, and child environments;
- production serialization and transport boundaries;
- sandbox behavior from the untrusted side.

Use local substitutes for owned services and mock true third parties only at
their adapters. Keep live-provider tests separate and explicit; a skipped live
test is not proof of integration.

## Reporting

Do not commit a secret to demonstrate a secret-scanning bug. Record the affected
component, impact, reproduction using synthetic data, containment, remediation,
and verification. Coordinate disclosure before publishing sensitive details.
