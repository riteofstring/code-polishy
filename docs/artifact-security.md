# Artifact Security

Code Polishy owns the reusable container-security mechanism. Targets declare
what artifact represents the product; they do not copy Trivy setup, database,
OpenVEX, SBOM, report, or hardening scripts.

## Execution contract

The policy release pins an official Trivy image by digest and verifies that
digest, the expected Trivy version, the minimal scanner Dockerfile hash, and the
scanner runtime's OpenVEX hash before use. It copies only the Trivy binary and
CA bundle into a non-root scratch image.

Each run:

1. verifies a reachable Docker server and supported server architecture;
2. builds and version-checks the minimal scanner without build network access;
3. refreshes the vulnerability database in a networked container that receives
   only a private cache mount and no target input or credentials;
4. validates database schema, freshness, timestamps, and hashes;
5. self-scans the exact scanner runtime;
6. captures each declared target into a private bounded archive;
7. scans offline with a read-only root, non-root user, dropped capabilities,
   `no-new-privileges`, resource/time limits, no socket, and no credentials;
8. verifies that the archive and vulnerability database did not change;
9. writes normalized JSON/Markdown evidence, a CycloneDX SBOM, and a hash
   manifest beneath `.code-polishy-reports/artifact-security` by default; and
10. removes temporary containers, images, archives, and databases.

HIGH and CRITICAL vulnerabilities, secrets, image-config misconfigurations,
and end-of-life operating systems are blocking. Secret matches are never copied
into normalized findings or reports.

## Target modes

Use `dockerfile` when the shared engine can build the product directly:

```json
{
  "name": "api-image",
  "module": "api-delivery",
  "mode": "dockerfile",
  "dockerfile": "deploy/api.Dockerfile",
  "context": ".",
  "platform": "linux/amd64",
  "openVex": "security/api.openvex.json"
}
```

Use `archive` for an existing contained Docker/OCI archive:

```json
{
  "name": "release-image",
  "module": "release",
  "mode": "archive",
  "archive": "dist/release-image.tar"
}
```

Use `command` only when artifact construction has product-specific sequencing.
Code Polishy runs the checked-in command as an argument array and sets
`CODE_POLISHY_ARTIFACT_OUTPUT` to a new private directory:

```json
{
  "name": "eval-image",
  "module": "eval-runtime",
  "mode": "command",
  "producer": {
    "argv": ["./scripts/build-eval-artifact.sh"],
    "cwd": ".",
    "manifest": "manifest.json",
    "environment": [],
    "timeoutSeconds": 1800
  },
  "openVex": "security/eval.openvex.json"
}
```

The producer must write a regular bounded archive and this versioned manifest
beneath the supplied directory:

```json
{
  "version": 1,
  "archive": "image.tar",
  "reference": "product/eval:policy-scan",
  "imageId": "sha256:optional-exact-image-id"
}
```

The producer owns only product construction. It must not run Trivy, download a
database, apply OpenVEX, or publish security reports.

## OpenVEX

Target OpenVEX is optional and product-specific. Documents must use the
OpenVEX 0.2 context, an absolute HTTPS identity, a non-future timestamp, and
bounded exact `not_affected` statements. Every statement needs a supported
justification, a substantive impact statement, and exact package PURLs.

Code Polishy scans the same immutable archive both without and with the document.
It reconciles the full normalized result sets and fails when VEX:

- suppresses a secret, misconfiguration, or end-of-life finding;
- suppresses a vulnerability without the exact advisory/PURL statement;
- changes or introduces a finding; or
- contains an unused statement.

Accepted VEX findings remain visible in the report. General supply-chain
`vulnerabilityAssessments` are a separate exact governance surface for
structured dependency findings and do not loosen OpenVEX reconciliation.
Container HIGH/CRITICAL findings remain governed only by the stricter OpenVEX
contract.

## Commands and prerequisites

```sh
code-polishy artifact-security
code-polishy supply-chain
code-polishy gate
```

The focused command runs configured artifact targets only. The online
supply-chain profile and gate include the same module. A Docker CLI and server
are required only when artifact targets exist; Trivy itself is policy-owned and
does not become a target manifest dependency.

## Behavior and final-state review evidence

Behavior-review files use a separate local artifact boundary under
`.code-polishy-reports/behavior-review`. Intent capture may exist even when
review remains optional. Each entry binds exact intent bytes, the current HEAD,
and a digest of staged, unstaged, deleted, and untracked candidate state. The
directory later holds additive requirements, the exact packet, structured
final-state evidence and findings, proofs, logs, and receipt. Journal appends
use an interprocess lock and every mutation publishes atomically.

Its hashes bind the same captured bytes, task-requirement snapshot, base and
candidate policy decision, selected features, commits, packet, and proof records
through the workflow. They are integrity checks, not signatures. The agent
harness remains responsible for supplying the actual user request and isolating
the review subagent. See the
[Behavior and Final-State Review Policy](policies/behavior-review.md) for the exact
workflow and limits.
