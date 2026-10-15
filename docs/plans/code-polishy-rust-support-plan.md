# First-class Rust support in Code Polishy

Date: 2026-09-24. Status: beta implementation in progress.
The original implementation branch was `beta-rust-support`, based on `main` at
`d6d14503ec561e54e61c36beb218f7ce684f75fe`. Integration continues on
`feature/rust-optional-install` with mainline safeguards and the `0.28.3` lock.
Imported from Masterbrainer's `docs/plans/code-polishy-rust-support-plan.md`
at planning commit `be593fb`. This copy owns the Code Polishy implementation.

## Outcome

Rust receives the same product-level integration as Go and Python: automatic
discovery, policy-owned tooling, source facts, architecture, quality, dependency
governance, tests, reports, receipts, documentation, and verified releases.
Rust integration remains in the core engine. The large Rust SDK and native test
runner are an explicit, separately installed official bundle authenticated by
the exact core release. Repositories without Rust avoid that payload. Missing
or altered tools block required Rust operations; installation optionality cannot
disable source recognition or required policy coverage. The
[optional Rust tools contract](../design/optional-rust-tools.md) owns this
boundary. Full language-pack extraction is not required for this delivery.

The Go policy engine remains the decision owner. A pinned Rust helper may supply
compiler/parser facts through a bounded child-process protocol. Do not embed
language-specific heuristics in unrelated engine modules or reimplement Rust
resolution with regular expressions.

Rust follows Go's built-in native routing. A separate conditional-module switch
is not necessary to activate or disable these baseline capabilities. WGSL remains
a distinct follow-on language capability for graphics projects, not a prerequisite
for ordinary Rust parity. Its existing source classification and unavailable
analysis findings remain honest until that validator is delivered.

Parity means the same ordinary user-facing commands and policy outcomes as Go,
with Rust-native compilation units, Cargo conventions and honest scope. Features,
targets, build scripts, procedural macros, tests and generated source need their
own semantics. Validate the selected supported configuration, not every possible
feature and target combination. Reuse existing execution controls and expose
their limits; a new Rust-only operating-system sandbox is not a prerequisite for
the capabilities already delivered through equivalent Go execution.

## Current baseline and architectural decisions

The original implementation base included the accepted changes through `0.27.11`.
The integration branch now includes mainline through `0.28.3`, preserving its
dependency-input and Windows filesystem safeguards.

Portable releases separate the SDK and Nextest into an authenticated companion.
The core inventory remains authoritative and the native policy remains required.
Local contract tests cover absent tools, exact installation, foreign payload
rejection, altered and additional files, symlink rejection, cancellation,
partial-publication rollback, and repair. The Linux x64 tool payload is about
686 MB before compression. Release jobs exercise installation and removal of the
companion through the public CLI on every supported host; those jobs have not yet
supplied evidence for this candidate.

The Linux x64 installed fixture passed for committed source `a3b6f82`: required
Rust checks reject the absent payload, the exact companion installs through the
launcher, formatting/compiler diagnostics/native tests/doctests execute, and
removing Nextest blocks testing until repair. That private artifact has release
digest `7070c7937b5a01e3a3858a3b36499e6b08e55fe367a3148674ebd8c0d1d47360`.
Additional bundle tests cover release-owned permissions, preventing archive
metadata from adding shared write access or removing executable permissions.
This is local execution evidence, not a published release or all-host acceptance.

Workspace containment at `d22adaf` passed the focused native, receipt, retry,
regression-proof and release suites. The changed-scope run completed 38 suites:
36 passed, the installed fixture exposed an outdated single-command assertion,
and the JavaScript bundle fixture could not complete an offline installation.
The installed assertion now requires successful workspace discovery before native
execution. The focused installed-release rerun passed at `d66647e`, covering
first adoption, pack verification, absent Rust tools, exact installation,
formatting, diagnostics, native tests, doctests, failure outcomes and repair.
Its private Linux x64 artifact has release digest
`424f856fa177a57d978660f0c28181262d22caa5fc8d137dc5a747a680d6d953`.
Across the original run and focused correction, 37 of the 38 selected suites
passed. The verification command still fails its 12 expired vulnerability
assessments. A diagnostic rerun identified the JavaScript fixture's missing cached
`@types/node@24.13.3` archive; this run does not establish a source defect.
Checkpoint validation still reports unsupported semantic-helper coverage,
Cargo dependency coverage, architecture imports and expired JavaScript
assessments. A clean verification checkout also lacked two module Python
environments. No policy failure was waived.

The ordinary installed-release contract now exercises the optional Rust
installation lifecycle, and Windows CI selects optional-bundle and native Rust
contract tests. Windows execution remains unverified locally. Authenticating
the 171-entry, 686,461,770-byte Rust payload took 3.33 seconds in a local
measurement, excluding core verification and the inventory walk. Integrity
checks remain active; verification is not cached across untrusted build-script
or test execution.

Full Rust delivery remains incomplete. The locked `0.28.3` engine cannot govern
the candidate semantic helper's Rust source and Cargo dependencies. Its admission,
compilation and semantic integration remain required; disabling those checks is
not an acceptable bootstrap. Complete native dependency notices/inventory and
all-host execution also remain open. The 2026-10-15 JavaScript dependency update
removes the expired brace-expansion assessments by adopting the now-aged fixes.
None of the remaining failures is waived by the
optional installation boundary.

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
4. **Workspace and semantic coverage:** expand the supported Rust configuration
   matrix to the Phase 1–5 cases below, including native boundaries. WGSL has a
   separate follow-on acceptance track.
5. **Installed-release parity:** prove every acceptance cell on the supported
   hosts before advertising first-class support and adopting it in Masterbrainer.

Milestones 2–4 cut across the phases below. Read-only manifest inventory can
precede tool execution, but resolved metadata cannot be admitted before the
tool/configuration boundary is ready. Reuse existing exact test suites for
changed modules; add a quick suite when a new module is introduced.

### Implemented beta slices

- Pinned toolchain installation and Cargo workspace discovery are connected to
  the normal check path and command plan. Discovery does not execute build scripts
  or materialize dependencies.
- Selected-file rustfmt uses Cargo editions and bounded project configuration;
  generated files, unselected child modules and concurrent edits remain protected.
- Frozen native-host compiler/Clippy checks preserve original-path diagnostics,
  default feature selection and gated-target behavior. Real fixtures cover type
  errors, lint defects, attempted suppression, absent/drifting locks, unavailable
  linkers, cancellation and build-script configuration output. Public CLI checks
  retain the remaining coverage failures instead of declaring full Rust support.
- Compiler warnings and the selected Clippy/unused groups use forced warning
  collection so typed policy findings remain independent of process failure.
  Real compiler tests prove source lint levels cannot hide those findings and
  type errors, default-denied compiler lints and build-script failures still fail.
- Parser-backed source rules now enforce comment, lint-attribute, format-skip and
  Rust-specific complexity policy, with original locations and independent
  function/closure measurements. Inline tests do not relax production limits.
  The pinned Tree-sitter Rust grammar supplies written-source facts; it does not
  pretend to resolve macro expansion or compiler configuration.
- General macro/module resolution, WGSL and
  installed-release integration remain unfinished. Project Clippy overrides
  and non-native targets currently fail
  explicitly rather than weakening the supported compiler configuration.
- Native dead-code coverage uses the pinned compiler's forced unused diagnostics
  for the checked configuration, preserving public library entry points. It does
  not claim whole-program public-API reachability or unused-code proof for
  configurations that were not compiled.
- Written-module architecture now connects Cargo targets, conventional and literal
  module paths, use trees, aliases, re-exports and qualified references to existing
  dependency-direction and cycle policy. Real Cargo fixtures cover forbidden
  edges, source cycles, renamed workspace dependencies, unrelated invalid crates
  and command-plan agreement. Native graph evidence survives the report schema.
- Standard macros retain dependencies from parsed written arguments, including
  nested assertions, formatting, vectors and match guards. Locations remain in
  original source. Lexical imports/generics and edition-specific standard/core
  preludes preserve scope and shadowing; unknown expansion remains explicit.
  Macro expansion and other unresolved syntax still fail coverage explicitly.
  A plain crate passes the complete public CLI `check`; this is a usable subset,
  not evidence that macro-heavy or arbitrary target configurations are supported.
- Typed Rust suites now use the pinned Nextest runner through ordinary suite
  selection, direct execution, gate plans and regression proofs. Structured
  JUnit evidence requires nonempty executed tests, complete counts and unique
  identities; assertions remain distinct from compiler/build/operational failures.
  Cargo workspace defaults, package and feature selection, integration targets,
  filters, cancellation, frozen locks and receipt invalidation have real runtime
  fixtures. Application logging variables remain valid explicit inputs; ambient
  Cargo/compiler/Nextest controls do not replace governed execution. Nextest state
  stays outside reusable read-only source views. Project Nextest settings require
  explicit support rather than silently dropping their default filters or scripts.
  Ordinary macro-heavy project coverage remains unfinished.
- Separate native doctest suites run the pinned stable Cargo/rustdoc without
  unstable flags. Version-bound output becomes a required structured artifact
  with complete case/count checks and distinct runtime, expected-panic,
  compile-only, compile-fail and ignored outcomes. Real fixtures cover workspaces,
  package/features, frozen locks, cancellation, empty/ignored runs, crate and
  build-script failures, direct/gate execution, regression proofs and receipts.
  A doctest result never substitutes for unit/integration evidence.
- Real dependency preparation exposed published packages whose unused test,
  example, bench or binary metadata points to files excluded from their archives.
  Resolution now retains those remote target facts without requiring absent
  inactive files. Local targets and dependency library/proc-macro/build sources
  remain mandatory; escaping and linked paths still fail. The complete Rust-facts
  suite passes this change, including the new red/green missing-target regression.

Cargo dependency-input governance now parses ordinary, build, development,
target-specific and workspace declaration tables, checks exact registry and Git
pins, contains local paths, and validates versioned lock identities/checksums.
It keeps Git and registry coordinates separate and retains explicit failures for
unsupported source substitutions. These declaration and lock readers are
data-only. Required signed Cargo Git assessments use the existing optional
Git-evidence protocol, with complete remote inventories and exact lock scope.

Native Cargo lock verification now uses bounded, complete resolved metadata through
the ordinary supply-chain runner. Online verification may fetch already locked
sources with `--locked`; offline verification requires prepared sources with
`--frozen`. Neither mode updates the lock or compiles project code. Facts retain
renamed edges, dependency kinds, target predicates and default-feature selection;
inactive optional packages remain in the full lock inventory for supply-chain
policy even when absent from the active graph.

Public crates.io minimum-age checks cover complete lock inventories, batch exact
versions by crate, and follow the registry's identified one-request-per-second
API limit. Missing metadata cannot pass or be filled by an assessment. Existing
exact release-age assessments now support `crates.io` and Cargo.lock scopes;
Git/private sources are not sent to the public registry. Exact new direct runtime
and optional declarations receive preferred-age advice; workspace definitions do
not receive direct-dependency advice without a proven consumer.

Cargo dependency review compares declarations and complete external lock
inventories without invoking Cargo. Rows preserve direct, workspace-definition,
inherited, override and resolved roles; renamed packages and source changes retain
their identity across lock scopes. Bare Cargo version requirements remain ranges.
Historical branch, tag, alternate-registry and override declarations can be
reviewed for removal without admitting them into current dependency policy.
Credential-bearing, malformed or escaping inputs fail before comparison.

Native Cargo advisory coverage now uses the pinned OSV-Scanner through a
public-crates-only projection of the complete lock. Raw Cargo extraction and
compiler call analysis are disabled. The report must account for every projected
version, and exact `crates.io` vulnerability assessments preserve the original
lock scope. Tests cover source isolation, incomplete reports, scanner exits,
assessment identity and the real pinned scanner with a temporary offline advisory
database. Signed Cargo Git inventories retain separate Git and crates.io
identities, including same-name/version packages and multiple crates from one
repository. They share signature/trust/expiry checks and gate receipt identity
with Python without sending private coordinates to public registries.

Configured Cargo license policy now authenticates cached `.crate` archives against
the complete lock inventory and applies the common SPDX evaluator to their
published manifests. It covers inactive optional/platform releases, preserves lock
scopes, and never substitutes Git or local metadata for public crate evidence.
Missing, corrupt, linked, ambiguous and oversized inputs fail coverage. Pre-install
dependency review defers installed-license checks, matching the Node workflow;
normal verification uses prepared archives without downloading or executing code.
Pinned Cargo packaging fixtures cover inherited workspace license metadata.

### Semantic helper admission checkpoint

`internal/rustfacts/semantic/` contains an initial, uncompiled Rust Analyzer
helper and fixtures for generated-only dependencies, nested expansion, `$crate`
hygiene and original call-site locations. It is not connected to public commands,
shipped in releases or evidence of completed macro support. No helper build or
fixture execution has been authorized by successful dependency admission yet.

The candidate pins Rust Analyzer `0.0.346` and locks Salsa, `salsa-macros` and
`salsa-macro-rules` to `0.28.5`. Earlier candidate Salsa releases fail
[RUSTSEC-2026-0308](https://rustsec.org/advisories/RUSTSEC-2026-0308.html), reported
2026-09-24. The fixed release is younger than the 30-day baseline. Registry lock
generation executed no build scripts, and the rejected vulnerable library was
not compiled. Review and data-only preparation do not grant executable admission.

The outgoing `0.28.3` schema accepts release-age assessments only for
`artifact`, `go`, `npm`, `pnpm` and `pypi`, not `crates.io`. Its source checks also
lack Rust analysis. A private source install cannot provide the authenticated
HTTPS publication index and archive-backed identity required by the supported
lock upgrade. A published native-Cargo engine and a separately authorized
self-hosting lock upgrade must precede the helper build. Target providers cannot
replace the locked assessment contract. Keep the current lock unchanged meanwhile.

The 2026-10-15 registry inspection covered all 188 locked registry packages:
checksums matched and none was yanked. Only the three Salsa packages remained
under 30 days; all are ordinarily eligible by 2026-10-24 at 09:56:31 UTC.
Waiting removes the age exception requirement. The helper is not deployed, so
the current evidence does not establish that an early security-fix admission
is safer than waiting. This metadata inspection is not executable admission,
successful compilation or a replacement for Cargo security checks.

After admission, compile and test the helper before integrating it. Complete
standard-library and attribute/derive expansion, source/configuration ownership,
the common command runner and release packaging; then replace the superseded
written graph at one coherent cutover. The full Rust objective and remaining
acceptance matrix are unchanged.

## Phase 0: define the complete coverage contract

- [x] Audit current Go/Python owners and Rust gaps against the recorded base;
      establish the parity inventory above.
- [ ] Turn each acceptance cell into executable evidence, preserving explicit
      unavailable/incomplete states and original-path diagnostics.
- [x] Choose the initial toolchain boundary: Rust 1.97.1 with its matching Cargo,
      rustfmt and Clippy; editions 2015, 2018, 2021 and 2024; minimum Rust versions
      in the compiled configuration may not exceed the governed compiler.
      Unselected workspace members retain their declared requirements without
      blocking compatible selected builds. Compilation initially uses
      the native host and default features. Additional feature/target selections
      need explicit analysis evidence. Installation hosts remain Linux/macOS on
      x64/arm64 and Windows x64; installation does not prove cross-compilation.
- [x] Route governed Rust through first-class native capabilities, like Go,
      without a separate conditional-module switch. Unsupported or missing
      analysis remains a blocking finding, not a successful generic command.

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

- [x] Discover workspace members, declared dependencies, inherited package
      metadata, features and targets using bounded, versioned, frozen Cargo
      metadata. Expose discovery failures through the normal check path and
      include its commands in execution planning. Preserve null resolution as
      absent dependency-graph evidence. Test real Cargo without executing build
      scripts or materializing dependencies.
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
      normal checks use frozen resolution and never refresh a lockfile or fetch
      dependencies. Online supply-chain review may fetch already locked source
      data without compiling it; offline verification requires prepared sources.
      The data-only bootstrap
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
- [ ] Run authorized compilation through the common governed runner, with a
      controlled environment, bounded resources, dedicated caches/output and
      process-tree cancellation. Govern dependency network access explicitly.
      Inventory generated `OUT_DIR` inputs and native tool invocations. Document
      build-script and procedural-macro execution and the actual containment
      provided; do not describe environment controls as an OS sandbox. Do not run
      target code merely to discover its policy.
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
- [ ] Follow-on WGSL track: validate shaders with an exact compatible parser/validator, map shader assets
      to consumers, and invalidate receipts when shaders change. GPU execution and
      visual correctness remain project-specific evidence. This is separate from
      the core Rust delivery gate.

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
- [x] Reject a required native Rust suite that selects zero tests, skips every test, or only
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

Targets use the official tool bundle without hand-maintained import
inventory, copied tool bootstrap, or duplicate lint/security gate to satisfy the
baseline. Required capabilities with known gaps cannot be described as complete.
Masterbrainer retains ownership of GPU baselines and real application workflows.

## Implementation status

The input coverage foundation is implemented: standard Rust settings trigger
full policy selection, and WGSL has distinct source classification and module
ownership. Temporary-repository fixtures cover changed/deleted settings, shader
dependency impact, data rejection, and missing structured analysis. Current
behavior is documented in [Rust and shader input coverage](../design/rust-inputs.md).

Cargo discovery now runs through the existing check path and command planning.
Bounded TOML inspection reports malformed manifests at their original locations;
Cargo supplies workspace membership, inherited metadata, declared dependencies,
features and targets. Discovery uses a sealed environment and does not execute
build scripts or resolve dependencies. Tests preserve valid sibling projects and
reject unsupported configuration, escaping paths, malformed metadata and missing
tools. [Cargo project facts](../design/cargo-facts.md) owns that boundary.

Developer setup installs authenticated Rust 1.97.1 components without rustup or
installation scripts. Linux runtime execution and installer integrity/recovery
fixtures pass. Native release packaging and a dependency-free installed Cargo
workflow are verified on Linux x64. The remaining host executions still require
verification; macOS/Windows installers and pinned archives alone are not proof.

Selected-file formatting, compiler diagnostics, parsed source policy and Cargo
dependency governance now have runnable beta implementations. Source parsing uses
exact authenticated binary wheels in the existing governed Python runtime; it
requires neither target package installation scripts nor a new Rust bootstrap.

Syntax requests are now batched by encoded bytes and source count, with each batch
visible in command planning. Valid siblings survive a failed or oversized input;
cancellation leaves unstarted sources explicitly uncovered. Large-input fixtures
also exposed unsafe coordinate-property access in the pinned Python binding.
Using its tuple interface preserves long-line and Unicode coordinates without a
dependency change. Native tests now validate Cargo's actual workspace and its
contained regular lock before compilation, retries and receipt reuse. Reusable
execution validates both source and copied workspaces while retaining governed
writable build directories. Direct and gate plans include those metadata commands;
regression proofs retain the same preparation boundary. Fixtures reject escaping
ancestor workspaces and linked locks before a build script can run, and distinguish
an explicit workspace lock from unrelated root or member locks.

Native test-source checks now reject empty built-in Rust tests, including ignored
and conditional declarations, without classifying helpers or closures as test
entries. Conditional test attributes retain production complexity limits outside
test-only scopes. The version-2 syntax contract, graph validation and report schema
carry the corresponding context and entry evidence. Nonempty bodies alone still
do not establish assertion strength or custom test-framework coverage.

Native unit/integration and separate doctest suites now retain nonempty structured results through
ordinary commands, gate artifacts, regression proofs and reusable receipts. The
[Rust test contract](../design/rust-tests.md) documents selectors and boundaries.
Native release builders now carry the Rust SDK, Nextest and parser dependencies.
Manifest version 7 binds the executable pins; the SBOM includes both parser
distributions and the top-level Rust tools. The installed Rust fixture passes on
Linux x64 against committed source `3fafe17`: selected formatting, JSON/SARIF
compiler diagnostics, nonempty native tests, assertion failure, zero-test
rejection, doctest example semantics and failures, and recovery through the
verified launcher. The private installed artifact has release digest
`5f4452b4fb01bdc396e948c761210c9e965f5246a377856213a89915c2fdc476`.
All-host installed evidence
and complete bundled dependency notices/inventory remain publication requirements.
No beta release has been published or adopted.

The Nextest source audit at `fc97e97bbe0a3927482a694247da00c099f4269e`
found 580 locked packages: 571 crates.io archives and 9 workspace packages,
including upstream test fixtures. All 571 archive checksums matched on
2026-09-25. A recursive notice-filename inventory found 979 files, but 45 crates
had no packaged license/notice/copyright files under those names. This is a
source-inventory observation, not proof of the linked binary graph or license
compliance. Completing the notice set requires exact upstream source evidence;
copying the lock or substituting generic license text does not resolve it.
Rust and Nextest SBOM entries retain explicit incomplete native compositions,
host-specific authenticated download references and carried notice locations.

The next milestones extend ordinary macro/module coverage and
complete installed-release integration. Existing coverage failures remain until
their implementations and evidence exist. This beta does not yet establish
first-class support for Masterbrainer's full project.

Primary references: [Cargo metadata](https://doc.rust-lang.org/cargo/commands/cargo-metadata.html),
[Cargo dependency semantics](https://doc.rust-lang.org/cargo/reference/specifying-dependencies.html),
[build-script execution](https://doc.rust-lang.org/cargo/reference/build-scripts.html),
[Clippy configuration](https://doc.rust-lang.org/clippy/configuration.html).

Additional references: [Cargo configuration](https://doc.rust-lang.org/cargo/reference/config.html),
[rustup overrides](https://rust-lang.github.io/rustup/overrides.html),
[rustfmt configuration](https://github.com/rust-lang/rustfmt#configuring-rustfmt).
