# Artifact Security

Code Polishy owns the reusable container-security mechanism. Targets declare
what artifact represents the product; they do not copy Trivy setup, database,
OpenVEX, SBOM, report, or hardening scripts.

## Execution contract

The policy release pins an official Trivy image by digest and verifies that
digest, the expected Trivy version, the minimal scanner Dockerfile hash, and the
scanner runtime's OpenVEX hash before use. It copies only the Trivy binary and
CA bundle into a non-root scratch image.

The scanner image disables Go's HTTP/2 client. Its database download uses
HTTPS over HTTP/1.1; target scanning remains offline. The release-owned
[scanner assessments](design/trivy-scanner-assessments.md) document the exact
remaining findings and their applicability. Ordinary Ubuntu CI and Linux release
builds use the same verification script to exercise the installed scanner against
a minimal scratch image with a fresh database before tagging and publication.

Each run:

1. resolves the selected Docker endpoint, requires it to be local, and verifies
   a reachable server and supported server architecture;
2. builds and version-checks the minimal scanner without build network access;
3. refreshes the vulnerability database in a networked container that receives
   only a private cache mount and no target input or credentials;
4. validates database schema, freshness, timestamps, and hashes;
5. self-scans the exact scanner runtime;
6. captures each declared target into a private bounded archive;
7. scans offline with a read-only root, the
   [calling user's identity](#docker-daemon-selection), dropped capabilities,
   `no-new-privileges`, resource/time limits, no socket, and no credentials;
8. verifies that the archive and vulnerability database did not change;
9. writes normalized JSON/Markdown evidence, a CycloneDX SBOM, and a hash
   manifest beneath `.code-polishy-reports/artifact-security` by default; and
10. removes temporary containers, images, archives, and databases.

HIGH and CRITICAL vulnerabilities, secrets, image-config misconfigurations,
and end-of-life operating systems are blocking. Secret matches are never copied
into normalized findings or reports.

Scanner self-check findings identify **Code Polishy's scanner**, its Trivy
version, and its pinned source image. They point to `.code-polishy.lock.json`,
because the locked Code Polishy release owns that runtime. Application findings
retain their declared artifact scope and remediation. Both remain blocking.

Repair a scanner finding by upgrading to a verified Code Polishy release carrying
a patched scanner; rebuilding an application image cannot repair it. The scan
does not establish the availability of a fixed Code Polishy/scanner release and
reports that explicitly. A component's fixed version in Trivy output alone does
not prove that a verified scanner release contains it. The self-check and its
release-owned OpenVEX policy remain required.

Built images are saved by immutable Docker identity. Trivy reports must match
the configuration digest from that exact saved archive; see
[Container Archive Identity](design/artifact-identity.md).

## Docker daemon selection

Docker commands receive `DOCKER_HOST`, `DOCKER_CONTEXT`, `DOCKER_CONFIG`,
`DOCKER_TLS_VERIFY` and `DOCKER_CERT_PATH`, so a run uses the same daemon as the
Docker CLI. These settings reach the CLI only; scanner containers never receive
them. A verbose run and every failure after daemon selection name the daemon and
its mode.

The daemon must be on the calling machine, because scans bind-mount private
local files. A `unix` or `npipe` endpoint, or `tcp`, `http` or `https` to a
loopback address, is accepted. Any other endpoint, including `ssh`, fails before
the daemon is contacted. Selecting a local daemon does not guarantee it can read
`TMPDIR`; a daemon in a virtual machine must have that path shared.

On a rootful daemon the scanner runs as the calling user's numeric identity. On
a rootless daemon that identity maps to an unrelated subordinate user, so the
scanner runs as `0:0`, which is the calling user inside the daemon's namespace.
It gains no host authority, and every other container restriction still applies.

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
Its optional `imageId` denotes the configuration digest reported by Trivy.

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
`vulnerabilityAssessments` are a separate exact governance surface and do not
loosen OpenVEX reconciliation. OpenVEX remains the only way to record container
non-exposure.

## Risk acceptance

A HIGH finding that OpenVEX does not cover blocks unless a person approves an
exact, expiring `risk-accepted` assessment. Use it when the vulnerability is
reachable, its deployment impact is limited, and no supported fix is usable yet:

```json
{
  "id": "api-urllib3-memory",
  "ecosystem": "container",
  "advisory": "CVE-2026-97689",
  "package": "pkg:pypi/urllib3@2.5.0",
  "affectedVersion": "2.5.0",
  "scope": "api",
  "severity": "high",
  "status": "risk-accepted",
  "basis": "temporary-no-fix",
  "reason": "The bundling tool has no release with the fix yet.",
  "impact": "Downloads use trusted registries; a memory limit bounds a failed job.",
  "evidence": "https://example.invalid/assessments/api-urllib3-memory",
  "owner": "Codex",
  "approvedBy": "A Maintainer",
  "reviewed": "2026-10-31",
  "expires": "2026-11-30"
}
```

`scope` is the declared target name and `package` is the scanner's exact PURL,
including any qualifiers. It lasts at most 30 days and covers every copy of that
package version inside the target, so review all of them. The scan and any
OpenVEX reconciliation run unchanged first, and the artifact's evidence report
keeps the raw finding and immutable artifact identity. The command report shows
the final disposition.

CRITICAL, unknown-severity, and CISA-known-exploited findings cannot be accepted.
Scan failures, secrets, misconfigurations, end-of-life findings, and scanner
self-check findings are not vulnerability assessment subjects. Other changes to
the finding block it again.

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
