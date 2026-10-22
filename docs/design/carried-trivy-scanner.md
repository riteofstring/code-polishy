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
`-trimpath`. The result is accepted only when its build information names
`go1.26.10-X:jsonv2` and exactly that module version and sum. The pinned module
content includes Trivy's `go.sum`, so the module graph is frozen. The
installer keeps the binary and Trivy's Apache-2.0 `LICENSE` in
`.tools/trivy/linux-<arch>` for the host architecture; Windows releases carry
`linux-amd64`. Release installation refuses a carried binary whose module
version differs from `tools/trivy-version.txt`, and the release manifest covers
its bytes, so the runtime trusts the installed release rather than rechecking
them.

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
