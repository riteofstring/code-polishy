# First-class Rust support in Code Polishy

Date: 2026-09-24. Status: beta implementation in progress.
Implementation branch: `beta-rust-support`, based on `main` at
`d6d14503ec561e54e61c36beb218f7ce684f75fe`, locked to Code Polishy `0.27.11`.
Imported from Masterbrainer's `docs/plans/code-polishy-rust-support-plan.md`
at planning commit `be593fb`. This copy owns the Code Polishy implementation.

## Outcome

Rust receives the same product-level integration as Go and Python: automatic
discovery, policy-owned tooling, source facts, architecture, quality, dependency
governance, tests, reports, receipts, documentation, and verified releases.
Target repositories must receive complete Rust support through normal Code
Polishy setup and upgrade, without a separate pack-installation procedure or a
hand-maintained Cargo policy stack. Automatic installation of an authenticated,
release-owned Rust provider is compatible with this outcome; an optional pack
or a few Cargo commands alone are not.

The Go policy engine remains the decision owner. A pinned Rust helper may supply
compiler/parser facts through a bounded child-process protocol. Do not embed
language-specific heuristics in unrelated engine modules or reimplement Rust
resolution with regular expressions.

## Current baseline and architectural decisions

The implementation base includes the accepted changes through `0.27.11`.
The earlier handoff inspected a divergent `0.25.0` checkout. Its unpublished
behavior-review changes are not part of this base.

The current [first-party language-pack plan](installable-first-party-language-packs.md)
and [universal discovery plan](universal-language-pack-capabilities.md) are
proposals, not implemented contracts. Rust should reuse the existing ownership,
selection, runner, report, and receipt boundaries without making the complete
four-language extraction a prerequisite. Add only the Cargo-specific facts and
execution integration needed by Rust; coordinate any public provider-protocol
change as one coherent cutover. No second scheduling or policy engine.

The current provider runtime resolves Node, and its discovery model is shaped
around JavaScript projects. Neither protocol version 3 nor a generic command
declaration establishes Cargo discovery, a Rust runtime, or a sandbox.
`runner.OSRunner` supplies process/environment controls; operating-system
filesystem and network containment still needs an explicit implementation and
host evidence before it can be advertised.

Implementation uses the locked agent workflow and mapped design context.
Intermediate beta commits may be incomplete; successful source classification
does not establish analyzer coverage. Keep the locked stable release unchanged
until a separately authorized release/upgrade. Publication and Masterbrainer
adoption are later operations.

## Baseline parity inventory

This is a source audit at the recorded base, not execution evidence. The fixture
column defines acceptance work; unchecked capabilities remain unavailable.
Package paths below are under `internal/` in this repository.

| Capability             | Current Go/Python baseline and owner                                                                                                                     | Rust facts, enforcement, and limits                                                                                                                    | Required observable fixture                                                                                    |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- |
| Discovery              | `repository/go_modules.go` parses contained module/workspace identities; `repository/python_project.go` assigns projects and reports unsupported layouts | `.rs` is recognized, but Cargo has no built-in project model; add parsed manifests and versioned Cargo package/target facts with contained ownership   | Virtual workspace, inherited package metadata, nested excluded package, escaping path, standalone source       |
| Input selection        | `policy/control_inputs.go` and `repository/repository.go` expand policy-sensitive changes; `repository/impact.go` follows declared reverse dependencies  | Cargo manifests/locks already expand selection; Rust toolchain/analyzer configs and WGSL classification are missing; precise Cargo closure comes later | Editing or deleting config reselects affected work; shader change reaches its owning module and dependents     |
| Formatting             | `quality` owns Go formatting and Python conditional commands; selected writes protect generated/data inputs                                              | Verified rustfmt, exact edition/config, and selected write authority; formatting a module must not rewrite its unselected children                     | Selected dirty source formatted, unchanged sibling/generated bytes preserved                                   |
| Correctness            | `quality` and `policymodule` compose native Go and Ruff/ty checks                                                                                        | Compiler/Clippy diagnostics with original paths and target/features; build-script and macro execution require controlled compilation                   | Valid crate and seeded type/lint defect, unavailable compiler, forbidden config suppression                    |
| Structure/comments     | `quality`, `pythonfacts`, and `pack` supply parsed measurements/comment facts and core limits                                                            | Parser facts for functions, depth, parameters, comments and lint attributes; declare Rust limits rather than quietly inheriting TypeScript conventions | Excessive complexity, doc comment under strict policy, raw string containing comment-like text                 |
| Architecture           | `architecture` and `architecture/sourcegraph` derive native imports and enforce owned edges/cycles                                                       | Cargo graph supplies inter-crate edges only; compiler/parser facts must account for intra-crate modules, cfg, re-exports and macros                    | Forbidden cross-module edge, valid alias, unresolved generated edge with explicit incomplete coverage          |
| Dead code              | Native Go tooling and Python Vulture/reachability contracts have language-specific limits                                                                | Compiler and entry-point evidence per configuration; public library APIs and macro expansion need stated limits                                        | Unused private item, retained public API, feature-only use; absent evidence cannot pass                        |
| Supply chain           | `supplychain` governs Go and Python/uv dependencies; unsupported Cargo manifests already report missing providers                                        | Exact declarations, lock agreement, source identity, age, licenses and advisory assessments; candidate resolution is separate from compilation         | Unpinned dependency, inherited dependency, lock drift, rejected Git provenance, stale assessment               |
| Test evidence          | `testing`, `testartifact`, and `testreceipt` own suite selection, artifacts and reuse                                                                    | Add a supported structured Rust result adapter; existing generic command exits/artifact shapes alone do not prove nonempty test execution              | Zero/all-skipped tests rejected, actual failed test, doctest result, cancellation, stale receipt               |
| Portability/shaders    | `portability` owns generic machine assumptions; no WGSL analyzer exists                                                                                  | Separate WGSL language and validator; host/target, FFI, unsafe and SDK evidence; GPU correctness stays project-owned                                   | Invalid shader, missing SDK, unsupported cfg branch, changed shader invalidating dependent evidence            |
| Distribution/reporting | `release`, `engine`, CLI, schemas and installed-release scripts own verified artifacts and reports                                                       | Deliver exact Rust tools/helper automatically on the existing five hosts; report coverage gaps and do not claim unsupported hosts                      | Public CLI through verified Linux/macOS/Windows installation, JSON/SARIF findings and unavailable-tool failure |

## Implementation milestones

1. **Input coverage foundation:** reserve WGSL as shader source; classify Cargo,
   rustup, rustfmt and Clippy configuration as policy-sensitive; prove real
   selection, ownership and missing-analysis failures. This does not resolve
   Cargo projects, execute Rust tools, validate shaders, or establish Rust parity.
2. **Cargo facts and sealed discovery:** introduce a data-only manifest reader
   and bounded Cargo metadata model alongside an exact governed toolchain.
   Deliver the first usable public-CLI discovery path together; do not leave
   an unused metadata abstraction waiting for future callers.
3. **One complete crate path:** for a contained crate with declared
   target/features, wire formatting, compiler checks, core quality/architecture,
   dependency policy and actual test evidence into existing commands. Unsupported
   layouts remain explicit failures. This is a beta subset, not general support.
4. **Workspace and semantic coverage:** expand the supported configuration
   matrix to the Phase 1–5 cases below, including WGSL and native boundaries.
5. **Installed-release parity:** prove every acceptance cell on the supported
   hosts before advertising first-class support and adopting it in Masterbrainer.

Milestones 2–4 cut across the phases below. Read-only manifest inventory can
precede tool execution, but resolved metadata cannot be admitted before the
tool/configuration boundary is ready. Reuse existing exact test suites for
changed modules; add a quick suite when a new module is introduced.

## Phase 0: define the complete coverage contract

- [x] Audit current Go/Python owners and Rust gaps against the recorded base;
      establish the parity inventory above.
- [ ] Turn each acceptance cell into executable evidence, preserving explicit
      unavailable/incomplete states and original-path diagnostics.
- [ ] Choose the supported Rust editions, compiler/MSRV relationship, target
      triples, and feature configurations. Select exact versions at implementation
      time; do not invent pins in this plan. Target editions 2015, 2018, 2021 and
      2024 subject to that toolchain's supported grammar. Code Polishy installation
      hosts remain Linux/macOS on x64/arm64 and Windows x64; a host install does not
      prove every Rust compilation target or feature combination.
- [ ] Define a first-class Rust conditional module activated by governed Rust
      source/Cargo workspaces. Unsupported or missing required analysis must block
      completion rather than fall back to a successful generic command.

| Capability   | Required built-in behavior                                                                     |
| ------------ | ---------------------------------------------------------------------------------------------- |
| Discovery    | Cargo workspace/package/target ownership, nested projects, standalone source coverage findings |
| Formatting   | Policy-owned rustfmt, controlled configuration, selected writes only                           |
| Correctness  | rustc/Clippy diagnostics with target/feature context and original locations                    |
| Structure    | Complexity, size, depth, parameter/comment rules using parsed facts                            |
| Architecture | Resolved module/crate edges and actual dependency direction                                    |
| Dead code    | Honest compiler/reachability evidence with explicit public/macro limits                        |
| Supply chain | Exact declarations, lock resolution, age, vulnerabilities, sources, exceptions                 |
| Tests        | Selection, affected closure, execution counts/outcomes, ownership and evidence                 |
| Portability  | Native targets, conditional compilation, host requirements, FFI                                |
| Release      | Verified Rust tools/helpers on every supported Code Polishy host                               |

## Phase 1: discovery and Cargo model

- [ ] Parse manifests as data, then use versioned Cargo metadata for resolved
      facts. Model virtual workspaces, members/excludes/default members, inheritance,
      renamed/path/git/registry dependencies, target-specific dependencies, features,
      build dependencies, proc-macro crates, examples, benches, tests and doctests.
      Use `--format-version 1`; package IDs and source IDs are opaque. Accept
      additive metadata fields within the supported format, while validating the
      fields actually consumed. `--no-deps` has no resolved dependency graph;
      `resolve: null` cannot mean an empty or successfully checked graph.
- [ ] Own `.rs`, `.wgsl`, `Cargo.toml`, `Cargo.lock`, `rust-toolchain` and
      `rust-toolchain.toml`, both `.cargo/config` and `.cargo/config.toml`,
      `rustfmt.toml`/`.rustfmt.toml`, `clippy.toml`/`.clippy.toml`, and generated-output
      provenance explicitly. WGSL is a distinct shader input, not Rust source or
      ignored data. Cargo output directories must come from governed configuration
      and metadata; do not globally ignore every directory named `target`.
- [ ] Reject path escapes and ambiguous ownership. Do not infer package names
      from directory basenames. Record incomplete dependency metadata as unavailable.
- [ ] Keep discovery offline/read-only after controlled dependency preparation;
      resolution must not silently refresh a lockfile or download dependencies.
      Use frozen resolution after dependency preparation. The data-only bootstrap
      path must still explain missing locks/dependencies without invoking a build.
- [ ] Propagate manifest, lockfile, feature, toolchain, shader and build-input
      changes to affected modules and evidence invalidation.

## Phase 2: sealed tools and execution boundaries

- [ ] Add policy-owned, integrity-checked Cargo/rustc/rustfmt/Clippy/helper
      installation and runtime resolution, with licenses, checksums and provenance.
      Match compiler-coupled tools. Distinguish the target's minimum Rust version from
      the exact approved execution toolchain. No implicit `stable` channel updates.
- [ ] Keep native SDK/linker requirements explicit per host. Missing Windows SDK,
      Xcode, system libraries or target components cannot be reported as a pass.
- [ ] Separate non-executing source/manifest analysis from compilation. Cargo
      checking can execute build scripts and procedural macros; `--offline`,
      `--locked`, and `--frozen` do not make that execution safe by themselves.
- [ ] Contain authorized compilation with no credentials, bounded resources,
      controlled network/filesystem access, dedicated caches and output directories,
      and process-tree cancellation. Inventory generated `OUT_DIR` inputs and native
      tool invocations. Do not run target code merely to discover its policy.
- [ ] Govern Cargo aliases, runners, wrappers, linkers, rustflags, environment,
      source replacement, and local/user configs. Separate necessary build settings
      from attempts to weaken analysis or execute unrelated commands. Account for
      ancestor configuration outside the checkout and Cargo home, explicit config
      files/includes, rustup overrides, and the actual invocation directory. A
      workspace-root invocation does not read each member's Cargo configuration.

## Phase 3: source semantics and architecture

- [ ] Use parser/compiler-backed facts for modules, imports, re-exports, renamed
      crates, `#[path]`, `include!`, macro-generated items, and `cfg`/feature branches.
      Cargo's crate graph alone is insufficient for intra-crate module boundaries.
- [ ] Analyse each supported target/feature configuration honestly. Do not use
      `--all-features` blindly when features are mutually exclusive, or treat a
      Linux build as coverage of Windows/macOS branches.
- [ ] Feed resolved facts into existing ownership, cycle and dependency rules.
      Missing macro expansion or unresolved dynamic/generated edges must be visible
      coverage limitations, not fabricated source edges or silent success.
- [ ] Enforce quality rules via normalized facts and existing policy defaults.
      Do not claim that Clippy alone supplies full dead-code or architecture proof.
      Explain public-library entry points and feature-specific reachability.
- [ ] Inventory unsafe blocks/functions, FFI, and native dependencies with exact
      ownership and rationale. Scoped graphics interop may require unsafe; neither
      a blanket prohibition nor a global exemption is appropriate.
- [ ] Govern Rust lint attributes and `#[expect]` without blanket allows. Keep
      diagnostic IDs stable; policy exceptions stay exact, justified and expiring.
- [ ] Validate WGSL with an exact compatible parser/validator, map shader assets
      to consumers, and invalidate receipts when shaders change. GPU execution and
      visual correctness remain project-specific evidence.

## Phase 4: Rust supply-chain governance

- [ ] Require exact registry declarations (`=x.y.z`), immutable full Git revisions,
      contained path dependencies, and a checked-in lock for the governed workspace,
      including library workspaces whose policy evidence depends on resolution. Plain `x.y.z`
      permits compatible updates in Cargo and is not an exact pin.
- [ ] Review workspace inheritance, optional/build/dev dependencies, patches,
      alternate registries, vendoring, yanked versions and native build inputs.
- [ ] Generate/review candidate resolution without running build scripts or
      macros, then install approved dependencies through the existing workflow.
- [ ] Extend age/provenance/security-fix admission and signed Git evidence to
      Cargo sources. Preserve distinctions among missing, stale and trusted facts.
- [ ] Integrate pinned Rust advisory tooling/data with the common finding and
      exception model. A lockfile advisory scan is not a reachability analysis;
      assess applicability through evidence before security-fix admission.
- [ ] Produce complete SBOM and release provenance, including bundled native
      libraries and helper executables. Reuse artifact-security ownership rather
      than adding an unrelated Rust scanner pipeline.

## Phase 5: tests, reports and release integration

- [ ] Map Rust suites to focused/recommended/full/supplemental profiles. Support
      unit, integration, doctest, benchmark and native app workflows distinctly.
- [ ] Reject a required suite that selects zero tests, skips every test, or only
      builds. Use structured, version-bound runner evidence; do not parse arbitrary
      terminal prose as authoritative results. Choose a supported structured runner
      deliberately if stable Cargo output cannot satisfy the evidence contract.
- [ ] Preserve temporary state, assertion strength, suite ownership, cancellation,
      performance leases, live approvals, and receipt identity semantics.
- [ ] Bind compiler, helper, source/config closure, features, target, native SDK,
      shaders and relevant environment into receipts. Do not reuse GPU performance
      results across adapters, drivers, displays or execution environments.
- [ ] Extend schema, diagnostics, JSON/SARIF, doctor, installation, docs catalog,
      adoption guidance, design context, CI bundles and release verification together.
      Generated surfaces must be regenerated from their owners.
- [ ] Supply valid and seeded-invalid fixtures: workspace ownership, forbidden
      edge, macro/cfg coverage failure, config suppression, unpinned dependency,
      lock drift, rejected provenance, shader error, zero-test selection, timeout,
      failed test and stale receipt. Include mixed Go/Python/Rust repositories and
      real Windows/macOS/Linux installations.

## Delivery and acceptance

Release only after the upstream repository's authorized release workflow proves
the entire capability matrix through the public CLI and installed artifact.
Publish an exact version/digest through separately authorized publication.
Masterbrainer then adopts that release through its dependency workflow.

No target should need a Rust language-pack installation, hand-maintained import
inventory, copied tool bootstrap, or duplicate lint/security gate to satisfy the
baseline. Required capabilities with known gaps cannot be described as complete.
Masterbrainer retains ownership of GPU baselines and real application workflows.

## Implementation status

The input coverage foundation is implemented: standard Rust settings trigger
full policy selection, and WGSL has distinct source classification and module
ownership. Temporary-repository fixtures cover changed/deleted settings, shader
dependency impact, data rejection, and missing structured analysis. Current
behavior is documented in [Rust and shader input coverage](../design/rust-inputs.md).

The next milestone is Cargo facts and sealed discovery. Workspace resolution,
Rust tool installation, semantic analysis, supply-chain checks, structured Rust
test results and release parity remain open. The existing Rust coverage failures
remain until their implementations and evidence exist. This beta foundation is
not yet usable as first-class Rust support for Masterbrainer.

Primary references: [Cargo metadata](https://doc.rust-lang.org/cargo/commands/cargo-metadata.html),
[Cargo dependency semantics](https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html),
[build-script execution](https://doc.rust-lang.org/cargo/reference/build-scripts.html),
[Clippy configuration](https://doc.rust-lang.org/clippy/configuration.html).

Additional references: [Cargo configuration](https://doc.rust-lang.org/cargo/reference/config.html),
[rustup overrides](https://rust-lang.github.io/rustup/overrides.html),
[rustfmt configuration](https://github.com/rust-lang/rustfmt#configuring-rustfmt).
