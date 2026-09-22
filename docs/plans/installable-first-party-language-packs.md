# Installable First-Party Language Packs

Status: proposed implementation plan; refreshed 2026-09-20 against `main` at
`c7c83ff00fa57ab91bb63895dd79508641972d41` with locked release `v0.27.8`;
immutable behavior origin `v0.25.0`

## Outcome and implementation boundary

Move all existing Go, Python, JavaScript and TypeScript, and Bash/POSIX shell
functionality into independently installed official language packs. Preserve
observable behavior, including policy activation, project discovery, failure
handling, generated-source protection, dependency evidence, and installed-release
operation. Moving only formatter and linter commands does not complete a language.

The planning work originated on `beta-language-packs`, branched from `beta` at
`854604a33e32421b3d3b25a60d888042a0751c43`, and was incorporated into `main`
after the v0.25.0 release without changing runtime ownership, pack selection, or
release defaults. That branch is historical planning provenance, not the active
implementation branch. This refresh audited `main` through the revision above.
Begin implementation from the then-current `main` after the inventory and test
framework below are ready, and account for any later delta before editing source.

Use small, reviewable milestones on the implementation branch. Preserve the
original reference when incorporating later `main` fixes; give each
behavior-changing fix its own fixture and record any deliberate reference
update. Do not merge partial native-ownership removal into `main` or advertise
parity while required cases are uncovered. Intermediate implementation commits
may stage the replacement contract and packs while native routes still exist,
but no release may ship that mixed ownership state. The first release carrying
the replacement protocol also removes native ownership and rejects repositories
that have not made the explicit pack-policy cutover. There is no compatibility
release, dual execution, alias, translation layer, or automatic pin migration.

The first deliverable is an executable behavior inventory and a differential
runner. No native implementation is removed before that runner proves the
replacement against its reference. This is evidence of the enumerated contract,
not a claim that finite tests prove every possible program equivalent.

## Refreshed implementation baseline

Keep three identities separate throughout the work:

1. The tagged `v0.25.0` release is the immutable origin for the existing pack
   contract and the native/provider regressions that motivated this plan.
2. The exact current-`main` task base is the primary behavior reference for an
   implementation task. It includes every accepted fix after `v0.25.0`.
3. The candidate core and exact pack artifacts are the proposed replacement.

The 2026-09-20 audit found `internal/pack`, `providers/javascript`, the pack
schema, the example pack, and the authoring guide unchanged from `v0.25.0`.
Manifest version 2 and protocol version 3 therefore retain their existing
authoring and JavaScript-shaped discovery limitations. This does not mean the
rest of the runtime is unchanged. The current-main ledger must include these
post-reference behaviors before extraction begins:

- selected format writes remain confined to their selected added or modified
  files and never fall back to unrelated command paths;
- managed repository wrappers, archive-backed setup, publication-backed v2
  locks, and their control-input classification remain core repository services;
- two-phase `upgrade plan` / `upgrade apply`, authenticated capability and
  diagnostic previews, stale-plan rejection, rollback, and lock-last authority
  cutover remain intact;
- reusable-suite evidence retains its exact receipt path and digest, while
  current checkpoint, merge-gate, and supplemental scheduling stays core-owned;
- dependency review before installation remains distinct from frozen,
  script-disabled installation, offline supply-chain verification, and installed
  license evidence;
- date-only exception, assessment, and dependency-governance expiry uses explicit
  UTC-day semantics;
- local policy-owned files are validated directly rather than protected by
  mirrored checksums; digests remain for trust boundaries, immutable artifacts,
  and reusable evidence.

At implementation task start, record the exact `main` revision and locked
release, inspect the delta from the audited revision above, and add any changed
observable behavior to the ledger before changing governed source. Later product
fixes update the current-main lane; they do not rewrite the immutable origin.

## Starting point and related work

The immutable v0.25.0 origin has native implementations for all four language
groups and an optional JavaScript/TypeScript provider. The refreshed current-main
baseline still uses manifest version 2 and analysis protocol version 3 for packs.
The optional provider supports useful Astro mapping, JavaScript type checking,
jsconfig, TypeScript aliases, asset resolution, and manifest/framework entries.
Preserve these alongside every later accepted native behavior; the provider is
not yet a complete replacement for the native language boundary.

The [analysis ownership design](../design/provider-scope.md) is the current
scope contract. In particular, read context, required coverage, reportable
diagnostics, and writable files are distinct. Generated source keeps its actual
path and uses its validated effective package context.

The [discovery and capabilities plan](universal-language-pack-capabilities.md)
provides the future protocol direction. Implement only the discovery, execution,
and evidence contracts needed to preserve these four languages. Cargo, Gradle,
Bundler, and CMake prototypes are separate future work and do not block this
migration. Any necessary future protocol replacement is one clean cutover;
there is no dual-protocol translation layer.

## Product boundary

| Core engine owns                                                     | Language pack owns                                                     |
| -------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| Governed inventory, path containment, data/generated classifications | Language recognition, syntax, ecosystem metadata interpretation        |
| Selection, write authority, diagnostic authority, module direction   | Project/package/target discovery and dependency closures               |
| Generic enforcement of release-locked policy and exceptions          | Language rules, policy declarations, activation facts, tool settings   |
| Installation integrity, execution limits, tool identities            | Language runtimes, parsers, formatters, analyzers, declared toolchains |
| Evidence validation, coverage requirements, failure reporting        | Type, complexity, dead-code, comment, import, and literal facts        |
| Generic dependency policy and approved service execution             | Manifest/lock readers, resolved graphs, ecosystem audit/build adapters |
| Test scheduling, receipts, reports, checkpoints, merge gates         | Language test recognition and impact facts consumed by core            |
| Markdown, workflow, artifact, and repository-service policy          | Pack fixtures, inventories, release notes, and platform support        |

Keep generic policy enforcement in core even when a language supplies its facts.
For example, a pack identifies comments and import edges; core enforces comment
policy and module direction. Language-specific rule definitions and defaults
must remain bound to the locked release's authenticated baseline. Selecting a
pack or reading target configuration cannot lower that baseline.

Inventory shared scanners and parsers before deciding where to move them. Core
may retain a parser or runtime needed by a repository service, such as YAML
workflow validation, but that service must not depend on installing a source
language pack. Do not leave JavaScript analysis in core merely because a shared
service uses JavaScript internally. Similarly, an engine implemented in Go can
still be built with Go without shipping Go source analysis to its users.

After a language cutover, core must not discover that language's projects,
interpret its manifests, invoke its analysis tools, or contain its source rules.
Validated pack declarations and facts replace those decisions. Generic path,
module, classification, execution, and evidence handling remain core-owned.

## Feature inventory: the migration's coverage ledger

Before extraction, add a machine-readable ledger and fixture format with strict
validation. Proposed locations are `tests/language-conformance/` for the ledger,
fixtures, and runner, and `providers/<language>/` for pack-owned implementation.
Finalize module ownership and quick boundary suites before adding governed code.
These paths describe planned artifacts; this document does not create them.

Each ledger row records:

- stable behavior ID, language, capability, and user-visible guarantee;
- current implementation entry points, contributing helpers, CLI routes, policy
  keys, activation conditions, and documentation;
- current test names and the gaps requiring new fixtures;
- future owner: core, pack, or shared protocol, with the boundary rationale;
- fixture IDs, selected command/profile/mode, required platform/toolchain cells,
  and expected findings, coverage, writes, and failures;
- current-main reference identity, any applicable immutable-origin identity,
  candidate pack identity, and exact evidence for each run;
- the post-audit change that introduced or last intentionally changed the
  behavior, when it differs from the immutable origin;
- status: untested, passing, failing, blocked, or deliberately changed.

Inventory behavior by tracing source, registered checks, configuration/schema,
capability declarations, CLI commands, release scripts, and existing tests in
both directions. Every language-related public entry point and policy option
must map to ledger rows; every row must map to executable assertions. Internal
functions map to the behavior they implement, with focused unit tests for
important branches and boundary cases. Counting functions or lines is not proof
of preserved behavior.

Do not treat today's tests as a complete specification. Read untested branches,
error paths, defaults, and disabled-policy behavior. Seed a defect and include a
valid counterexample wherever a rule can produce false positives. Exercise
thresholds immediately below, at, and above their limits. Existing useful unit
and integration tests move with the implementation; shared CLI fixtures protect
the public contract across that move.

Fail ledger validation for missing or duplicate IDs, nonexistent fixtures,
unmapped required cases, skipped required platform cells, or an empty suite.
Every required row needs passing evidence before its language can cut over.
Unsupported combinations require an explicit unsupported-result assertion and
must not be recorded as successful analysis.

### Initial inventory checklist

This table is a starting checklist, not a completed or exhaustive inventory.
Split its entries into individually testable ledger rows during Phase 0.

| Area                                   | Existing behavior to enumerate and preserve                                                                                                                                                                                                                                                                                                                                    |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Go discovery and tools                 | Root and nested modules, packages and workspaces, test packages, build constraints/tags, local replacements, exact toolchain/environment, gofmt, vet, staticcheck, compilation/type errors, dead-code and complexity behavior, configured builds                                                                                                                               |
| Go architecture and dependencies       | Package/import graphs, focused dependency closure, module direction and cycles, source comments/directives, module/sum ownership, exact versions, lock evidence, govulncheck and OSV coverage, release age and dependency policy                                                                                                                                               |
| Python projects and style              | Static pyproject discovery, nested and source-layout projects, interpreter pins, project-local `.venv` dependency evidence, Ruff configuration/activation, formatting, lint, complexity, ty configuration and full project diagnostics                                                                                                                                         |
| Python reachability and architecture   | Vulture, module/object imports, computed imports, runtime-loader contracts, entry points/plugins, external distribution ownership and provenance, callable/attribute flows, framework and repository contracts, persisted reachability evidence and invalidation, docstrings/directives, module direction and cycles                                                           |
| Python dependencies and execution      | PEP 508 requirements, extras/groups/markers, build-system requirements, uv lock reconciliation, Git dependency state/signature evidence, frozen-check requirements, OSV and release-age evidence, carried CPython/uv/tool installation, missing or malformed environment behavior                                                                                              |
| JavaScript and TypeScript projects     | Root/nested packages and workspaces, no root manifest, generated `context`, tsconfig/jsconfig/unique alternate config, compiler members, JS checking, aliases/assets, module/CommonJS modes including `.cts`, JSX/TSX, Astro mapping, installed dependency types                                                                                                               |
| JavaScript and TypeScript analysis     | Format/lint/complexity/comments, effective React and framework activation including disabled overrides, downstream type errors, package-wide Knip findings, metadata-only selection, conventional and manifest/framework entry points, policy glob grammar, import graphs and project roots                                                                                    |
| JavaScript and TypeScript dependencies | Node/package-manager pins, package/workspace/lock ownership, pnpm lock synchronization, dependency-source policy, licenses, release age, native audit and OSV, lifecycle-script restrictions, sealed runtime/bundle inventories and platform-specific packages                                                                                                                 |
| Bash and POSIX shell                   | Supported extensions/shebangs and dialects, syntax and ShellCheck behavior, existing formatting support or explicit absence, comment/directive parsing, heredocs/quoting/substitutions, portability facts, configured commands, exact shell/tool requirements and unavailable-platform behavior                                                                                |
| Shared integration                     | Generated/data/test classification, control inputs, parse-only data and literal modules, selected format-write authority, comment and metric policy, test discovery/ownership/impact, portability evidence, build/check coverage, policy modules, exception identity, doctor/adoption, check/format/gate modes, reusable receipt paths/digests, and installed-release behavior |
| Dependency workflow                    | Pre-install dependency review, frozen script-disabled installation, offline supply-chain verification, installed license evidence, release-age and vulnerability decisions, and the boundary between pack-owned ecosystem facts and core scheduling                                                                                                                            |
| Engine lifecycle and publication       | Managed wrappers, archive-backed setup, publication-backed v2 locks, authenticated capability catalogs, two-phase upgrade planning/application, lock-last guidance cutover, source recovery, native archive/index/OCI publication, and installed-release verification                                                                                                          |

Start the source audit in `internal/quality`, `internal/architecture`,
`internal/repository`, `internal/pythonfacts`, `internal/javascript`,
`internal/supplychain`, `internal/policymodule`, `internal/portability`,
`internal/testing`, and `internal/policy`; follow their engine/CLI callers and
`tools/`, `scripts/`, schemas, fixtures, documentation, and release assets.
The existing `providers/javascript` tests are an additional reference lane.
Explicitly assign adjacent CSS, HTML, PowerShell, and other recognized formats
that share current helpers; extracting these four packs must not silently remove
their existing behavior or imply new unsupported language coverage.

## Equivalence test architecture

### Freeze reproducible references

Preserve the tagged v0.25.0 native implementation at
`a800e66ef3ac5fc57c9d103dc84688aaa68ef4e8` as the immutable origin build, with
executable/artifact digests, exact tool and dependency pins, platform, command
environment, and policy identity. Also freeze the exact current-main task base
and its installed release as the primary reference build. The governing installed
release lock is workflow authority; it is not automatically the correct behavior
reference for another implementation tree.

Use four expectation lanes:

1. Current-main native baseline: each current language implementation, including
   accepted changes after `v0.25.0`.
2. Immutable origin: the tagged `v0.25.0` behavior used to attribute the existing
   protocol and provider regressions, never to override a later accepted fix.
3. Existing optional provider: its supported JS/TS and Astro improvements and
   explicit incomplete-coverage behavior.
4. Required corrections: separately documented fixes where no reference is
   the intended behavior, backed by direct expected outcomes and regressions.

The original main revision `a57aaa6fc70959699c8f8e6c0f0975dd05e0922d`
is historical evidence for disputed native parity, not a blanket replacement for
the tagged v0.25.0 origin. Never refresh expected results simply to match the
candidate. Each intentional difference names the behavior, reason, affected
fixtures, old/new outcomes, and a maintainer decision before public cutover.
Keep dependency/tool upgrades separate from extraction where possible so their
diagnostic changes are attributable.

### Run the same repositories through both implementations

The harness materializes each fixture into separate temporary repositories from
identical bytes, permissions, Git state, policy, dependencies, and initial caches.
Only the documented engine/pack selection and protocol configuration differ.
Install reference and candidate into separate prefixes and invoke their real
launchers; never switch ownership inside one running core process to compare.

Ordinary dependency and release-age fixtures use controlled registry/advisory
responses, a fixed clock, and exact local dependency artifacts. Record the
response identities so a changing public service cannot masquerade as a language
regression. Test denial of unauthorized network and target-script execution.
Live upstream checks remain separately authorized release evidence.

For each scenario, run the applicable reference CLI and candidate core plus exact
selected pack, then collect structured reports, process status, input/read evidence,
coverage, writes, artifacts, and filesystem changes. Exercise format check and
format write separately. Re-run formatted output to assert idempotence and
compare byte-for-byte protection of every file outside write authority.

The harness compares normalized semantic records and retains the raw outputs.
Normalization may remove temporary root prefixes, durations, invocation IDs,
ordering where order is not contractual, and the expected engine/pack identity
difference. It must not discard rule/check IDs, severity, paths, ranges, subjects,
diagnostic content, duplicate findings, exception application, incompleteness,
or exit status. Record version-specific message substitutions explicitly instead
of broadly stripping text. Validate each raw response against its own protocol
before comparison so normalization cannot hide invalid evidence.

Compare graph nodes/edges, package roots, compilation/reporting scope, and
actual input identities as well as findings. Baseline internals may not expose
all provider evidence; assert those candidate invariants directly and test their
observable consequences on both sides. Protocol-specific fields need valid
semantics, not identical wire bytes.

### Make the tests able to fail meaningfully

The runner itself needs ordinary boundary tests that deliberately remove a
finding, truncate coverage, change an exit status, inject a forbidden edit, or
expand diagnostic scope and confirm that comparison fails. Empty reports from
both sides must not pass when the fixture seeds a known defect.

Every analysis rule gets valid and invalid examples; every boundary gets denied
and permitted examples. Assert exact intended failures for malformed metadata,
missing tools, incomplete graphs, unsupported syntax, cancellation, and limits.
Use direct assertions in addition to differential comparison, because two
implementations can share the same defect. Broad mutation campaigns remain
supplemental and require their normal explicit trigger.

### Required scenario dimensions

| Dimension            | Required cases                                                                                                                                                                      |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Command scope        | Full repository, explicit file/module, Git changes, metadata-only changes, deleted/renamed files, empty selection, online/offline profiles                                          |
| Diagnostic authority | Selected local files, unchanged compilation dependents, package-wide dead code, selected architecture dependency closures, unrelated projects excluded                              |
| Ownership            | Single pack, mixed packs, per-capability claims, conflicts, partly foreign-owned units, missing/corrupt/incompatible pack, unclaimed files                                          |
| Project layout       | Root, nested-only, multiple independent packages, workspaces, shared metadata, configured and inferred projects where supported                                                     |
| File safety          | Generated output, mapped package outside physical ancestry, parse-only data, tests, invalid UTF-8, BOM/line endings, links, special files, escaping paths, concurrent input changes |
| Policy               | Defaults, explicit enable/disable, threshold boundaries, exact exceptions and expiry, target configuration unable to weaken baseline                                                |
| Failure isolation    | One broken pack/tool/configuration preserves useful unrelated diagnostics and produces visible incomplete/failure evidence                                                          |
| Scale                | Large relevant unit, many unrelated files, unrelated oversized asset, bounded messages/comments, repeated capabilities without whole-repository hashing                             |
| Execution            | Exact runtime, absent/wrong host tool, sealed environment, target scripts/configuration not implicitly executed, approved network/output authority, timeout/cancellation            |
| Evidence and reuse   | Deterministic facts, actual reads, tamper rejection, affected receipt invalidation, unrelated receipt preservation, no pack-triggered supplemental work                             |

Use a behavior-to-scenario matrix instead of blindly multiplying every dimension.
Every applicable dimension must have assigned cases; all known high-risk
interactions require explicit combined fixtures. Record the rationale for
inapplicable combinations. Keep quick pack boundaries and focused fixtures in
ordinary verification; run the broad installed/platform matrix at its declared
CI event. A platform is not passed because its cell was skipped locally.
Record subprocess counts, bytes read/hashed, elapsed time, and peak memory for
representative small and large cases. Set explicit regression budgets from the
reference measurements before extraction, separating deterministic I/O assertions
from noisy timing measurements. Pack startup and duplicate tool execution must
not turn focused checks into repository-wide work.

### Mandatory provider regression corpus

Carry forward the regressions repaired in the v0.25.0 origin as named, direct
assertions:

- `a.ts` changes and an unchanged importing `b.ts` receives the type error.
- A removed entry or metadata-only change reports newly unreachable unchanged
  files at the required package scope; unrelated owned packages retain the
  current full/focused dead-code semantics.
- Python-only selection does not invoke unrelated JavaScript architecture;
  one selected JS project reaches its dependency closure, not every project.
- An unavailable pack fails its declared claims without hiding unrelated
  language/capability diagnostics; wholly absent metadata does not invent claims.
- With no root package.json, `frontend/package.json` and its TypeScript config
  own `python_pkg/generated/bundle.js` through `context`. Lint, typecheck,
  architecture, and dead-code use the frontend context. Findings keep the bundle
  path, and format write leaves its bytes unchanged.
- Unique `tsconfig.app.json`, jsconfig, explicit React disabling, conventional
  root/src cli/index/main and config entries, manifest/Astro entries, and literal
  bracket-containing route paths preserve their intended behavior.
- An unclaimed Vue/Svelte file does not invalidate unrelated provider-owned
  JS/TS analysis; genuinely mixed compilation ownership is reported explicitly.
- One-file lint/format does not hash unrelated assets or fail on an unrelated
  repository-size limit; necessary context still has enforced budgets.
- Invalid UTF-8 is never decoded and written back with replacement characters.
  CommonJS `.cts`, actual project roots, bounded comments/notes/subjects, and
  useful source diagnostics despite broken optional metadata remain covered.

## Protocol and policy work before extraction

First produce a capability-gap table against manifest v2/protocol v3. Include
builds, dependency facts/audits, project discovery, test recognition/impact,
portability, policy activation, and runtime distribution, not only the existing
analysis operations. Give every missing capability an owner and an executable
contract fixture before selecting the new protocol shape.

The implementation audit records these replacement-contract gaps. Status describes
the implementation branch, not a public compatibility stage.

| Boundary                    | v3 gap                                                                                                                                    | Replacement owner                                     | Executable proof                                                                                                 | Status                                                                                                                                                                                                                                                                                                        |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Complete language ownership | A pack claims individual command capabilities, so an omitted capability can return to native analysis.                                    | Core routing and exact selected manifest              | Provider ownership cases for omitted, unavailable, conflicting, and unrelated paths                              | Manifest v3/protocol v4 fail closed inside each selected pack boundary; explicit unsupported declarations inventory intentional absence without a no-op command or native fallback.                                                                                                                           |
| Project discovery           | Core constructs JavaScript package/configuration units and has no pack-owned discovery result.                                            | Pack discovery with generic core validation           | Nested, metadata-only, generated-source, overlap, malformed-metadata, and large-unit fixtures for all four packs | Generic static discovery, bounded opaque scope data, engine-issued handles, JavaScript discovery, and Python nearest-`pyproject.toml` discovery are implemented. Python quality fixtures exercise the installed scope; its remaining cases and complete four-pack evidence remain open.                       |
| Execution authority         | Only contained adapters and one policy-owned Node runtime are expressible.                                                                | Manifest command contract and core toolchain resolver | Self-contained and exact host-toolchain positive/negative fixtures on supported platforms                        | Exact multi-tool declarations, verified release bytes, one optional launcher, sealed path bindings, and no-network authority are implemented; supported-platform evidence remains open.                                                                                                                       |
| Governed transport          | One 8 MiB JSON request carries JavaScript-shaped units and at most 10,000 hashed inputs.                                                  | Core inventory transport                              | Small and large relevant units, unrelated large assets, cancellation, and resource budgets                       | Generic inventory/selection/scopes replace JavaScript units; request, response, inventory, context, and opaque-data bounds are implemented. Large-project and cancellation evidence remains open.                                                                                                             |
| Build outcomes              | `build` names a command but returns no structured targets or coverage.                                                                    | Pack facts and core coverage enforcement              | Configured build pass, diagnostic, incomplete, and unavailable-tool fixtures                                     | Open.                                                                                                                                                                                                                                                                                                         |
| Dependency facts and audits | Commands can claim manifest paths but cannot return a validated resolved graph, lock reconciliation, licenses, age, or advisory evidence. | Pack ecosystem readers and generic core policy        | Frozen, offline, and online dependency workflow fixtures per language                                            | Open.                                                                                                                                                                                                                                                                                                         |
| Test recognition and impact | Protocol carries no language test or impact facts.                                                                                        | Pack facts consumed by the core scheduler             | Recognition, ownership, focused impact, and unrelated-change fixtures                                            | Manifest-owned source patterns, shebang recognition, and language-specific test patterns are implemented; impact facts and four-pack evidence remain open.                                                                                                                                                    |
| Portability                 | Protocol carries no language portability facts or platform/tool support result.                                                           | Pack facts and core portability policy                | Shell dialect and unavailable-platform fixtures plus declared platform cells                                     | Bounded located literal facts and core-owned machine-path, sibling-reference, and external-input decisions are implemented; Shell and platform evidence remains open.                                                                                                                                         |
| Policy activation           | Request policy and source context contain JavaScript-specific activation fields.                                                          | Locked core policy plus pack-owned interpretation     | Explicit enable/disable, generated inheritance, and counterexample fixtures                                      | A bounded generic declaration channel now binds opaque versioned policy data to the exact pack, capability, and validated scope handles. Python framework and reachability declarations are transferred without Python-shaped universal fields; pack interpretation and full activation evidence remain open. |
| Distribution and migration  | Only local source install, verify, and root exist; there is no authenticated catalog, list, update, remove, or migration preview.         | Pack lifecycle and upgrade transaction                | Local fixture catalog, atomic failure, preview, rollback, and installed-release cases                            | Authenticated local lifecycle and explicit repository migration plan/apply/rollback are implemented. The installed pre-cutover upgrade sequence and full first-party pack set remain open.                                                                                                                    |

The engine supplies canonical governed paths, module ownership, data/generated
classifications, validated source-context mappings, effective locked policy, and
explicit full-versus-focused intent. Pack discovery interprets ecosystem metadata
and returns bounded project/target/membership facts; core validates identities,
paths, ownership, and permitted scope before authorizing analysis. Packs must
not reconstruct engine policy from physical ancestry or target dependencies.

Preserve four separate concepts throughout discovery and execution:

1. Read context: the relevant governed inputs required for the discovered scope.
2. Selected targets and required coverage: what triggered this operation and
   what it must analyze or explicitly mark incomplete.
3. Diagnostic scope: the scope or dependency closure permitted to report findings,
   including unchanged sources when the capability requires it.
4. Write targets: only the selected, writable source paths authorized by core.

A discovered scope cannot grant itself arbitrary diagnostic or write scope.
Validate closure facts and exact per-capability ownership; share only the context
needed by that operation. Core normalizes repository-policy globs to literal
paths. Packs add ecosystem entry facts within inventory, while generated outputs
retain their physical read and finding paths and remain non-writable.

Define static discovery and exact self-contained/host-toolchain execution for
these packs. Keep graph discovery conditional so local lint can retain useful
results when unrelated metadata is malformed. Do not introduce evaluated target
code execution to make extraction easier. Preserve existing declared build/check
execution authority separately from static discovery; additional authority needs
an explicit reviewed contract.

Resolve bounded transport for large relevant units, deterministic identity reuse,
strict response validation, byte-safe UTF-8 handling, cancellation, and output
containment before freezing a replacement protocol. Do not replace a global
file cap with silent truncation. Keep ecosystem structure pack-owned while core
validates the generic evidence needed to enforce its guarantees.

## Pack authoring and certification gate

Treat pack authoring as a supported product surface, not an exercise in reading
core implementation. Before any language extraction, the companion protocol plan
must produce:

- versioned machine-readable schemas for every manifest, request, response, and
  evidence document, mechanically checked against the production decoder;
- executable minimal adapters and request/response fixtures for every operation,
  profile, mode, discovery type, and execution type used by the first-party packs;
- language-neutral test vectors or small reference helpers for canonical paths,
  input identities, coverage accounting, diagnostic scope, and dependency
  closure, without requiring another runtime to reproduce Go internals;
- bounded validation errors that identify the exact field and collection index,
  reject the offending value safely, and state the expected constraint;
- one local command that runs the same production validation used after
  installation and emits stable machine-readable results for editor and CI use.

Certification has three distinct evidence layers:

1. Protocol conformance proves that requests and responses obey the contract,
   including malformed and boundary cases.
2. Capability behavior proves valid counterexamples, seeded defects, incomplete
   coverage, policy activation, and framework-specific semantics across each
   declared supported version and configuration.
3. Installed product integration exercises the exact pack identity in
   representative disposable applications through ordinary checks, format check,
   format write, `doctor --strict`, and applicable gates. Format write also proves
   edits, idempotence, and byte preservation outside write authority.

The existing SQLite proof remains one executable example, not the authoring kit
or framework-readiness evidence. Wire-compatible field-specific diagnostics may
improve protocol v3 before replacement, but do not create a long-lived v3 SDK or
compatibility layer. Publish the durable kit with the replacement contract and
use it for first-party and third-party packs alike.

## Official pack distribution and repository transition

The initial set is `go`, `python`, `javascript-typescript`, and `shell`.
JavaScript/TypeScript share one runtime and project ecosystem. The existing
optional artifact is named `javascript`; its transition to the final identity
must be explicit in the migration preview and pin update, without an alias or
hidden fallback. Preserve all of its supported capabilities.

Installed and selected are different states. Installation adds one exact
name/version/digest to the immutable store; repository selection pins that
identity in `.code-polishy.json`. No implicit downloads, upgrades, machine-global
selection, target execution, or ambient toolchain lookup are introduced.

Define a release-authenticated catalog with exact artifact URL, digest, size,
one exact engine version and protocol, supported platforms, discovery/execution modes,
tool and dependency inventories, licenses, and provenance. Local packs use the
same protocol and validation; an official-looking name does not authenticate a
local artifact. Installation verifies bytes before an atomic, code-free store
update. Offline CI uses preprovisioned artifacts and exact pins.

Reuse the current engine publication model where its trust primitives fit: a
bounded versioned index, separately reviewed index digest, exact per-platform
artifacts, verified installation, immutable storage, and offline reuse. Pack
artifacts keep their own catalog namespace, receipts, compatibility identity,
and lifecycle. The publication-backed v2 repository lock continues to select
the engine only; exact pack selection remains explicit repository policy. Do not
make an engine-lock update silently install, select, update, or remove a pack.

Implement and document a concrete lifecycle, starting from these proposed forms:

```text
code-polishy pack catalog --catalog PATH --sha256 DIGEST
code-polishy pack install --official NAME@VERSION --catalog PATH --sha256 DIGEST
code-polishy pack update NAME --to VERSION --catalog PATH --sha256 DIGEST
code-polishy pack remove NAME@VERSION [--digest DIGEST]
code-polishy pack verify --source PATH
code-polishy pack list [--format human|json]
```

Update previews show capability, authority, toolchain, provenance, and policy
changes. `pack list` and `doctor --strict` distinguish installed, selected,
missing, incompatible, and corrupt releases. Removing a selected pack does not
rewrite repositories; their next run fails clearly for its coverage while other
packs and repository services retain useful diagnostics.

Before removing native support, provide a deliberate repository migration flow:

1. Inventory existing native and optional-provider coverage and any custom claims.
2. Preview exact compatible packs, policy/pin changes, tools, and required setup.
3. Install only the explicitly authorized artifacts, without changing selection.
4. Verify the candidate configuration in temporary state and compare required
   capability coverage, including generated-source ownership and custom checks.
5. Atomically update repository pack policy only after success, without changing
   the engine lock. Failure leaves prior selection and repository bytes intact;
   a verified unused pack may remain in the immutable store and is reported as
   such.
6. Demonstrate rollback using the preserved prior engine and configuration, with
   no dual implementation embedded in the new engine.

Use this ordered cutover without publishing an intermediate compatibility release:

1. Finish and verify the replacement contract, authoring kit, local lifecycle,
   migration preview, and every parity-complete pack before public cutover.
2. Build and authenticate the cutover engine, exact pack set, and catalog without
   publishing them. Use the incoming candidate to preview and verify the explicit
   pack-policy migration while the repository lock still names the old engine.
   Beginning that migration is an intentional hard cutover: the old engine need
   not continue operating against the new policy, and rollback restores the prior
   policy before the old engine is used again.
3. Publish one coordinated breaking release that replaces protocol v3 and removes
   native language ownership together. Its `upgrade plan` rejects an unprepared
   repository before authority changes and evaluates the incoming release with the
   repository's already selected exact packs. `upgrade apply` changes managed
   guidance, wrappers, and the engine lock with the lock last; it never rewrites
   pack pins, translates old manifests, or supplies a native fallback.

The old engine never runs a protocol it cannot understand. Only the user's
explicit incoming migration command may update pack policy before the engine lock;
that begins the hard-cutover window in which the repository must either finish
the engine upgrade or roll the policy back. Prove interruption and rollback
separately for pack-policy migration and for current two-phase engine upgrade,
starting from a real pre-migration installation. Follow the then-current release
checklist for publication and lock changes rather than freezing that command
choreography in this plan.

Use local authenticated fixture catalogs for development. Artifact publication,
credentials, and live distribution checks are separate authorized release work.

## Milestones and exit gates

### Phase 0: Complete the inventory and references

Audit all surfaces above, resolve shared ownership, record platform/tool support,
pin reproducible current-main, immutable-origin, and provider references, and
record every behavior delta after the audit revision. Classify unsupported
behavior and intentional changes without disguising either as parity.

Exit: every discovered feature and policy path has a ledger row, owner, reference,
and planned executable cases; the recorded task base matches the implementation
branch. Open gaps are visible; this phase does not assert that the cases already
pass.

### Phase 1: Land the conformance framework first

Build the ledger validator, fixture materializer, dual-installation CLI runner,
strict comparator, and structured evidence report. Reuse existing meaningful
fixtures and add missing positive, negative, focused, safety, and failure cases.
First prove reference-versus-reference reproducibility and deliberate mismatch
detection, then run the current optional JS provider and one non-JavaScript
adapter to expose remaining protocol and authoring gaps. Prototype the versioned
schemas, field-indexed diagnostics, language-neutral test vectors, and the three
certification layers with this same framework.

Exit: the runner detects seeded loss of diagnostics, coverage, and write safety;
all required native reference cases execute; optional-provider gaps are explicit;
and a pack author can diagnose the seeded malformed fixtures without reading core
source. No extraction starts before this independent measurement exists.

### Phase 2: Finish the generic contract and local lifecycle

Implement only the capability/discovery/toolchain gaps needed by the ledger.
Add authenticated local catalog fixtures, atomic lifecycle operations, missing
pack isolation, and a core-only installed smoke test. Provide each new boundary
with a quick ordinary suite and test conflict/malformed evidence cases. Implement
and continuously verify the production-checked schemas, examples, reference
vectors or helpers, validator output, and installed-product certification
workflow that will ship with the replacement contract.

Exit: realistic fixtures for all four languages fit the contract without core
interpreting their ecosystem metadata. Local pack lifecycle and migration
previews work without publication. A pack cannot weaken locked policy or acquire
more read, write, diagnostic, network, or supplemental authority. Protocol,
capability, format-write, and installed-integration evidence are distinct and
machine-readable. Phase 2 is the readiness gate for governed language extraction.

### Phase 3: Build the complete shell pack

Move supported Bash/POSIX source recognition, syntax/comment handling,
ShellCheck integration, portability facts, tool/platform declarations, and
applicable configured-check behavior. Inventory formatting rather than inventing
a new formatter during migration. Keep core launcher/build scripts independent
of whether a user's repository selects the shell pack.

Exit: the complete shell ledger passes through the installed pack, generic
core-only repository services operate without selecting it, and unsupported
host/tool cases fail as specified. Native shell ownership remains only for
unclaimed paths and capabilities until the migration release sequence below.

### Phase 4: Build the complete Python pack

Move the validated static project inventory, source-facts adapter, Ruff/Vulture/ty,
CPython/uv/tool distribution, framework/runtime contracts, architecture,
reachability state, and ecosystem dependency evidence. Preserve explicit `.venv`
inputs and all existing dynamic-reference and provenance limits.

Status (2026-09-22): the executable quality slice owns nearest-`pyproject.toml`
static discovery, isolated Ruff format/lint/complexity, `ty` type checking, and
CPython comment/docstring/function facts. Ruff uses no cache, format writes
return only core-authorized edits, type checking covers unchanged scope members,
and eight built-pack fixtures prove pass and seeded-defect outcomes. Dead code,
architecture, dependency evidence, project-version derivation, environment
resolution, framework contracts, and the shared conformance matrix remain open.

Exit: every Python ledger row passes, including adoption, nested/generated
projects, framework contracts, dependency failures, and focused scopes. Python
semantics and tool setup have one selected pack owner, unrelated packs keep
operating when Python is absent or broken, and native ownership remains available
only to unclaimed paths and capabilities during migration.

### Phase 5: Consolidate JavaScript and TypeScript

Use the existing provider as a starting point, fill its complete native feature
ledger, and consolidate the native/provider graphs and policy mechanics into one
owner. Preserve Astro, JS checking, jsconfig, aliases/assets, and all mandatory
provider regressions. Move dependency/build policy adapters and independent
runtime distribution as well as source analysis.

Exit: both native-preservation and provider-enhancement lanes pass; generated
ownership and non-writability hold across capabilities; nested-only packages
work; and the optional `javascript` selection has an explicit transition. The
pack supplies its independent runtime; native JS analysis remains only for
unclaimed paths and capabilities until the removal release.

### Phase 6: Build the complete Go pack

Move project/package discovery, gofmt/vet/staticcheck, type/complexity/dead-code
behavior, source facts, builds, and module/dependency/vulnerability adapters.
Choose verified self-contained tools or an exact declared host-toolchain model
based on the existing platform requirements and prove it with installed fixtures.

Exit: all Go rows pass, including build tags, nested modules, workspace policy,
local replacements, failure handling, and focused scopes. Go may remain the
engine's implementation language; selected user-source Go analysis is wholly
pack-owned while native ownership remains available only for unclaimed paths and
capabilities during migration.

### Phase 7: Prepare the cutover and prove repository migration

Freeze the replacement protocol and authoring kit only after all four packs pass
their ledgers. Prepare one cutover engine, exact pack set, and authenticated
catalog without publishing a mixed native/pack release. Update permanent docs,
schemas, examples, release manifests, and website together. Publication remains
separately authorized after native removal and final evidence are complete.

Run all 16 pack-selection subsets as installed smoke tests, with full per-pack
feature coverage and explicit mixed-language interaction fixtures. Include
missing/corrupt packs and claim conflicts, not only healthy installations. On
every declared platform, exercise real installed executables and exact tools;
cross-compilation alone is not runtime evidence. Record unsupported combinations
honestly rather than silently skipping them.

Exercise current `upgrade plan` / `upgrade apply` from real pre-cutover locks,
then run the separate explicit pack-policy migration. Prove interruption and
rollback at both boundaries. The engine upgrade must neither select packs nor
edit their pins; the pack migration must not replace the engine lock.

Exit: every required ledger/platform cell has evidence for the unpublished
cutover candidate; exact protocol and pack artifacts are authenticated; and
representative repositories can migrate and roll back without an authority gap.
No public release or repository can select both old and replacement protocols.

### Phase 8: Remove native ownership and residue

Audit core imports, language switches, schemas/defaults, commands, tool pins,
installers, assets, docs, test recognition, supply-chain readers, and release
builders. Remove native language discovery, tools, source rules, fallbacks, and
obsolete tests; move useful tests to their final owners. Preserve generic
repository services with direct core-only fixtures.

Remove native ownership in the same candidate prepared by Phase 7, then repeat
the installed matrix. Repositories missing required packs fail clearly before
useful unrelated diagnostics are lost. The
incoming upgrade plan rejects missing, corrupt, incompatible, or incompletely
selected packs before lock cutover and reports the exact remediation.

Exit: there are no unexplained differences, language semantics, or hidden native
fallbacks in core; offline setup, explicit pack migration, doctor, format/check,
CI, gates, and the removal-release upgrade agree on identity and coverage. Only
then may an authorized public release expose the replacement protocol and packs.

## Verification and delivery rules

During implementation, run the narrowest useful exact suite after a coherent
change. Add shared conformance and installed smoke suites to their appropriate
ordinary events; policy declarations alone do not authorize supplemental work.
The [Verification and Testing Policy](../policies/verification.md) remains the
owner of test scheduling, artifacts, receipts, and gate reuse.

At each task boundary, use the then-current locked agent workflow, design context,
and event-selected verification. This plan fixes outcomes and gates, not a stale
copy of evolving task-start, checkpoint, upgrade, or release commands.

Checkpoint task-owned progress at meaningful milestones. For completed code work
on the implementation branch, use the locked workflow's checkpoint gate against
the previous checkpoint. At a real merge into `main`, use one final gate owned by
the configured local or CI authority, with `main` as the explicit merge target.
Do not run duplicate full gates solely because the same candidate moves between
stages.

Bind reports to reference and candidate commits/artifacts, fixture and ledger
digests, toolchains, platform, and protocol. Reuse evidence only when its complete
identity still matches. Reference artifacts remain test inputs, outside the
shipped engine; preserving them is not a runtime compatibility layer. Carry
portable contract fixtures forward after migration so later pack releases face
the same observable requirements.

Ordinary plan-only edits require Markdown formatting and documentation checks,
not application tests. Publishing, pushing, opening pull requests, and running
credentialed/live/supplemental probes retain their explicit authorization rules.

## Completion criteria

- All four packs independently install, select, update, remove, and verify using
  exact identities and the same trust boundary as third-party packs.
- Every existing behavior has an inventory row and meaningful executable
  assertions, with no untested required row at a language cutover.
- Differential and direct assertions preserve findings, coverage, policy,
  diagnostic scope, writes, failures, evidence, and platform behavior.
- The existing JS/TS provider's supported improvements remain available.
- Missing or broken packs cannot produce silent coverage loss or suppress
  unrelated diagnostics.
- The core has no migrated language semantics or fallback; shared repository
  services and generic policy continue to work without language packs.
- Migration and rollback are proven from real pre-migration installations.
- A pack author can implement and locally certify the public contract without
  reading core source, and every rejected field identifies its exact constraint.
- Protocol conformance, capability behavior, framework behavior, format write,
  and installed product integration have separately attributable evidence.
- Release surfaces describe one coherent ownership model, supported by exact
  final-candidate evidence rather than a parity claim based on code inspection.
