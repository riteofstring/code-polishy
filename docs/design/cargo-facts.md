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

Cargo can report the physical checkout path while the caller uses an operating
system alias, such as macOS's `/var` or Windows short directory names. Input
containment recognizes both spellings of the repository root, then reads the
relative path through the existing contained regular-file boundary. Only the
root is resolved: links within source paths remain invalid, and absent inactive
remote targets retain their ordinary missing-file semantics.

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

Published dependencies can retain Cargo metadata for tests, benches, examples or
binaries that their archives intentionally omit. These targets do not build when
the package is consumed as a library dependency. Their declared identities and
contained paths remain facts, but missing files do not invalidate dependency
resolution. Library, procedural-macro and build-script sources must still exist,
as must every local package target. Existing non-regular inputs, symlinks and
escaping paths remain invalid even for inactive remote targets. This distinction
does not authorize executing a dependency's absent development targets.

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

## Parsed source facts

Rust source syntax is parsed by the upstream Tree-sitter Rust grammar in the
already governed CPython helper runtime. The grammar and its Python binding are
exact dependencies in this module's manifest and lock. This supplies syntax,
comments and structural measurements without importing or compiling project
code. Cargo and rustc remain authoritative for crate identity, resolution,
conditional compilation, macro expansion and type correctness. Syntax facts
must not masquerade as a compiler-resolved semantic graph.

Requests carry bounded UTF-8 source bytes through the common structured runner;
the parser does not choose additional project files or execute Rust attributes
and macros. Error-recovered or incomplete syntax cannot establish complete
source coverage. Parsed comments are distinct from comment-like raw strings;
attributes and macro token trees retain their original source locations.

Source selection is partitioned in order into requests of at most 4096 files and
8388608 encoded bytes, including JSON escaping and envelope overhead. These are
per-command limits, not a repository-size ceiling. Each batch is part of the
ordinary command plan and validates its exact source inventory. An individually
oversized source or a failed batch produces coverage findings for the affected
paths without discarding valid siblings. Cancellation prevents later batches
from starting and leaves their files explicitly uncovered.

Locations unpack Tree-sitter's point tuples and translate byte columns into
original Unicode coordinates. The pinned Python binding's
[point attribute getters](https://github.com/tree-sitter/py-tree-sitter/blob/v0.26.0/tree_sitter/binding/point.c)
return borrowed integer references, which can corrupt memory when rows or columns
exceed CPython's small-integer cache. Tuple unpacking uses the normal owned-reference
sequence interface; large-file and Unicode-coordinate fixtures exercise the real
parser without modifying its wheel or weakening source coverage.

Source complexity is measured on written syntax, not expanded macro output.
Functions and closures have independent measurements. Conditions, loops, Boolean
short circuits, guards, or-patterns, `?` and let-else add decisions; an exhaustive
match adds one fewer decision than its arm count. Macro sites remain explicit
facts, and their opaque token trees do not become guessed executable syntax.
The compiler still checks the selected expansion. Source metrics include written
configuration branches rather than claiming coverage of every compiled variant.

Rust owns separate `quality.complexity.rust` and `rustTest` limits, initially 12
and 20 with the same strict-below comparison as Go. Projects may tighten them.
Built-in test functions and exact `cfg(test)` scopes receive the test limit;
file-level test ownership alone cannot relax production functions co-located
with inline tests. A conditional test attribute does not establish test-only
ownership: its function still receives the production limit unless an enclosing
test-only scope already excludes production builds. Custom test attributes and exclusive Cargo test-target scope
need further test-context evidence. Depth and parameter measurements are retained
as facts; TypeScript's thresholds do not silently become Rust restrictions.

The `rust-source/v2` contract separates production/test context from built-in
test-entry classification. Helpers and closures inside a test scope retain test
complexity limits without becoming runnable test functions. Direct `test`
attributes and nested `cfg_attr` test declarations retain distinct entry facts;
compiler execution separately establishes which configuration actually ran.
Missing context, entry classification or body evidence invalidates syntax facts.
Graph validation and the public report schema require the same protocol.

Native Rust lint rejects a declared built-in test whose body contains no
statements other than comments, attributes and empty semicolons. The finding uses
the existing `quality.testEvidence` rule and the original function location.
Ignored or conditionally enabled tests cannot conceal empty bodies. Ordinary
functions, helpers and closures are not test entries merely because their file
or enclosing scope contains tests. This is the same high-confidence vacuity
boundary as Go's empty-test checks, not proof of assertion strength, nontrivial
helper behavior, custom framework expansion or runtime execution.

Comment policy uses parsed line/block comments and value-bearing doc attributes,
not text searches. Metadata such as `doc(hidden)` is not prose. Inline `allow`,
`expect` and `rustfmt::skip`, including nested `cfg_attr` forms, cannot bypass
native policy ownership. Exceptions belong to the existing exact policy mechanism
for the underlying finding. Source validation and command failure remain blocking.

## Written module dependencies

Rust architecture consumes parsed module declarations, use trees, aliases,
re-exports and qualified paths. Cargo target roots establish crate identity;
directory names do not. Conventional `mod` paths and literal `#[path]` attributes
must resolve to contained governed source. Imports resolve through module and
item bindings, not a text search for names. The core retains physical source
paths and applies its existing module-direction and source-cycle policy.

This graph describes potential dependencies in written source, including
conditional branches, as native Go import analysis does. It is not a claim that
every branch was compiled. Conflicting conditional module identities, opaque
generated imports, ambiguous bindings and missing source facts make graph
coverage incomplete rather than selecting a convenient edge or reporting an
empty graph. Compiler checks separately validate the selected Cargo configuration.

The graph handles conventional external/inline modules, literal external module
paths, nested use trees, renamed workspace dependencies and local aliases and
re-exports. Function, block, closure, implementation and generic bindings retain
lexical scope; they cannot leak into sibling scopes or substitute for a module's
members. Prelude lookup respects edition, `no_std`, `no_implicit_prelude` and
local shadowing. Qualified module members never inherit the implicit prelude.

Known standard macro arguments are parsed using the same upstream Rust grammar
with their original byte locations retained. This includes assertion/formatting
expressions, vector element/repeat expressions and match patterns/guards.
Nested invocations keep the same resolution checks. Stringified tokens are data,
not guessed imports. Standard derives and macros must retain standard-library
identity; imports, glob ambiguity or macro-use expansion cannot silently stand
in for that identity. This analyzes written arguments, not macro-expanded code
or expanded complexity.

Custom macro definitions/invocations, non-builtin attribute expansion,
conditional/inline path overrides, absolute external-prelude paths and unresolved
external glob bindings currently fail coverage explicitly. These are beta
limitations, not optional exemptions.

A future compiler-semantic provider must retain generated references, hygiene
and original locations as well as written tokens. The Rust Analyzer revision
carried by Rust 1.97.1 builds its LSIF and SCIP indexes by iterating tokens in
physical source files and skipping unresolved classifications. Its
[static index](https://github.com/rust-lang/rust/blob/8bab26f4f68e0e26f0bb7960be334d5b520ea452/src/tools/rust-analyzer/crates/ide/src/static_index.rs)
can resolve written tokens through expansion, but does not enumerate every
reference introduced entirely by generated syntax. A successful navigation index
therefore cannot establish complete macro dependency coverage. General expansion
remains a required semantic-provider milestone; the written graph does not silently
admit those missing edges.

The deferred semantic helper draft is retained at commit `3a973e2` under
`internal/rustfacts/semantic/`, outside the beta source tree. It uses
Rust Analyzer's HIR library to visit expanded syntax, resolve names with macro
hygiene and map references back to physical source. An explicit Cargo manifest,
native target and frozen build command establish its intended input boundary;
partial metadata and failed build-script collection are errors. Its draft
protocol is internal and has no active Go consumer or installed executable.
Standard-library loading, attribute/derive expansion, selected configuration
coverage, complete bounded source ownership, execution integration and distribution
remain unfinished. Its fixtures have not run: dependency admission currently
prevents building this helper. It supplies no architecture coverage yet.

The deferred draft uses Rust Analyzer crates `0.0.346`, the first inspected published
line using Salsa `0.28`, with Salsa and its macro crates locked to `0.28.5`.
Earlier candidates depend on the affected Salsa series in
[RUSTSEC-2026-0308](https://rustsec.org/advisories/RUSTSEC-2026-0308.html).
Registry inspection on 2026-10-15 found 188 registry packages in the candidate
lock, with matching published checksums and no yanked releases. Only Salsa and
its two macro crates remain younger than 30 days. All three become ordinarily
eligible by 2026-10-24 at 09:56:31 UTC. This data-only review neither compiles
the helper nor establishes complete dependency admission.

The currently locked `0.28.3` engine lacks native Cargo governance and
`crates.io` release-age assessments. Its lock command requires an authenticated
HTTPS publication index and archive-backed release identity; a private source
installation alone cannot activate new governing policy. A published engine
with native Cargo enforcement and a separately authorized self-hosting lock
upgrade are required before admitting and compiling this candidate. Waiting for
the fixed dependencies to age avoids an early-admission exception; no demonstrated
deployed-helper exposure currently justifies one. An alternate policy file,
handwritten lock or disabled source check cannot replace that boundary.

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
Native dead-code coverage uses rustc's forced unused-item/import/macro diagnostics
for the selected Cargo configuration. Public library APIs remain legitimate
entry points; a private item used only by a disabled feature is unused in the
checked configuration. This is compiler reachability, not a claim that every
public API, configuration or macro expansion is globally live. Lint, type-check
and dead-code providers cannot split one native compilation unit. Parsed
attribute policy remains independent of compiler collection.
Project Clippy settings currently fail
with an explicit unsupported-configuration diagnostic; they cannot silently
override the release-owned defaults.

The compiler receives the host SDK's executable and library search environment,
but no ambient Cargo/rustup flags, wrappers or Rust compiler selection. Build
scripts and procedural macros may execute during checking. Their output
directories, emitted configuration and native linkage are compiler evidence,
not a claim of filesystem or network sandboxing. Native SDK failures remain
ordinary failed checks; the Rust distribution does not supply every native SDK.
Native Windows CI and release smoke tests explicitly initialize Microsoft's x64
developer shell and verify that the active linker belongs to the selected Visual
Studio installation. The SDK's existing PATH and library inputs reach sealed
Rust commands; automatic discovery alone is not a supported CI prerequisite.

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

## Test runner provisioning

The already governed Python runtime installs the exact cargo-nextest binary on
all supported hosts, using one cross-platform implementation. Checked-in archive
identities are authenticated before reading the executable payload. The installer
extracts only that regular member, with bounded bytes, and runs a staged version
probe before atomic replacement. Corrupt downloads, invalid payloads, failed
probes and replacement failures preserve a previous working executable. Reuse
checks the installed release; it does not hash local state. This provisions a
runner, not test evidence or doctest execution.
