# Optional Git assessments

Local dependency review must work without a custom attestation service.
`supplyChain.gitEvidence.required` explicitly selects the stronger signed
assessment policy; its default is false. Provider declarations alone do not
activate that policy. Disabled evidence is neither read nor credited in gate
receipts, so stale artifacts cannot imply verified coverage.

Public registry scans cannot establish arbitrary Git-content security or an
authenticated publication date. The default therefore reports a nonblocking
Git-source coverage warning. It preserves exact pin and lock checks and runs
public registry vulnerability and minimum-age checks even in mixed Git and
registry locks. Public scan projections omit Git coordinates and never invent
registry versions or publication dates for them.

Cargo uses the same contained public-projection boundary, with its own complete
locked-package report checks described in [Rust registry advisories](rust-inputs.md#registry-advisories).
Its native lock extractor is disabled because the pinned scanner does not retain
Cargo source identity. Cargo locks also use the existing signed assessment
protocol when required, without substituting registry evidence for Git contents.
The complete remote inventory retains crates.io coordinates separately from Git
coordinates, including multiple packages in one Git repository and inactive
optional/platform releases. Cargo supplies no lock-level package subdirectory;
the Git subject covers the complete repository snapshot at the exact revision,
with one named subject per package. No directory is guessed from its crate name.

Gate evidence inventories both Cargo and uv locks through one bounded, data-only
reader. The combined inventory shares the 512-lock and 32-MiB limits; Cargo also
retains its stricter 8-MiB per-lock parser bound. Unsupported sources prevent
complete assessment. Each signature and inventory is authenticated once per
evidence snapshot and its identity is reused in the receipt binding. Disabled
policy reads neither ecosystem's signed artifacts. Exact declarations and Cargo's
separate frozen/locked resolution still establish agreement with the manifests.
Public crates.io scanning and release-age checks remain active for mixed locks.

Opting in retains complete inventory binding, signature verification, scanner
trust, expiry, license, vulnerability, and authenticated-age requirements.
Missing or invalid evidence blocks the selected policy. Its receipt remains
part of gate identity and freshness validation.

See [Git dependency evidence](../git-dependency-evidence.md) for configuration
and the signed assessment protocol.
