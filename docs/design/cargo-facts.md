# Cargo Project Facts

The `rustfacts` boundary owns Cargo command preparation and metadata validation;
the repository layer owns data-only manifest parsing and tool paths. Quality,
architecture and dependency policy consume facts without acquiring their own
Cargo resolvers. Commands use the common structured runner so limits, cancellation
and command evidence have one owner.

Workspace discovery runs `cargo metadata --format-version 1 --no-deps --frozen`.
It can discover members, targets, inherited metadata, declared dependencies and
features before dependencies are materialized. A null `resolve` is deliberately
retained as absent dependency-resolution evidence. Discovery neither compiles
code nor proves that dependencies or any target configuration have been checked.
The selected manifest and Cargo's actual workspace membership determine scope;
directory naming is never a package identifier.

Discovery retains each package's minimum Rust version as a fact. Cargo enforces
compiler compatibility when the selected package/feature/target configuration is
checked. An unselected member with a newer minimum version must not prevent
discovery, selected formatting or a compatible default-member build.

Cargo runs from the repository root with a sealed environment, absolute pinned
compiler paths and repository-local Cargo home and target output under `.tools`.
Ambient rustup selection, wrappers and user Cargo home are not inherited. Ancestor
Cargo configuration outside the checkout must not silently enter analysis.
Repository configuration is parsed before execution; unsupported execution-altering
settings produce a diagnostic. This controls discovery inputs, not operating-system
access by future build scripts or procedural macros.

## Locked resolution

Dependency verification uses full version-1 Cargo metadata separately from
workspace-only discovery. Online supply-chain checks and pre-install review use
`--locked`: Cargo may fetch the already locked sources into its governed home,
but cannot update the lock or execute build scripts and procedural macros.
Offline verification uses `--frozen` and requires that cache to be prepared.
Both modes require a contained, parsed workspace lock before resolution runs.
Neither repairs a missing or stale lockfile.

Resolved facts preserve opaque package IDs, source identities, renamed edges,
dependency kinds, target predicates and enabled features. Every graph node and
edge must refer to a real package, and resolved package coordinates must occur
in the reviewed lock inventory. Cargo owns resolution; this validation does not
reimplement its resolver. Remote manifests and source targets must remain in the
governed Cargo home. Local path packages retain contained checkout paths.

Metadata uses default features and retains target-specific edges for all
platforms. It is not evidence that every feature or target was compiled. The lock
may also include inactive optional dependencies absent from this resolved graph;
dependency age and advisory inventories must not drop those locked packages.
Commands belong to the existing supply-chain execution plan, including offline
gates, so receipts and cancellation have the same owner as other languages.

## Selected formatting

The pinned rustfmt receives one selected source through stdin, with the edition
reported by Cargo and an explicit contained configuration file. It never receives
a source filename that would authorize recursive formatting of child modules.
Configuration discovery stops at the checkout boundary; absent project settings
use the release-owned default. Stable project style settings are honored, but
formatting exclusions, disabled formatting, edition conflicts and unknown or
unstable settings fail visibly.

Formatting excludes generated/data/link inputs and writes through the existing
contained regular-file boundary. The current bytes are compared with the original
input immediately before writing, preserving concurrent caller edits without a
local-state digest. Read-only formatting, like Go's gofmt check, is an internal
source operation; Cargo discovery and compilation use the check execution plan.
The public format operation routes all child processes through its supplied runner.

## Compiler checks

Compilation uses the matching pinned cargo-clippy executable directly, with an
absolute Cargo path. This bypasses Cargo's ambient external-subcommand lookup.
The common runner captures Cargo's JSON events with bounded output and process
tree cancellation. Cargo must retain its reviewed lock and available dependency
cache through `--frozen`; a missing lock is a preparation failure, never an
invitation for the check to resolve dependencies.

Checks select the manifest's ordinary Cargo packages, default features and all
enabled targets, using the governed native host explicitly. They do not enable
every feature or establish coverage of other target configurations. Compiler
warnings, Clippy's default lint group and unused-code lints use `--force-warn` at
the invocation boundary. Source `allow`, `expect`, `deny` and `forbid` attributes
cannot hide these diagnostics or turn policy lint collection into a compiler
failure. Code Polishy treats the resulting typed findings as blocking errors,
subject to its existing exact exception policy. Failed compilation always adds
a project failure, even when the compiler also reports typed source diagnostics;
an excepted lint cannot make a failed build or operational invocation pass.
Rust does not support forcing the aggregate `warnings` group. The release owns
the concrete warning-by-default lint inventory for its pinned compiler, checked
against that compiler's advertised lint interface during toolchain tests. Rust
compiler lints that default to deny retain their failure semantics; the policy does
not lower them to warnings or enable every opt-in restriction lint.
The advertised `tail-call-track-caller` warning requires unstable explicit tail
calls in this release and is not selectable on the supported stable toolchain.
Its exclusion is verified against the real compiler, not silently ignored at
runtime.
Parser-backed attribute governance and complete dead-code coverage remain
separate requirements. Project Clippy settings currently fail
with an explicit unsupported-configuration diagnostic; they cannot silently
override the release-owned defaults.

The compiler receives the host SDK's executable and library search environment,
but no ambient Cargo/rustup flags, wrappers or Rust compiler selection. Build
scripts and procedural macros may execute during checking. Their output
directories, emitted configuration and native linkage are compiler evidence,
not a claim of filesystem or network sandboxing. Native SDK failures remain
ordinary failed checks; the Rust distribution does not supply every native SDK.

Structured success requires a final Cargo outcome and a compiled target for
every selected package. An empty stream, unrecognized output, partial compilation
or missing target is not a pass. Findings retain package/target/compiler context
and contained original source locations, including available macro call sites.
Diagnostics outside available source remain attached to the selected manifest
with the reported source identified; they are not discarded or assigned an
invented line. Workspace metadata, not parsed package-ID strings, owns identity.

[Rust input coverage](rust-inputs.md) defines control input selection and the
governed toolchain. The [implementation plan](../plans/code-polishy-rust-support-plan.md)
records the remaining Rust capabilities and acceptance work.
