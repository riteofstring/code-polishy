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

The pinned `govulncheck` in binary mode reports the same eleven module
advisories for the carried and official binaries: HTTP/2 server and transport
issues in `golang.org/x/net`, gRPC server and xDS issues, and
`golang.org/x/crypto` SSH channel and OpenPGP issues. The source build changes
none of them. Advisories below the scanner's HIGH threshold are outside this
release gate.

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
