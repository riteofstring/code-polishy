# Rust and Shader Input Coverage

Rust compilation settings affect which code and diagnostics exist. Cargo
configuration can change source registries, rustflags, wrappers, linkers and
runners; rustup declarations change the selected toolchain; rustfmt and Clippy
settings change analysis. Their standard filenames are therefore sensitive
control inputs at every repository depth, alongside Cargo manifests and locks.
This includes both `.cargo/config` and `.cargo/config.toml`, `rust-toolchain`
and `rust-toolchain.toml`, `rustfmt.toml` and `.rustfmt.toml`, and `clippy.toml`
and `.clippy.toml`. `.config/nextest.toml` is also a control input because its
profiles can change test selection and execution.

The existing repository selection boundary expands changed or deleted control
inputs to full governed scope. This is deliberately conservative until Cargo
workspace and configuration resolution can prove a narrower affected closure.
Recognizing a filename does not parse, approve or execute its settings, inspect
configuration outside the repository, or establish reproducible Cargo execution.

Cargo manifest inspection uses a pinned Go TOML parser and the existing bounded,
contained regular-file reader. It parses package and virtual-workspace headers
without invoking Cargo, importing project code or executing build scripts.
Unknown manifest fields remain available to Cargo rather than being rejected
by an incomplete schema. Malformed selected manifests produce original-path
diagnostics without discarding successfully parsed siblings or inspecting
unrelated projects during a focused check. This inventory is not a resolved
workspace, target, feature or dependency graph; those facts belong to Cargo.

## Dependency declarations

The same data-only reader preserves registry versions, renamed packages, local
paths, Git selectors and workspace inheritance across ordinary, optional, build,
development and target-specific dependency tables. Workspace definitions and
patch/replace tables are inventoried too. Source identity comes from declaration
fields, not directory names or a guessed Cargo package ID. Inherited declarations
retain the inheritance marker until Cargo establishes their effective workspace;
the reader never fills in a guessed version.

Native pin policy requires exact `=major.minor.patch` registry requirements,
including prereleases when selected. Cargo ignores build metadata in requirements,
so it cannot provide an exact artifact pin. Version constraints accompanying a
path or Git dependency must also be exact because they govern registry resolution
when the crate is published. Local dependency manifests must be contained regular
inputs; Git declarations need a full immutable revision and the existing validated
Git repository identity. Pin checks do not authenticate Git contents or invent a
publication date. Cargo follows the shared [optional Git assessment
policy](git-evidence-policy.md); signed evidence is required only when selected.

Source substitutions, unsupported dependency fields and alternate registries need
explicit source governance. Static manifest checks neither install dependencies
nor run build scripts. They do not establish lock agreement, vulnerability, license
or release-age coverage; those remain separate dependency-policy operations.

The existing `dependencyOverridePolicies` contract governs Cargo's root
`patch.crates-io` table with ecosystem `cargo`. Its canonical JSON identity is
computed from the parsed TOML table, retaining every declaration field. Changed,
missing, expired and unused ownership remains visible. Supported substitutions
select contained local package manifests or exact Git revisions; other patch
registries and deprecated `replace` declarations remain unsupported.

A governed local replacement is consumed as a dependency of the declaring
application. Its registry requirements are transitive constraints resolved by
that application's lock, so copying a dependency into the repository does not
require rewriting its upstream constraints or creating a standalone lock.
Consumer declarations retain exact-version policy, and replacement Git and path
sources retain containment and immutable-pin checks. Cargo must prove each
replacement's actual source and package identity in the consumer graph; a
workspace member cannot obtain transitive treatment through a patch declaration.
This does not authorize the replacement's own development targets or establish
an independently buildable library project.

Local replacement source remains governed repository code. A visible source
coverage warning distinguishes review of its provenance and changes from the
public registry's age, advisory and license evidence. The override owner must
record that review and its maintenance plan; a matching table digest neither
authenticates the copied source nor proves it unaffected. Registry dependencies
in the complete consumer lock retain their ordinary age and security checks.
The [consumer resolution](cargo-facts.md#locked-resolution) supplies the actual
source binding without compiling the replacement or executing its build scripts.

Lock inventory supports Cargo's version-3 and version-4 records. A package
identity retains its name, complete version and exact source; different versions
or sources are not collapsed. Git-index and sparse registry records must carry
their archive checksum,
which is authoritative artifact identity rather than a local change detector.
Dependency-edge strings remain opaque until Cargo supplies resolved graph facts.
The version-bound source decoder recognizes the pinned Cargo release's public
crates.io identity and immutable Git source encoding. Unknown sources fail
explicitly and never become public registry queries. Git packages sharing a name
and version with a registry crate remain separate coordinates. Parsing this
inventory alone does not prove that it agrees with the selected manifests.
Native lock agreement is established separately through [governed Cargo
resolution](cargo-facts.md#locked-resolution), using the actual workspace lock
and the existing supply-chain runner. Pre-install review may fetch locked source
data; offline verification may not. Age, security and license coverage still
require their respective evidence, not merely successful Cargo resolution.

## Registry release age

Online age checks cover every public crates.io package in the complete Cargo
lock, including inactive optional and platform-specific dependencies. Requests
include the authoritative workspace locks returned by Cargo even when a selected
file list only names member manifests. Existing resolved lock facts are reused
within the operation; independently inventoried locks are read through the same
contained boundary. Requests batch exact versions by crate across lock scopes
and use crates.io's publication
timestamps. The sparse index does not provide those timestamps; downloading the
entire registry database is disproportionate for a project-sized inventory.
The [crates.io API](https://crates.io/api/openapi.json) supplies this evidence.

The client follows the registry's [data-access policy](https://crates.io/data-access):
identified requests, at most one request per second within the process, bounded
responses and no metadata redirects. Waiting callers honor cancellation without
reserving future request slots. Missing timestamps, unexpected identities,
malformed responses and request failures remain missing evidence, never age passes.
The existing exact release-age assessment policy supports the `crates.io`
ecosystem and the actual Cargo.lock scope. It cannot fill missing observations.

Git, local paths and alternate registries never become public crates.io queries.
Optional Git assessment policy retains its visible coverage warning; required
Cargo Git evidence uses the shared signed inventory protocol for complete Cargo
locks, with exact repository/commit/package identity and no guessed subdirectory.
Registry age evidence does not establish vulnerability or
license coverage and is not collected by the offline profile.

The [historical dependency comparison policy](historical-review-policy.md#cargo-dependency-comparison)
keeps declaration changes separate from effective Cargo resolution. Git lock
entries may record a commit without a declared ref; that commit is preserved
without inventing a branch or treating the commit as a declaration from the
manifest. Historical moving refs remain reviewable but do not become exact pins.

## Registry advisories

The pinned OSV-Scanner provides native advisory coverage for complete public
Cargo lock inventories, independent of the currently enabled features or host.
Its [version-bound Cargo extractor](https://github.com/google/osv-scalibr/blob/164402d9140e/extractor/filesystem/language/rust/cargolock/cargolock.go)
does not preserve source identity. Recursive scans therefore disable both Cargo
manifest and Cargo lock extraction. A bounded, data-only projection supplies
only exact public crates.io coordinates from the governed lock reader. Private
registries and unsupported source encodings fail before public scan planning;
local and Git packages never become registry requests. Optional Git policy keeps
its visible warning, and requiring Git evidence does not hide public registry
advisories from a mixed Cargo lock.

Cargo uses captured public inventories and the common scanner runner. Separate
locks preserve their own original finding scopes. Planning does not write
artifacts; execution supplies the captured bytes through an isolated read-only
snapshot with explicit scanner configuration. No transitive resolution or Rust
call analysis is enabled, so an advisory scan does not execute build scripts or
procedural macros.
The shared [security evidence policy](security-evidence.md) defines which
online commands may reuse raw results and how their complete inputs are bound.

Scanner scope must contain the actual workspace lock established by Cargo. A
member-only scope cannot claim an ancestor lock's public dependencies; it needs
workspace-level OSV coverage or a governed security provider.

Projected Cargo scans request every package in the result. Missing, repeated,
foreign or wrong-version coordinates fail coverage, including an empty result for
a nonempty input. Scanner errors do not become clean reports. Complete advisory
reports retain aliases, exact versions, severity and Cargo.lock scope through the
existing report-bearing exit and vulnerability-assessment policies. Assessments
use the `crates.io` ecosystem and cannot approve a different lock or version.
The pinned runtime contract is exercised against a temporary synthetic offline
advisory database, without querying a live provider.

RustSec also publishes [informational advisory categories](https://github.com/RustSec/advisory-db/blob/main/README.md#advisory-format).
An `unmaintained` notice identifies a maintenance concern rather than an
unspecified exploit. The OSV reader retains affected-package metadata and emits
`supplyChain.osvUnmaintained` warnings with exact package, version, advisory,
aliases and original lock scope. Only RustSec records explicitly classifying
the matching crates.io package as unmaintained, without group severity,
advisory/affected severity or a CVSS score, qualify. Every grouped identifier
must be covered by such a record. Missing metadata, contradictory matching
records, CVE identities, security aliases, other ecosystems and `unsound` or other notice
categories remain blocking vulnerability findings. A maintenance notice cannot
mask a separate vulnerability for the same package or require an invented
non-exposure assessment. Complete warning-bearing scanner exits retain the same
protocol checks as vulnerability-bearing exits; operational failures still fail.

## Configured license policy

When `supplyChain.allowedLicenses` is configured, installed-license verification
checks every public crates.io package in each complete lock, including inactive
optional and platform dependencies. It reads the published Cargo.toml from cached
`.crate` archives under the governed Cargo home. The lock's authoritative archive
checksum authenticates the exact bytes; opaque registry directory names never
establish source identity. Each candidate archive is read and authenticated once
per operation, and only observations matching the requested lock checksum can
satisfy that lock. Separate lock scopes retain separate findings.

This follows the pinned Cargo cache layout described by [Cargo Home](https://doc.rust-lang.org/cargo/guide/cargo-home.html),
whose internal layout is version-bound rather than a stable public API. Inputs
must be contained regular files beneath physical cache directories. Reads and
decompression are bounded: 256 registry directories, 128 MiB per archive, 1 GiB of
archive input per operation, 512 MiB per decompressed stream, 65,536 entries and
8 MiB per manifest. The shared license-facts timeout also applies. Missing,
corrupt, ambiguous or mismatched manifests are missing license coverage, not
permission to use an installed directory's unverified metadata.

The common SPDX evaluator decides declared expressions, including `OR`, `AND` and
`WITH`. It does not guess a license from `license-file` names or infer one from
text. Cargo resolves workspace inheritance when it produces the published
manifest; a real pinned-Cargo packaging fixture verifies that boundary without
running build scripts. Local packages and Git contents do not become public
registry license claims; Git follows its separate optional assessment policy.

Like Node, dependency review defers installed-license evidence until after the
candidate has been prepared. No license policy means no license scan. Ordinary
supply-chain verification reads prepared archives without downloading, unpacking
or executing them. Missing archives can be prepared with the pinned Cargo's
[`fetch --locked`](https://doc.rust-lang.org/cargo/commands/cargo-fetch.html) using
the governed Cargo home; this includes all lock targets without compiling code.

## Shader inputs

WGSL is executable shader source with its own built-in language identifier,
`wgsl`. It participates in module ownership, source-comment coverage and the
existing required analysis capabilities. A shader cannot be protected as data
or silently acquire Rust analysis coverage. Generated shader source retains
correctness and ownership obligations under the existing generated-source rules.
Recognition supplies no analyzer: absent structured evidence still produces
coverage findings, even when a generic Cargo command is configured.

Ordinary shader changes retain focused file selection and the module dependency
closure. Projects express renderer-to-shader ownership through existing module
dependencies. Compiler-derived shader consumer edges and complete Rust receipt
inputs belong to later semantic integration; file recognition alone proves
neither shader validity nor GPU execution correctness.

The [Rust support plan](../plans/code-polishy-rust-support-plan.md) records the
remaining discovery, analysis, dependency, test and installed-release work.

## Governed toolchain

The initial compiler is Rust 1.97.1, released 2026-07-16. Cargo, rustc, rustdoc,
rustfmt and Clippy come from that same official release, alongside the host
standard library. The pin is independent of a project's declared minimum Rust
version. The beta targets editions 2015, 2018, 2021 and 2024 on the existing
five Code Polishy installation hosts; additional compilation targets require
separate component and SDK evidence.

The [upstream 1.98 regression](https://github.com/rust-lang/rust/issues/161441)
does not reproduce on 1.97.1. The initial pin avoids both that affected release
and an under-30-day toolchain admission. Checksums come from the
[official 1.97.1 manifest](https://static.rust-lang.org/dist/channel-rust-1.97.1.toml).
Installers authenticate component archives, copy their payloads without executing
installation scripts, preserve bundled licenses, and stage a complete runtime
before replacing the previous installation. They do not install rustup or select
an ambient channel. Release publication must carry the runtime and its exact pin;
the developer installer alone does not establish installed-release coverage.

[Cargo project facts](cargo-facts.md) defines workspace discovery and its execution
boundary separately from source classification.

The source-facts parser uses Tree-sitter 0.26.0 and its Rust grammar 0.24.2 through
the existing pinned CPython runtime. Both upstream releases are older than the
minimum admission age and provide binary wheels for all five supported hosts.
The Rust facts manifest and `uv.lock` own dependency versions and archive
identities; the installer reads those lock entries rather than maintaining a
second checksum inventory. It selects only compatible binary wheels, authenticates
each download once, and never runs package installation scripts or source builds.
Staged packages must pass a real Rust parse before replacing the previous exact
distributions. Replacement retains recoverable backups until the installed
parser passes that same check. Wheel metadata and licenses remain installed.

Native formatting parses the selected handwritten source before invoking rustfmt.
Direct and nested conditional `rustfmt::skip` attributes, including the
upstream [macro and attribute exclusion forms](https://github.com/rust-lang/rustfmt#tips),
fail at their original locations and their files remain untouched. Raw-string contents are data, not
attributes. The formatter consumes the same captured bytes that were parsed and
compares those bytes with the current file before writing; a concurrent edit
cannot introduce an unchecked exclusion or be overwritten. Invalid or missing
syntax evidence does not authorize a write. Generated and unselected inputs remain
outside this operation, and valid selected siblings can still be formatted.

Rust unit and integration execution uses the independent pinned cargo-nextest
0.9.138 release, published 2026-06-21. Its stable JUnit output supplies actual
test-case outcomes on the stable compiler, whose built-in libtest JSON interface
still requires unstable options. The binary archives cover the same five hosts;
the macOS archive is universal. Checked-in identities come from the exact
[upstream release](https://github.com/nextest-rs/nextest/releases/tag/cargo-nextest-0.9.138).
Installation must authenticate archives and verify the staged executable before
replacing a working tool. No Cargo source build, install script or self-update
participates. Nextest does not run doctests; it cannot supply doctest coverage.
Separate native doctest suites use the same pinned Cargo and rustdoc, with the
version-bound evidence adapter defined in [Rust test evidence](rust-tests.md).
