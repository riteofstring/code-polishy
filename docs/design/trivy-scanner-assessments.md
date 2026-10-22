# Trivy Scanner Assessments

Reviewed by Codex on October 17, 2026. Reassess by November 13, 2026, or whenever
the scanner image, invocation, transport, or advisory evidence changes.

## Runtime and admission

The scanner uses official Trivy 0.74.0, published August 14, 2026, and built with
Go 1.26.6. Its source revision is
`e1fd17a0ea4a8cf24bc4b4dd7e2cfbf4bb31b994`. The image index is pinned to
`docker.io/aquasec/trivy@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969`.
The registry manifest and image labels bind that version and source revision;
Code Polishy copies only its executable into the scanner image, alongside the
release-owned trust bundle described below.

[Trivy 0.74.0](https://github.com/aquasecurity/trivy/releases/tag/v0.74.0) meets
the 30-day admission minimum. Its updated Go runtime and dependencies remove
the older XML, ASN.1, URL, DNS, Go module, go-git, ORAS, SSH, and text findings.
In particular, offline image analysis parses package metadata through Trivy's
Java and NuGet XML parsers, so local archives are not a sound reason to dismiss
the older XML decoder vulnerability.

Trivy 0.75.0 and 0.76.0 were published October 1 and October 14, respectively.
Neither is old enough at this review. The remaining findings have the specific
absent execution paths or transport mitigation below, so there is no need to
admit a fresh scanner. Review the official 0.76.0 release when it reaches the
minimum age on November 13; refresh the scan and remove assessments whose exact
package versions or findings no longer occur.

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
[`onceSetNextProtoDefaults`](https://github.com/golang/go/blob/go1.26.6/src/net/http/transport.go)
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

The October 17 database reports seven HIGH findings. Their exact package PURLs
and dispositions are in `artifact-security/scanner.openvex.json`.

| Advisory                                                                                           | Package version                              | Applicable API and disposition                                                                                                                                                                                                                                       |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [CVE-2026-95837](https://github.com/containerd/containerd/security/advisories/GHSA-p7v4-vr35-mj6f) | `github.com/containerd/containerd/v2@v2.3.3` | The CRI server's `CreateContainer` restores checkpoint process credentials and capabilities. No CRI server or restore operation executes in the scanner. Reading an image archive does not invoke this API.                                                          |
| [CVE-2026-78669](https://pkg.go.dev/vuln/GO-2026-6611)                                             | `golang.org/x/net@v0.58.0`                   | The affected HTTP/2 SETTINGS handling requires a live HTTP/2 transport. Database download uses the standard library transport, and archive scanning starts no gRPC or containerd connection. The standalone `x/net/http2` transport is not used by these operations. |
| [CVE-2026-84304](https://github.com/grpc/grpc-go/security/advisories/GHSA-vp52-pcj8-j9qc)          | `google.golang.org/grpc@v1.82.1`             | Fragmented HTTP/2 DATA frames exhaust a live gRPC transport. Neither the local archive scanner nor the HTTP database client creates a gRPC connection.                                                                                                               |
| [CVE-2026-84445](https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj)          | `google.golang.org/grpc@v1.82.1`             | Missing authority/Host headers panic xDS server routing. No gRPC or xDS server is started.                                                                                                                                                                           |
| [CVE-2026-78667](https://pkg.go.dev/vuln/GO-2026-6609)                                             | `stdlib@v1.26.6`                             | `ServeContent`, `ServeFile`, and file-serving handlers parse malicious Range headers. The scanner serves no HTTP requests and installs no file-serving client transport.                                                                                             |
| [CVE-2026-78669](https://pkg.go.dev/vuln/GO-2026-6611)                                             | `stdlib@v1.26.6`                             | Repeated HTTP/2 window changes exhaust CPU in a client or server. The image disables the standard HTTP/2 client before Trivy starts, and no server mode is enabled. This assessment depends on that mitigation.                                                      |
| [CVE-2026-97031](https://pkg.go.dev/vuln/GO-2026-6607)                                             | `stdlib@v1.26.6`                             | `decodeInnerClientHello` processes malicious ECH extensions on a TLS server. The scanner's TLS use is client-side database download; it accepts no TLS clients.                                                                                                      |

## Scanner trust bundle and CVE-2026-111943

Reviewed by Claude Opus 5.5 on October 22, 2026 (UTC). Reassess when Trivy
0.76.1, built with Go 1.27.3, reaches the admission minimum at
2026-11-20T11:18:34Z, or whenever eMudhra, Mozilla, or Go change the distrust
set.

[GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809) affects
`crypto/x509.Certificate.Verify` before Go 1.26.10 and 1.27.3. The
[upstream fix](https://github.com/golang/go/commit/aa64342a9e13affffea3e989d67e574475af5a78)
rejects any chain containing one of eight public keys from the compromised
eMudhra issuing CAs `EM DV/OV TLS CA - G3B`, `EM DV/EV TLS CA - G3C`, and
`EM OV CS CA - G3B/G3C`. Trivy 0.74.0 is built with Go 1.26.6, and its database
download is the scanner's only TLS client. A peer holding a compromised key and
an interception position on the database hosts could therefore serve a forged
database that suppresses blocking findings.

The source image's bundle (119 roots, SHA-256
`b8d837841b88bfaa1a0fa827cbca8e2576418dd47c9fc4bb7f1f9d89c83111b9`) trusts
`emSign ECC Root CA - G3`. That root
[cross-signed `emSign Root TLS CA - G3`](https://crt.sh/?id=14658191778)
(certificate SHA-256
`3094025792039a68af6e553686410bdab7c0d4a36b9084cba64202e35d6ebbdd`), which
issued the compromised TLS CAs. Go 1.27.0 without the fix verifies all four
compromised TLS CAs to that root. The compromised CAs' other issuer,
`AAA Certificate Services`, is absent from the bundle.

`artifact-security/scanner-ca-certificates.crt` is that source bundle without
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
adds no root and changes no other trust decision. It is frozen with the source
image, as the copied bundle already was. Replace it with the new image's bundle
when the scanner pin changes, then reapply only distrust that bundle lacks.

The database hosts do not depend on the removed roots. `ghcr.io` chains through
Sectigo Public Server Authentication Root R46 to USERTrust RSA, and
`pkg-containers.githubusercontent.com` chains through Let's Encrypt to ISRG
Root X1. On October 22 the rebuilt scanner downloaded
`ghcr.io/aquasecurity/trivy-db:2` through this bundle. Tests in
`internal/artifactsecurity/trust_test.go` check that the bundle omits the three
roots and leaves the cross-certificate without a trusted issuer, while a fixture
copy of the removed root still verifies it. They also verify captured chains
for both database hosts.

The finding remains affected and has no OpenVEX statement. Removing these roots
closes every path to the compromised keys found in Certificate Transparency,
but Certificate Transparency need not contain every CA certificate. An
undisclosed certificate from another bundled root to an eMudhra CA would reopen
the path. Go's public-key denylist does not depend on disclosure. No root-pool
restriction proves the same property: limiting the pool to the current database
roots would still trust Sectigo, which cross-signed these CAs from
`AAA Certificate Services`, and would break downloads after an ordinary host
certificate change. Rebuilding Trivy 0.74.0 with Go 1.26.10 would replace the
official binary with a locally built scanner and add a networked source build.
The aged Trivy release remains the resolution; this bundle reduces exposure
until then.

OpenVEX reconciliation still requires every statement to match an observed
advisory and exact package PURL. Accepted findings remain in the scanner report.
New or unmatched findings, changed findings, and unused statements remain
blocking. Target OpenVEX and generic dependency assessments cannot override
these release-owned decisions.
