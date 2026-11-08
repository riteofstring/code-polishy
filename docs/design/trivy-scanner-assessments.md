# Trivy Scanner Assessments

RSA JWK execution paths reviewed by Codex on October 28, 2026 UTC; HTML parser
binary evidence reviewed by Codex on November 8, 2026 UTC. Reassess by November
13, 2026, or whenever the scanner image, invocation, mounts, environment,
transport, or advisory evidence changes.

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

Docker client settings select the daemon and reach only the Docker CLI. The
scanner's container environment stays `HOME=/tmp` plus the image's `GODEBUG`.
Under a rootless daemon the scanner runs as `0:0`, the caller inside the daemon's
user namespace, because any other identity maps to a subordinate user that cannot
open the private mounts. This changes file ownership, not scanner inputs: the
operations, mounts, network, dropped capabilities and absence of credentials are
unchanged, so the CVE-2026-116587 path analysis below still applies. A remote
daemon is refused because it cannot read those mounts.

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

## CVE-2026-78661: HTML text-node coalescing

The November 8 installed scanner check reports `CVE-2026-78661` for
`pkg:golang/golang.org/x/net@v0.60.0`. The
[Go advisory GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860) identifies
quadratic string copying in `html` parser text-node coalescing. Its affected
entry points are `Parse`, `ParseFragment`, `ParseFragmentWithOptions`,
`ParseWithOptions`, and the parser's `addText`, `fosterParent`, and `parse`
methods. Attacker-controlled HTML must reach that parser to trigger the issue.

The module is present, but the vulnerable parser code is absent from both
executables selected by the pinned image index. Inspection used the actual
upstream Linux binaries, with these immutable identities:

| Architecture | Platform manifest digest                                                  | Executable SHA-256                                                 |
| ------------ | ------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| amd64        | `sha256:2e6c00d711ab4ae41d8a30c2c1fd3bd3b62704c177bb120f95012e939edfb3df` | `d1dfca6ae10a1ad4540bb94e64f0510077c47008920a9b554986b39dca573d83` |
| arm64        | `sha256:fc223b6f033406fd0c5efe59b566295e90cff76ccd2af696bd0d66d53ad83a2d` | `b422130ea54e53e142e65c838662e48919ce9c3f23f17d80066b43e70550cb85` |

Both binaries report Go 1.27.3, `golang.org/x/net` v0.60.0, `CGO_ENABLED=0`,
and source revision `e713f67f7d54decdcd9b3132c9751833dcced698`. Their build
metadata also reports `vcs.modified=true`, so an upstream source listing alone
is not the basis for the absence claim. The inspection is bound to the actual
image manifests and executable bytes above.

The ordinary ELF symbol tables are stripped. `govulncheck` v1.3.0 extract mode
returns zero package symbols for each binary; its binary scanner then falls
back to module-level findings. That fallback does not establish parser
reachability or absence.

Instead, inspection parsed the ELF `.gopclntab` and validated its Go header,
function count, function-name bounds, and every runtime function's name. The
Go 1.27.3 linker's
[`walkFuncs` and `generateFuncnametab`](https://github.com/golang/go/blob/go1.27.3/src/cmd/link/internal/ld/pcln.go)
include both linked functions and functions in their inline trees in this name
table. It is not a search over arbitrary binary strings. The amd64 binary has
207,730 runtime functions and 226,330 distinct function names including inlined
functions; arm64 has 207,624 and 226,434. Positive controls `main.main`,
`runtime.main`, and `golang.org/x/net/context.init` are present on both.

The complete HTML-related function-name inventory is identical on both:

```text
golang.org/x/net/html.init
golang.org/x/net/html/charset.Lookup
golang.org/x/net/html/charset.(*htmlEncoding).NewEncoder
golang.org/x/net/html/charset.NewReaderLabel
golang.org/x/net/html/charset.htmlEncoding.NewDecoder
golang.org/x/net/html/charset.(*htmlEncoding).NewDecoder
```

The parser entry points and coalescing methods are absent, including inline
metadata. Charset conversion and package initialization do not implement HTML
parsing. The disposition is therefore `not_affected` with justification
`vulnerable_code_not_present` for this advisory and PURL only. Resource limits
and offline execution are not the basis of this assessment.

### Reproduce the binary inspection

Inspect the pinned index with `docker buildx imagetools inspect --raw`, require
the two Linux platform digests above, and pull each platform manifest by digest.
Use `docker create --platform linux/ARCH IMAGE version`, then `docker cp` to copy
`/usr/local/bin/trivy` from the stopped container; remove that container afterward.
No scanner code needs to execute for this inspection. Check the binary metadata
with `go version -m` and `govulncheck -mode=extract`.

Save the following inspector outside the checkout and run it with the pinned
Go 1.26.10 toolchain: `go run inspect-pcln.go /absolute/path/to/trivy-amd64
/absolute/path/to/trivy-arm64`. Require both recorded executable digests,
function counts, positive controls and HTML inventories to match. A malformed
or unsupported metadata table fails inspection rather than proving absence.

```go
package main

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"debug/gosym"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

func main() {
	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		file, err := elf.NewFile(bytes.NewReader(data))
		if err != nil {
			panic(err)
		}
		pcln, err := file.Section(".gopclntab").Data()
		if err != nil {
			panic(err)
		}
		if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || binary.LittleEndian.Uint32(pcln[:4]) != 0xfffffff1 || pcln[7] != 8 {
			panic("unsupported ELF or Go pclntab")
		}
		nfunc := binary.LittleEndian.Uint64(pcln[8:16])
		start := binary.LittleEndian.Uint64(pcln[32:40])
		end := binary.LittleEndian.Uint64(pcln[40:48])
		if start >= end || end > uint64(len(pcln)) {
			panic("invalid function-name table bounds")
		}
		table, err := gosym.NewTable(nil, gosym.NewLineTable(pcln, file.Section(".text").Addr))
		if err != nil || uint64(len(table.Funcs)) != nfunc {
			panic(fmt.Sprintf("incomplete pclntab: %v", err))
		}
		names := map[string]bool{}
		var html, net []string
		for _, raw := range bytes.Split(pcln[start:end], []byte{0}) {
			if len(raw) == 0 {
				continue
			}
			if !utf8.Valid(raw) {
				panic("invalid function name")
			}
			name := string(raw)
			names[name] = true
			if strings.HasPrefix(name, "golang.org/x/net/html.") || strings.HasPrefix(name, "golang.org/x/net/html/") {
				html = append(html, name)
			}
			if strings.HasPrefix(name, "golang.org/x/net/") {
				net = append(net, name)
			}
		}
		for _, function := range table.Funcs {
			if !names[function.Name] {
				panic("runtime function missing from name table")
			}
		}
		if !names["main.main"] || !names["runtime.main"] || len(net) == 0 {
			panic("positive controls absent")
		}
		sum := sha256.Sum256(data)
		result := map[string]any{"binary": path, "machine": file.Machine.String(), "sha256": hex.EncodeToString(sum[:]), "runtimeFunctions": len(table.Funcs), "functionNamesIncludingInlined": len(names), "xNetFunctionNames": len(net), "htmlFunctionNames": html, "positiveControls": []string{"main.main", "runtime.main", net[0]}}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			panic(err)
		}
	}
}
```

The official [Trivy 0.76.2 release](https://github.com/aquasecurity/trivy/releases/tag/v0.76.2)
was published November 5, 2026 at 12:50:25 UTC and contains x/net v0.62.0 and
go-jose v4.1.6. It is younger than the required 30-day admission window and
includes additional dependency and base-image changes. The absent HTML parser
and the separately assessed RSA credential path do not justify admitting that
fresh release early. Retain the exact 0.76.1 image and its existing certificate-fix
admission. Reassess by November 13 UTC or immediately if either image digest,
compiler/build settings, scanner execution contract or advisory changes. Do not
carry this binary-absence claim to a different image.

## Scanner finding enforcement

The signed scanner manifest embeds the OpenVEX statements above. Its publisher
signature, exact image, execution contract, and assessment deadline are verified
before the statement can authorize a suppression. See
[Scanner manifests](scanner-manifests.md) and
[Publisher setup](../scanner-publisher-setup.md).
Every statement must match an observed advisory and package PURL. New or
unmatched findings, changed findings, and unused statements remain blocking.
Target OpenVEX and generic dependency assessments cannot override these
publisher-authenticated decisions.

An explicit empty `statements` array accepts no findings and avoids a second
enforcement scan. Missing or null arrays remain malformed policy.

Ordinary Ubuntu CI and Linux release publication call the same installed
artifact-security verification script. The scanner self-report and minimal
scratch-image report must both have no blocking findings and valid SBOMs.
Fresh database acquisition remains part of that test; a successful result is
evidence for its recorded advisory snapshot, not a waiver of later findings.

## Finding ownership

The scanner runtime and execution contract are owned by the locked Code Polishy
release. Scanner image and assessment selection belong to the authenticated
manifest in the repository lock. Its blocking
findings use the lockfile as their scope and identify the Trivy version and pinned
source image. Ownership comes from the internal scanner artifact mode, not a
repository-declared target name. Application findings keep their artifact scope,
so the same advisory in both artifacts produces distinct findings.

Scanner remediation uses an explicit `scanner-update` preview and adoption of an
authenticated manifest revision. An assessment-only update can retain the same
Code Polishy release and scanner binary. The scan establishes no fixed package
revision from a dependency's reported fixed version. Scanner findings cannot be
repaired by rebuilding an application or by supplying project waivers.
