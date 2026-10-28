# Trivy Scanner Assessments

Reviewed by Codex on October 28, 2026 UTC. Reassess by November 13, 2026, or whenever
the scanner image, invocation, mounts, environment, transport, or advisory
evidence changes.

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

## CVE-2026-116587: RSA JWK parsing

The October 28 fresh-database scan reports
`CVE-2026-116587` for
`pkg:golang/github.com/go-jose/go-jose/v4@v4.1.5` in the scanner executable.
The [upstream advisory](https://github.com/go-jose/go-jose/security/advisories/GHSA-mgcc-cww5-fp2q)
describes excessive CPU consumption when attacker-controlled RSA private-key
parameters reach JWK parsing. In the
[resolved parser](https://github.com/go-jose/go-jose/blob/v4.1.5/jwk.go),
`JSONWebKey.UnmarshalJSON` reaches `rawJSONWebKey.rsaPrivateKey`, which calls
`crypto/rsa.PrivateKey.Validate` before bounding the modulus and primes.

The exact scanner source and resolved dependencies establish these paths:

- gRPC 1.84.0's
  [certificate watcher](https://github.com/grpc/grpc-go/blob/v1.84.0/credentials/tls/certprovider/pemfile/watcher.go)
  reads a configured SPIFFE trust-bundle map. Its
  [`BundleMapFromBytes`](https://github.com/grpc/grpc-go/blob/v1.84.0/internal/credentials/spiffe/spiffe.go)
  calls go-spiffe 2.8.1's
  [`spiffebundle.Parse`](https://github.com/spiffe/go-spiffe/blob/v2.8.1/bundle/spiffebundle/bundle.go),
  which unmarshals a `jose.JSONWebKeySet`. The
  [xDS credentials configuration](https://github.com/grpc/grpc-go/blob/v1.84.0/internal/xds/bootstrap/tlscreds/bundle.go)
  selects this trust-file input. Code Polishy's scanner receives no xDS
  bootstrap, SPIFFE trust file, caller environment or credentials, and its
  enabled operations do not create this credential provider.
- The anonymous database download uses Trivy's OCI client and
  [go-containerregistry 0.22.1 bearer transport](https://github.com/google/go-containerregistry/blob/v0.22.1/pkg/v1/remote/transport/bearer.go).
  The transport decodes the registry's token response and forwards the token as
  a bearer string; it does not parse JWKs or select gRPC credentials. The
  downloader receives only its private cache.
- Target input follows `archiveStandaloneScanService` and
  [`NewArchiveImage`](https://github.com/aquasecurity/trivy/blob/e713f67f7d54decdcd9b3132c9751833dcced698/pkg/fanal/image/archive.go).
  This selects local Docker/OCI archive readers. Archive contents and image
  environment values remain analysis data, and cannot configure the scanner's
  credential providers. Image-configuration analysis uses OPA 1.21.1, whose
  [JWT built-ins](https://github.com/open-policy-agent/opa/blob/v1.21.1/v1/topdown/tokens.go)
  use `lestrrat-go/jwx/v3` and do not invoke this go-jose parser.
- Sigstore 1.10.9's
  [`StaticTokenGetter.GetIDToken`](https://github.com/sigstore/sigstore/blob/v1.10.9/pkg/oauthflow/flow.go)
  is another go-jose consumer. The scanner's enabled commands do not initiate
  Sigstore OIDC token acquisition or accept an identity-token argument.

The disposition is `not_affected`, with justification
`vulnerable_code_not_in_execute_path`, for that advisory and package PURL only.
An attacker-controlled archive or registry response has no enabled path to the
affected RSA parser. Container resource limits are additional controls, not the
basis of this assessment. The affected dependency remains present and the raw
finding remains visible in `acceptedFindings`.

The official [go-jose 4.1.6 release](https://github.com/go-jose/go-jose/releases/tag/v4.1.6)
was published October 14 at 23:14:52 UTC and reaches 30 days on November 13.
The official Trivy release inventory still ends at 0.76.1 on October 28; no
fixed official scanner release is available. Retain the exact official scanner
and its separate certificate-fix admission. Codex owns reassessment by November
13 UTC, including checking for an official scanner carrying go-jose 4.1.6 or
later. Remove the statement when that affected package leaves the scanner.
Any new scanner command, credential flow, configurable policy, mount or
environment input requires reassessment before use.

## Scanner finding enforcement

The release-owned OpenVEX document contains the one exact statement above.
Every statement must match an observed advisory and package PURL. New or
unmatched findings, changed findings, and unused statements remain blocking.
Target OpenVEX and generic dependency assessments cannot override these
release-owned decisions.

An explicit empty `statements` array accepts no findings and avoids a second
enforcement scan. Missing or null arrays remain malformed policy.

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
