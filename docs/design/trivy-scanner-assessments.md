# Trivy Scanner Assessments

Runtime, admission, and findings reviewed by Claude Opus 5.5 on October 22, 2026
(UTC); the operation analysis below was first reviewed by Codex on October 17.
Reassess when Trivy 0.76.1 reaches the admission minimum at
2026-11-20T11:18:34Z, when the Go pin changes, or whenever the scanner
invocation, transport, or advisory evidence changes.

## Runtime and admission

The scanner is Trivy 0.74.0, built from its official Go module with the pinned
Go 1.26.10 and carried in each release; see
[Carried Trivy Scanner](carried-trivy-scanner.md) for its provenance, build,
and runtime identity. Trivy 0.75.0 and 0.76.0 were published October 1 and
October 14 and are built with Go releases that lack the GO-2026-6809 fix. Trivy
0.76.1 is not yet aged.

## Enabled operations and inputs

Code Polishy invokes only `version`, `image --download-db-only`, and
`image --input` scans. The anonymous database download receives only its private
cache. It fetches the fixed `ghcr.io/aquasecurity/trivy-db:2` repository through
`pkg/db`, `pkg/oci`, `pkg/remote`, and `pkg/x/http`. The OCI download requires one
layer and uses `google/go-containerregistry` with the standard `net/http`
transport. It does not use ORAS extraction or a gRPC service.

The scanner Dockerfile sets `GODEBUG=http2client=0`. Trivy's
[`NewTransport`](https://github.com/aquasecurity/trivy/blob/e1fd17a0ea4a8cf24bc4b4dd7e2cfbf4bb31b994/pkg/x/http/transport.go)
clones `http.DefaultTransport`; Go's
[`onceSetNextProtoDefaults`](https://github.com/golang/go/blob/go1.26.10/src/net/http/transport.go)
honors that setting and does not configure the bundled HTTP/2 client. Database
downloads therefore use HTTPS over HTTP/1.1. This removes an unnecessary
protocol from the scanner's network surface without changing its database
source, TLS verification, severity thresholds, or target isolation.

Archive input selects
[`archiveStandaloneScanService`](https://github.com/aquasecurity/trivy/blob/e1fd17a0ea4a8cf24bc4b4dd7e2cfbf4bb31b994/pkg/commands/artifact/scanner.go),
which calls `NewArchiveImage` and a local scan service. These containers have
no network, credentials, or runtime socket. They do not run an HTTP, TLS,
gRPC, xDS, or containerd server and do not restore containers from checkpoints.
Timeouts and resource limits remain defense in depth; they are not the basis
for declaring a reachable denial of service unaffected.

## Exact remaining findings

The October 22 database reports four HIGH findings in the carried scanner. Its
Go 1.26.10 standard library removes the earlier `stdlib@v1.26.6` findings,
including GO-2026-6809. The exact package PURLs and dispositions are in
`artifact-security/scanner.openvex.json`.

| Advisory                                                                                           | Package version                              | Applicable API and disposition                                                                                                                                                                                                                                       |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [CVE-2026-95837](https://github.com/containerd/containerd/security/advisories/GHSA-p7v4-vr35-mj6f) | `github.com/containerd/containerd/v2@v2.3.3` | The CRI server's `CreateContainer` restores checkpoint process credentials and capabilities. No CRI server or restore operation executes in the scanner. Reading an image archive does not invoke this API.                                                          |
| [CVE-2026-78669](https://pkg.go.dev/vuln/GO-2026-6611)                                             | `golang.org/x/net@v0.58.0`                   | The affected HTTP/2 SETTINGS handling requires a live HTTP/2 transport. Database download uses the standard library transport, and archive scanning starts no gRPC or containerd connection. The standalone `x/net/http2` transport is not used by these operations. |
| [CVE-2026-84304](https://github.com/grpc/grpc-go/security/advisories/GHSA-vp52-pcj8-j9qc)          | `google.golang.org/grpc@v1.82.1`             | Fragmented HTTP/2 DATA frames exhaust a live gRPC transport. Neither the local archive scanner nor the HTTP database client creates a gRPC connection.                                                                                                               |
| [CVE-2026-84445](https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj)          | `google.golang.org/grpc@v1.82.1`             | Missing authority/Host headers panic xDS server routing. No gRPC or xDS server is started.                                                                                                                                                                           |

## Go module advisories below the scanner gate

The pinned `govulncheck` reports further Go advisories for the carried binary.
In binary mode it finds eleven whose symbols are present and six more whose
modules are present; source mode over Trivy's exact module reports
whole-program reachability through command registration and package
initializers. Neither result establishes which code runs in Code Polishy's
invocations, so each advisory below is assessed against its precondition and
the three enabled operations:

- `trivy version`, with no network.
- `trivy image --download-db-only`, which returns after the database download
  in [`initDB`](https://github.com/aquasecurity/trivy/blob/e1fd17a0ea4a8cf24bc4b4dd7e2cfbf4bb31b994/pkg/commands/artifact/run.go). The download clones
  `http.DefaultTransport` in [`NewTransport`](https://github.com/aquasecurity/trivy/blob/e1fd17a0ea4a8cf24bc4b4dd7e2cfbf4bb31b994/pkg/x/http/transport.go).
  That transport sets `ForceAttemptHTTP2`, but Go 1.26.10's
  `onceSetNextProtoDefaults` and ALPN selection honor `GODEBUG=http2client=0`,
  so the bundled standard-library HTTP/2 client is neither configured nor
  offered. Trivy itself imports no `golang.org/x/net/http2`. Of the linked
  modules, only the Azure SDK configures `x/net/http2` at startup, on its own
  private client; Google authentication and API transports and the Kubernetes
  client configure it when their clients are built. No linked code configures
  `http.DefaultTransport`. The registry credential helpers in
  [`authOptions`](https://github.com/aquasecurity/trivy/blob/e1fd17a0ea4a8cf24bc4b4dd7e2cfbf4bb31b994/pkg/remote/remote.go) match only Google, Azure, and AWS
  registry domains, so `ghcr.io` builds none of those clients.
- `trivy image --input` with `--scanners vuln,secret` and
  `--image-config-scanners misconfig,secret`, in a container without network.
  [`ScanImage`](https://github.com/aquasecurity/trivy/blob/e1fd17a0ea4a8cf24bc4b4dd7e2cfbf4bb31b994/pkg/commands/artifact/run.go) selects
  `archiveStandaloneScanService`, and
  [`NewArchiveImage`](https://github.com/aquasecurity/trivy/blob/e1fd17a0ea4a8cf24bc4b4dd7e2cfbf4bb31b994/pkg/fanal/image/archive.go) opens the archive with
  `go-containerregistry` Docker-archive and OCI-layout readers. No SBOM source
  is configured, so remote SBOM and Rekor lookups do not run.

| Advisory                                                                                                                                                                                                               | Module                                                                                                              | Precondition                                                                                                                     | Disposition                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [GO-2026-6603](https://pkg.go.dev/vuln/GO-2026-6603), [GO-2026-6612](https://pkg.go.dev/vuln/GO-2026-6612), [GO-2026-6617](https://pkg.go.dev/vuln/GO-2026-6617)                                                       | `golang.org/x/net@v0.58.0`                                                                                          | An `x/net/http2` server receiving requests.                                                                                      | Not exposed: no operation starts an HTTP server.                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| [GO-2026-6610](https://pkg.go.dev/vuln/GO-2026-6610)                                                                                                                                                                   | `golang.org/x/net@v0.58.0`                                                                                          | An `x/net/http2` transport response forwarded to an HTTP/1 client.                                                               | Not exposed: the only `x/net/http2` transport configured in these operations is the Azure SDK's unused private client, and nothing is proxied. The standard-library copy is fixed in Go 1.26.9.                                                                                                                                                                                                                                                                                                          |
| [GO-2026-6611](https://pkg.go.dev/vuln/GO-2026-6611), [GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348), [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443), [GO-2026-6791](https://pkg.go.dev/vuln/GO-2026-6791) | `x/net`, `grpc`, `containerd`                                                                                       | As above.                                                                                                                        | Reported HIGH by the scanner; assessed in OpenVEX.                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| [GO-2026-6441](https://pkg.go.dev/vuln/GO-2026-6441)                                                                                                                                                                   | `google.golang.org/grpc@v1.82.1`                                                                                    | A gRPC xDS server applying RBAC HTTP filters.                                                                                    | Not exposed: no gRPC server is started.                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| [GO-2026-6354](https://pkg.go.dev/vuln/GO-2026-6354), [GO-2026-6355](https://pkg.go.dev/vuln/GO-2026-6355)                                                                                                             | `golang.org/x/crypto@v0.55.0`                                                                                       | An SSH connection with a malicious peer.                                                                                         | Not exposed: the importers are Git, Terraform-module, BuildKit, and signature code that image scanning does not call. The download is HTTPS and scans have no network.                                                                                                                                                                                                                                                                                                                                   |
| [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)                                                                                                                                                                   | `golang.org/x/crypto@v0.55.0`                                                                                       | Use of the unmaintained OpenPGP packages.                                                                                        | Not exposed: only Rekor PGP and Helm entry types import them, through Rekor SBOM attestation lookup, which requires `--sbom-sources rekor`. No fixed version exists.                                                                                                                                                                                                                                                                                                                                     |
| [GO-2026-6444](https://pkg.go.dev/vuln/GO-2026-6444)                                                                                                                                                                   | `github.com/containerd/containerd/v2@v2.3.3`                                                                        | A CRI server handling `ExecSync`.                                                                                                | Not exposed: no CRI package is linked into the binary.                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| [GO-2026-6597](https://pkg.go.dev/vuln/GO-2026-6597), [GO-2026-6796](https://pkg.go.dev/vuln/GO-2026-6796)                                                                                                             | `github.com/containerd/containerd/v2@v2.3.3`                                                                        | containerd image-pull handler traversal or layer unpack.                                                                         | Not exposed: of the linked Linux packages, only Trivy's `pkg/fanal/image/daemon` imports containerd, and only the daemon image-source table in `NewContainerImage` calls it; `--input` uses `go-containerregistry`'s `tarball` and `layout` readers instead. These records list no vulnerable symbols, so govulncheck also counts linked containerd package initializers.                                                                                                                                |
| [GO-2026-6505](https://pkg.go.dev/vuln/GO-2026-6505)                                                                                                                                                                   | `go.opentelemetry.io/otel/exporters/otlp/otlptrace`, with linked `otlptracegrpc` and `go.opentelemetry.io/otel/sdk` | Constructing an OTLP trace exporter.                                                                                             | Not exposed: in the linked Linux build only `github.com/docker/cli/cli/command` imports `otlptracegrpc` and `otel/sdk/trace`, and only Trivy's daemon image source imports that package. The `otel/sdk` metric packages are imported only through Google Cloud Storage under `hashicorp/go-getter`, used for VEX repository, Terraform module, and plugin downloads that these operations do not perform.                                                                                                |
| [GO-2026-4919](https://pkg.go.dev/vuln/GO-2026-4919)                                                                                                                                                                   | `github.com/aquasecurity/trivy@v0.74.0`                                                                             | Use of the malicious v0.69.4 release, or of v0.69.5 and v0.69.6 images, published with stolen release credentials in March 2026. | Not affected: the Go record's range is open-ended, but [GHSA-69fq-xp46-6x23](https://github.com/aquasecurity/trivy/security/advisories/GHSA-69fq-xp46-6x23) limits the Go package to `= 0.69.4` and states that the malicious code was never committed to `main`. GitHub's comparison shows release commit `1885610c` is not an ancestor of `e1fd17a0` (merge base `20458b83`). The carried scanner is built from the checksum-verified module source and runs no upstream release workflow or artifact. |

These dispositions depend on the invocations, network modes, and severity
gate above. Adding an operation, scanner, SBOM source, credential helper, or
server mode invalidates them.

## Scanner trust bundle

[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809) affects
`crypto/x509.Certificate.Verify` before Go 1.26.10 and 1.27.3. The
[upstream fix](https://github.com/golang/go/commit/aa64342a9e13affffea3e989d67e574475af5a78)
rejects any chain containing one of eight public keys from the compromised
eMudhra issuing CAs. The carried scanner includes that fix. Its trust bundle
also removes the roots Mozilla distrusted, so the scanner does not depend on
the denylist alone.

The official image's bundle trusts `emSign ECC Root CA - G3`, which
[cross-signed `emSign Root TLS CA - G3`](https://crt.sh/?id=14658191778)
(certificate SHA-256
`3094025792039a68af6e553686410bdab7c0d4a36b9084cba64202e35d6ebbdd`), the
issuer of the compromised TLS CAs. Unpatched Go 1.27.0 verifies all four
compromised TLS CAs to that root.

`artifact-security/scanner-ca-certificates.crt` is the scanner's only trust
store. It is the official Trivy 0.74.0 image's bundle (119 roots, SHA-256
`b8d837841b88bfaa1a0fa827cbca8e2576418dd47c9fc4bb7f1f9d89c83111b9`) without
the three eMudhra roots that the
[Mozilla bundle dated 2026-10-20](https://curl.se/ca/cacert.pem) removed:

| Root                      | SPKI SHA-256                                                       |
| ------------------------- | ------------------------------------------------------------------ |
| `emSign ECC Root CA - G3` | `8d417db2dd8bf5e3084d1e3f196d583849d81bdd4c00c70b9d39369e96b8c782` |
| `emSign Root CA - C1`     | `b7408b4d2be0238ba37004dd34e276c6019bd2f24c9db7d4980f5f6c359a4bcc` |
| `emSign ECC Root CA - C3` | `eabc185c4e82d942b1a5978ba3c0181487d6b3b9974e5c49f72f6d0bd9637150` |

The remaining 116 certificates keep their original bytes and order; the
filtered bundle differs from the source only by deleted lines. Its SHA-256 is
`faf05e99823ab205a6dd9416042ae3750f5a9e36de03d01d2cb4811097616bcc`. The bundle
adds no root and changes no other trust decision. Refresh it from a current
Mozilla-derived bundle when the scanner pin changes, keeping only distrust that
bundle already applies.

The database hosts do not depend on the removed roots. `ghcr.io` chains through
Sectigo Public Server Authentication Root R46 to USERTrust RSA, and
`pkg-containers.githubusercontent.com` chains through Let's Encrypt to ISRG
Root X1. On October 22 the carried scanner downloaded
`ghcr.io/aquasecurity/trivy-db:2` through this bundle. Tests in
`internal/artifactsecurity/trust_test.go` check that the bundle omits the three
roots and leaves the cross-certificate without a trusted issuer, while a fixture
copy of the removed root still verifies it. They also verify captured chains
for both database hosts.

OpenVEX reconciliation still requires every statement to match an observed
advisory and exact package PURL. Accepted findings remain in the scanner report.
New or unmatched findings, changed findings, and unused statements remain
blocking. Target OpenVEX and generic dependency assessments cannot override
these release-owned decisions.

## Finding ownership

The scanner runtime is owned by the locked Code Polishy release. Its blocking
findings use the lockfile as their scope and identify the carried Trivy version.
The locked release authenticates the scanner bytes. Ownership comes from the
internal scanner artifact mode, not a repository-declared target name.
Application findings keep their artifact scope,
so the same advisory in both artifacts produces distinct findings.

Scanner remediation requires a verified Code Polishy release carrying a repaired
scanner. The scan has no authenticated inventory of newer release contents, so
it explicitly establishes no fixed Code Polishy/scanner release. A component's
reported fixed version is not evidence of an available scanner release. Ownership
changes neither blocking severity nor OpenVEX reconciliation, and does not suggest
repairing the application, suppressing the finding, or substituting an unverified
scanner.
