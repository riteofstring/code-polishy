# Carried Trivy Scanner

The scanner is Trivy 0.74.0, built by Code Polishy from the official Go module
`github.com/aquasecurity/trivy@v0.74.0` with the pinned Go 1.26.10. The module
proxy records that version at 2026-08-14T10:24:58Z from tag `v0.74.0`, commit
`e1fd17a0ea4a8cf24bc4b4dd7e2cfbf4bb31b994`, and the checksum database records
its content as `h1:B2/h1Aehsq/zykg0jxpWUlOX7e3UXpbxjZZs/0vNlC4=`, pinned in
`tools/trivy_module_sum.txt`. [Trivy 0.74.0](https://github.com/aquasecurity/trivy/releases/tag/v0.74.0)
meets the 30-day admission minimum, and its `go.mod` has no replacements.

`tools/install-go-tools.sh` and `tools/install-policy-tools.ps1` run
`go install github.com/aquasecurity/trivy/cmd/trivy@v0.74.0` in a private module
and build cache, ignoring user Go configuration and private-module exceptions.
They fetch only from `proxy.golang.org`, verify against `sum.golang.org`, and
copy the official build's settings: `CGO_ENABLED=0`, `GOEXPERIMENT=jsonv2`, and
`-s -w -X github.com/aquasecurity/trivy/pkg/version/app.ver=0.74.0`, adding
`-trimpath`. The result must match the carrier identity below. The pinned module
content includes Trivy's `go.sum`, so the module graph is frozen. The
installer keeps the binary in `.tools/trivy/linux-<arch>` for the host
architecture; Windows releases carry `linux-amd64`.

A carrier is admitted only when the binary's build information records exactly
`go1.26.10-X:jsonv2`, Trivy's module, version, and checksum, and the build
settings `CGO_ENABLED=0`, the expected `GOARCH`, `GOOS=linux`, and
`-trimpath=true`, so a same-module build for another platform or with other
settings is refused. The tool installer's reuse check, release installation on
Linux and macOS, and the Windows release build apply that identity. The release
manifest covers the admitted bytes, so the runtime trusts the installed release
rather than rechecking them.

Distributing the binary distributes every module linked into it. For each
module the binary's build information names, including Trivy itself, the
installer copies, with its relative path, every regular file under a
`LICENSES` directory and every file whose name contains `license`, `licence`,
`copying`, `notice`, or `patents`. Name matching also covers forms such as
`APACHE-LICENSE-2.0`, `GO-LICENSE`, `3rdpartylicenses.md`, and
`*.json.license`, and nested forks such as `zstd/internal/xxhash` or
`third_party/forked/golang` keep their own attribution. Source, configuration,
and test-data formats such as `.go`, `.c`, `.sh`, `.rego`, YAML, JSON, and XML
are not notices and are not copied outside a `LICENSES` directory, even when
their names mention a license. Each module still needs a non-source license or
copying file at its root. Unreadable or empty build information, a module
missing from the cache, a replaced module, or a module without that root
license fails the build.

The installer writes `notices-layout` containing
`code-polishy-trivy-notices-v2` only after every module's notices are copied.
The marker names the notice rules above, not a digest; it distinguishes a
complete carrier from one copied under earlier rules or interrupted, which the
identity alone cannot. A missing or different marker makes the installer
rebuild the carrier, and release installation, the Windows release build, and
SBOM composition refuse it. There is no compatibility path for earlier
layouts; changing the copy rules requires a new marker value.

Release manifest version 9 records Trivy's version as a tool. The release SBOM
reads the carried binary's build information, lists Trivy with every linked
module as its dependency, and records each module's root and nested notice
paths. A release whose carried binary lacks build information, whose module
lacks a root license notice, or whose notices lack the current layout marker
cannot produce an SBOM.

The resulting graph has the same 375 dependency versions and checksums as the
official binary in
`docker.io/aquasec/trivy@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969`.
Every one was published to the module proxy by 2026-08-13; the newest is
`golang.org/x/mod@v0.40.0`. A rebuild from the populated module cache with no
network proxy or checksum database produced the same binary
(`60a36e8a86456cbbc20a097f14552354001047a1a65b93ca1c0a2e84c9220860` for
`linux/arm64`). That rebuild reused the build cache, so it shows identical
inputs and link output rather than a cold reproduction.

At runtime the scanner image copies the carried binary for the Docker server's
platform through a named build context. Docker pulls no scanner image, and the
installed release is only read. A release without a binary for that platform
fails before building.

The scanner's vulnerability findings, trust bundle, and their review dates are
in [Trivy Scanner Assessments](trivy-scanner-assessments.md).
