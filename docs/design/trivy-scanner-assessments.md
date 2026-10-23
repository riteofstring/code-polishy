# Trivy Scanner Assessments

Reviewed by Codex on October 23, 2026 UTC. Reassess by November 20, 2026, or whenever
the scanner image, invocation, transport, or advisory evidence changes.

## Runtime and admission

The scanner uses official Trivy 0.76.1, published October 21, 2026 at
11:18:34 UTC, with binaries built using Go 1.27.3. Its source revision is
`e713f67f7d54decdcd9b3132c9751833dcced698`. The image index is pinned to
`docker.io/aquasec/trivy@sha256:790fd66d1697395d8693d1acaa1ea702406a20f109b6fc7bac8db623eca6c06c`.
The registry manifest and image labels bind that version and source revision;
Code Polishy copies only its executable and CA bundle into the scanner image.

[Trivy 0.76.1](https://github.com/aquasecurity/trivy/releases/tag/v0.76.1)
explicitly corrects CVE-2026-111943 through the patched Go runtime and an updated
CA bundle. The exact `trivy-0-76-1-x509-security-fix` admission covers the period
until it reaches 30 days on November 20 at 11:18:34 UTC; it expires that day.

The previous Go 1.26.6 runtime can accept certificates issued through compromised
intermediates during the scanner's HTTPS advisory-database download. An attacker
with such a certificate and a network interception position can forge the
database that drives vulnerability decisions. Offline target scans do not remove
this earlier network input. No aged official scanner release contains the fix.
Disabling database refresh loses current advisory coverage, and maintaining a
private certificate workaround or rebuilding upstream adds a separate security
maintenance path. The official runtime correction and CA update address both
parts of the documented upstream remedy.

This is not a runtime-only patch relative to 0.74.0: the official comparison has
106 commits, including dependency and analyzer updates. Fresh parser behavior,
upstream packaging defects, and supply-chain compromise remain risks. Pinning the
official image index, checking both Linux architectures, and running the installed
scanner against fresh advisory data constrain those risks without proving their
absence. Waiting retains an exploitable authentication boundary in the scanner's
security input, so the bounded official update is preferable. Remove this exact
admission when the version ages or leaves the pin; do not extend it to another
version or suppress the certificate finding.

## Enabled operations and inputs

Code Polishy invokes only `version`, `image --download-db-only`, and
`image --input` scans. The anonymous database download receives only its private
cache. It fetches the fixed `ghcr.io/aquasecurity/trivy-db:2` repository through
`pkg/db`, `pkg/oci`, `pkg/remote`, and `pkg/x/http`. The OCI download requires one
layer and uses `google/go-containerregistry` with the standard `net/http`
transport. It does not use ORAS extraction or a gRPC service.

The scanner Dockerfile sets `GODEBUG=http2client=0`. Trivy's
[`NewTransport`](https://github.com/aquasecurity/trivy/blob/e713f67f7d54decdcd9b3132c9751833dcced698/pkg/x/http/transport.go)
clones `http.DefaultTransport`; Go's
[`onceSetNextProtoDefaults`](https://github.com/golang/go/blob/go1.27.3/src/net/http/transport.go)
honors that setting and does not configure the bundled HTTP/2 client. Database
downloads therefore use HTTPS over HTTP/1.1. This removes an unnecessary
protocol from the scanner's network surface without changing its database
source, TLS verification, severity thresholds, or target isolation.

Archive input selects
[`archiveStandaloneScanService`](https://github.com/aquasecurity/trivy/blob/e713f67f7d54decdcd9b3132c9751833dcced698/pkg/commands/artifact/scanner.go),
which calls `NewArchiveImage` and a local scan service. These containers have
no network, credentials, or runtime socket. They do not run an HTTP, TLS,
gRPC, xDS, or containerd server and do not restore containers from checkpoints.
Timeouts and resource limits remain defense in depth; they are not the basis
for declaring a reachable denial of service unaffected.

## Scanner findings and empty assessment policy

The October 23 fresh-database scan of the minimal Trivy 0.76.1 runtime reports
no HIGH or CRITICAL vulnerabilities. The previous seven exact assessments are
removed because their affected package versions are no longer carried. The
release-owned OpenVEX document explicitly contains an empty `statements` array.

An explicit empty array means no findings are accepted: the observed blocking
findings pass through unchanged. Missing or null arrays remain malformed policy.
The runtime avoids a second enforcement scan when there are no assessments.
For nonempty documents, every statement must still match an observed advisory
and exact package PURL. New or unmatched findings, changed findings, and unused
statements remain blocking. Target OpenVEX and generic dependency assessments
cannot override these release-owned decisions.

Ordinary Ubuntu CI and Linux release publication call the same installed
artifact-security verification script. The scanner self-report and minimal
scratch-image report must both have no blocking findings and valid SBOMs.
Fresh database acquisition remains part of that test; a successful result is
evidence for its recorded advisory snapshot, not a waiver of later findings.

## Finding ownership

The scanner runtime is owned by the locked Code Polishy release. Its blocking
findings use the lockfile as their scope and identify the Trivy version and pinned
source image. Ownership comes from the internal scanner artifact mode, not a
repository-declared target name. Application findings keep their artifact scope,
so the same advisory in both artifacts produces distinct findings.

Scanner remediation requires a verified Code Polishy release carrying a repaired
scanner. The scan has no authenticated inventory of newer release contents, so
it explicitly establishes no fixed Code Polishy/scanner release. A component's
reported fixed version is not evidence of an available scanner release. Ownership
changes neither blocking severity nor OpenVEX reconciliation, and does not suggest
repairing the application, suppressing the finding, or substituting an unverified
scanner.
