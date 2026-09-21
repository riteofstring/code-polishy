# Language-Pack Discovery and Universal Capabilities

Status: proposed replacement-protocol direction; refreshed 2026-09-20 against
`main` at `c7c83ff00fa57ab91bb63895dd79508641972d41` with locked release
`v0.27.8`; current pack-contract origin `v0.25.0`

## Relationship to the first-party migration

The [first-party migration plan](installable-first-party-language-packs.md)
owns the complete feature inventory, equivalence runner, and extraction sequence
for shell, Python, JavaScript/TypeScript, and Go. The planning documents were
incorporated into `main` after v0.25.0; implementation begins on a separate branch,
and its test framework and authoring contract land before native implementations
are removed.

The 2026-09-20 audit found the pack runtime, optional JavaScript provider, pack
schema, example pack, and authoring guide unchanged from v0.25.0. The current
contract therefore still uses manifest version 2 and protocol version 3 even
though the surrounding product is now locked to v0.27.8. This document describes
a future contract, not the current wire schema. Preserve protocol v3's effective
generated-source context, per-capability ownership, unit-wide diagnostic
authority, focused architecture closure, and restricted write targets when
moving ecosystem discovery into packs.

The same audit confirms that current request construction remains shaped around
JavaScript package manifests, tsconfig/jsconfig, pnpm/Astro metadata, and
React-specific lint activation. Current fixture verification always uses check
mode, and malformed source facts can still collapse several field failures into
one generic error. Those are active contract limitations, not evidence that the
broader runtime is frozen. Recheck the delta from the audited revision at each
implementation task base.

Implement the contract needed by those four existing language groups first.
The broader ecosystem prototypes below are a separate research track. They are
required evidence before claiming support for those additional discovery models,
not prerequisites for migrating the current languages. Choose version numbers
when the replacement schema is concrete and cut over all affected release
surfaces together, without a compatibility translation layer.

## Outcome

Make language packs predictable across ecosystems without pretending every
language has the same kind of project.

Every pack must declare one discovery mode: `file-scoped`, `static`, or
`evaluated`. Project discovery is required only for capabilities that need a
project or build graph. Evaluated discovery requires explicit approval because
it can execute repository-controlled build logic and may need a host toolchain.

Code Polishy owns the governed file inventory, execution boundary, evidence,
installation integrity, and gates. Each pack owns its ecosystem-specific
interpretation and tools.

## Why this shape

The word “project” does not mean the same thing across ecosystems:

- Cargo and pub expose mostly static workspace and package structure;
- Gradle, SwiftPM, Bundler, and CMake can evaluate code or environment-dependent
  configuration;
- .NET combines solutions, projects, imported properties, and targets;
- SQL and Protobuf usually belong to a framework or build system rather than a
  standalone language project model;
- aggregate workspaces, shared locks, generated sources, and overlapping build
  targets make exactly-one-project ownership invalid.

A universal project DTO would either lose important meaning or grow into a copy
of every build system. The common contract should instead describe inputs,
execution authority, capability outcomes, and verifiable evidence.

## Product boundary

| Code Polishy owns                                        | A language pack owns                                   |
| -------------------------------------------------------- | ------------------------------------------------------ |
| Exact pack selection and receipt verification            | Recognizing its ecosystem layouts                      |
| Complete governed file inventory and active selection    | Interpreting manifests, locks, and build metadata      |
| File classifications and repository modules              | Resolving packages, targets, imports, and dependencies |
| Module dependency direction                              | Running ecosystem-specific tools                       |
| `scope.data` parse-only and no-rewrite rules             | Producing structured findings and evidence             |
| Ordinary and supplemental execution boundaries           | Declaring supported capabilities and discovery mode    |
| Timeouts, isolation, artifacts, reports, and receipts    | Declaring any host toolchain requirements              |
| Checkpoint and merge gates                               | Shipping conformance fixtures                          |
| Versioned schemas, production validators, and author kit | Ecosystem examples and capability-specific test cases  |

Language packs remain execution and evidence adapters. Repository services such
as GitLab CI remain built-in policy. A broader plugin system is a separate
product decision.

## Contract decisions

### 1. Require an explicit discovery mode

Every language entry in a pack manifest declares exactly one mode:

| Mode          | Meaning                                                                           | Approval                       |
| ------------- | --------------------------------------------------------------------------------- | ------------------------------ |
| `file-scoped` | Commands use governed files directly and do not claim a project graph.            | No additional approval         |
| `static`      | The pack reads declared files without executing repository-controlled code.       | No additional approval         |
| `evaluated`   | The pack may invoke build-system logic that evaluates repository-controlled code. | Explicit target-owner approval |

The declaration is mandatory. Running discovery is conditional:

- formatting and file-local linting may use `file-scoped`;
- architecture, build, lock synchronization, and dependency policy require
  `static` or `evaluated` discovery when their evidence depends on a graph;
- a pack must omit a capability it cannot support honestly in its declared
  mode;
- there is no silent fallback from `static` to `evaluated` or from discovered
  scope to file-only behavior.

The pack author chooses and implements the mode. Setup automation may recommend
a compatible pack and explain the tradeoff. It does not invent discovery logic
or silently choose greater execution authority.

### 2. Treat evaluated discovery as an execution boundary

Evaluated discovery can run repository-controlled configuration such as Gradle
settings, Ruby gem specifications, Swift package manifests, or CMake scripts.
Before the first evaluated run, Code Polishy asks the target owner to approve:

- the exact pack name, version, and digest;
- the declared host executable and version constraint, if any;
- that repository-controlled build logic may execute;
- the network policy and writable paths;
- the capabilities that will consume the result.

Approval is exact and stored in target configuration. A pack version, digest,
toolchain contract, or execution-policy change invalidates it. Non-interactive
runs fail with a clear instruction when approval is missing.

The prompt should describe the concrete authority, for example:

> This Gradle pack needs evaluated discovery. It will run the approved Gradle
> executable and may evaluate this repository's build configuration. Allow this
> exact pack release and toolchain contract?

### 3. Separate repository inventory from active selection

Every operation keeps four bounded scopes separate:

- `inventory`: governed, pack-relevant read context needed for the analysis;
- `selection`: the files or metadata that triggered this operation, with explicit
  full-versus-focused intent and required capability coverage;
- `diagnostic scope`: validated analysis members or dependency closures on which
  this capability may report findings, including unchanged sources;
- `write targets`: selected source files that core explicitly allows to change.

Inventory entries include engine-owned classifications such as executable
source, module, generated output, parse-only data, control file, and dependency
input, plus exact per-capability ownership and validated effective source-context
mappings. Paths remain canonical repository-relative physical paths. A generated
file's effective package context never changes its finding path or write status.
Core supplies effective locked policy and normalizes repository-policy globs to
literal paths. Packs interpret ecosystem metadata within that authority.

The adapter cannot expand read authority by walking the repository. Discovery
returns candidate membership and closure facts; core validates their inputs,
ownership, and capability-specific scope before analysis. A typecheck may report
an unchanged compilation member, and dead-code analysis may report package-wide
findings. Architecture follows selected dependency closures. None of these
expands formatting writes, and generated source and `scope.data` remain
non-writable. Local operations need not discover unrelated project graphs.

### 4. Keep discovery results ecosystem-specific and bounded

Discovery returns structured evidence and an opaque, canonical analysis-scope
document owned by the pack's protocol schema. Code Polishy validates generic
properties only:

- strict JSON shape, protocol version, size, count, and depth limits;
- canonical paths contained in the supplied inventory;
- exact per-capability ownership and validated membership/closure evidence for
  the requested diagnostic scope, without granting additional write authority;
- deterministic canonical scope content and an engine-issued invocation-local
  scope handle;
- no unknown fields, extra JSON values, or evidence-free success;
- no capability result referring to a different or unknown scope handle.

Code Polishy does not require a universal `{root, sourceRoots, manifests,
locks}` project object, exactly-one file ownership, or non-overlapping projects.
A Cargo adapter may describe workspaces and packages; a Gradle adapter may
describe builds and projects; a CMake adapter may describe configured targets.

Validated discovery facts are reused by reference within one engine invocation;
the engine does not rehash trusted in-memory state to detect changes. When a
report or reusable receipt crosses a trust or persistence boundary, core derives
one canonical evidence identity from the validated scope and records it there.
Each capability retains its own selection, coverage, diagnostic, and write
authority; local work need not obtain a graph it does not use. Packs may return
a concise display summary for diagnostics, but the engine does not infer policy
from display text.

### 5. Identify packs by ecosystem provider

Pack identities should name the system they interpret when language alone is
ambiguous. Examples include `rust-cargo`, `jvm-gradle`, `jvm-maven`,
`swift-swiftpm`, `ruby-bundler`, `php-composer`, `cpp-cmake`, and `dotnet-msbuild`.

One pack may cover multiple source languages used by that ecosystem. A SQL or
Protobuf pack should identify its actual authority, such as dbt, Flyway, Buf,
or Bazel, when project discovery depends on that system.

Provider conflicts are resolved at the same authoritative boundary used for
built-in and repository-owned providers. Code Polishy rejects ambiguous
ownership rather than guessing which provider wins.

### 6. Declare toolchain execution explicitly

Every command declares one execution type:

- `self-contained`: every executable ships inside the verified pack;
- `host-toolchain`: the command uses exact host executables under a declared
  version and platform contract.

Host-toolchain commands must declare executable resolution, accepted versions,
environment inputs, network policy, and writable paths. Code Polishy resolves
and validates the tool before execution and records its identity in evidence.
It never relies on ambient `PATH` lookup.

Evaluated discovery commonly needs a host toolchain, but the two concepts remain
separate. A self-contained parser can still perform static discovery, and an
ordinary capability may need an approved compiler without evaluating discovery.

### 7. Preserve universal engine guarantees

The following remain engine-owned for every mode and ecosystem:

- JSON, JSONC, YAML, and YML under `scope.data` are parsed without byte
  rewriting;
- generated executable source remains governed;
- supplemental mutation and risk suites require an explicit trigger or the
  stable-release-candidate workflow;
- packs cannot declare test suites or a `supplemental` command profile;
- packs write only to engine-owned per-execution temporary and artifact paths;
- packs cannot decide suite scheduling or receipt reuse;
- receipt identity includes the exact pack, capability, discovery scope, and
  toolchain inputs, so relevant changes invalidate affected evidence while
  unrelated prose-only changes do not;
- installs remain content-addressed, atomic, read-only, and verified before and
  after adapter execution;
- findings remain restricted to governed paths and the command's allowed scope;
- checkpoint and merge gates keep their normal evidence and report rules.

Capability names define outcomes rather than tools. A pack may use Cargo,
Clippy, Gradle, Ruff, Composer, or another ecosystem tool, and must omit any
optional capability it cannot support faithfully.

The [Verification and Testing Policy](../policies/verification.md) owns the
artifact, receipt-reuse, final-gate, and suite-deduplication contracts. This
pack protocol supplies exact inputs to that engine policy; it does not create a
second scheduler.

### 8. Make pack authoring a supported contract

An author must be able to build and diagnose a pack without reading Go source or
reverse-engineering the optional JavaScript provider. The replacement contract
ships one versioned authoring kit with:

- machine-readable schemas for manifests and every request, response, discovery,
  scope, finding, fact, edit, and evidence document;
- executable valid and invalid examples for every operation, mode, profile,
  discovery type, and execution type;
- language-neutral test vectors or small reference helpers for contained paths,
  input identities, coverage accounting, diagnostic authority, and dependency
  closure;
- a local validator that uses the production decoder and produces stable
  machine-readable results;
- field-indexed errors such as `facts.comments[0].kind`, with a bounded offending
  value and exact expected constraint, while never printing source or environment
  content unnecessarily.

Mechanically check schemas, examples, validator behavior, and production types in
one ordinary contract suite so they cannot drift. An SDK for a particular runtime
is optional; normative schemas and test vectors are not. Do not create a durable
protocol v3 compatibility SDK. Wire-compatible diagnostic improvements may land
earlier, while the durable kit cuts over with the replacement protocol.

Keep three certifications distinct:

1. Protocol conformance validates transport and evidence shape.
2. Capability conformance validates real seeded defects, valid counterexamples,
   coverage, policy, and ecosystem or framework semantics across each declared
   supported version and configuration.
3. Installed integration validates the exact installed identity in representative
   disposable applications through ordinary product commands, including format
   check and write, idempotence, unrelated-byte protection, `doctor --strict`, and
   applicable gates.

Passing one layer never implies another. `pack verify` may orchestrate all three,
but its result records them separately and states what was not exercised.

## Scale requirements

Measure the current request, context, per-file, and response bounds against
large relevant units before freezing a replacement protocol. Protocol v3 already
scopes context to the operation; preserve that isolation. An unrelated large
asset or repository file count must not block one-file formatting or linting,
while genuinely oversized required input must fail explicitly.

The chosen transport must:

- preserve complete relevant analysis context without one unbounded JSON message;
- use deterministic bounded transport with a final content digest, introducing
  paging or streaming only where measurements require it;
- put explicit byte, entry, depth, and time budgets on both sides;
- avoid repeated full-inventory transfer for every capability in one run;
- keep adapters unable to enumerate files outside the governed inventory;
- provide useful typed failures when a repository exceeds a limit.

This plan does not prescribe streaming, a temporary read-only inventory file,
or another transport until prototypes measure the tradeoffs.

## Implementation sequence

### Phase 0: Prove the required discovery models

For the first-party migration, use the four existing language inventories and
shared conformance fixtures from the first-party migration plan. Identify
capability gaps before extraction, including dependency/build adapters, test and
portability facts, policy activation, and toolchain distribution. Prove each
actual project model with small, nested, focused, generated-source, and large-unit
fixtures. Record the exact current-main task base and any delta after the audited
revision. Capture author-facing friction as executable malformed fixtures,
including ambiguous field errors, check-only format verification, and assumptions
that require JavaScript package metadata.

For future expansion beyond those languages, build disposable adapters for four
deliberately different systems:

1. Cargo for a mostly static workspace model;
2. Gradle for evaluated, conditional multi-project configuration;
3. Bundler for executable Ruby manifests and optional lockfiles;
4. CMake for configuration-dependent targets and compiler context.

Each prototype must exercise a small repository and a large or deeply nested
fixture. Record the minimum inventory, toolchain, authority, output, and scale
requirements. Use those results before claiming that the contract supports
these additional ecosystems. Do not introduce evaluated discovery or new host
execution merely to move an existing static analyzer into a pack.

Do not remove the current protocol or require repository migration during
contract prototyping. Keep any replacement cutover coordinated with the owning
migration plan.

### Phase 1: Define the manifest contract

1. Add required discovery mode and command execution type declarations.
2. Add host-toolchain identity, platform, environment, network, and writable
   path fields.
3. Define which standard capabilities are legal for each discovery mode.
4. Define exact stored approval for evaluated discovery and host toolchains.
5. Draft machine-readable schemas for every contract document and mechanically
   check them against production types without freezing final version numbers.
6. Define field-indexed error records, stable machine-readable validator output,
   normative reference vectors, and the three certification layers.
7. Update `docs/adding-a-language.md` with ecosystem-provider guidance and
   executable examples for all three modes.

### Phase 2: Add governed inventory transport

1. Build the governed inventory once and derive the relevant context for each
   operation without repeatedly hashing unrelated repository contents.
2. Classify entries at the authoritative file-policy boundary, including source
   context and exact per-capability ownership.
3. Keep selection, required coverage, diagnostic scope, and write targets
   separate for each operation.
4. Implement the bounded transport selected from Phase 0.
5. Validate the complete logical input before adapter execution.

### Phase 3: Add discovery and scope consistency

1. Add strict discovery requests, responses, typed failures, and evidence.
2. Validate returned paths against inventory and enforce resource limits.
3. Canonicalize each validated scope once and bind it to an engine-issued
   invocation-local handle.
4. Reuse the validated in-memory scope where inputs match while keeping each
   capability's diagnostic and write authority distinct.
5. Reject capability evidence tied to another handle; derive a canonical digest
   only when durable report or receipt evidence requires one.
6. Keep the scope in memory; do not add persistent caching in this change.

### Phase 4: Enforce evaluated and host-toolchain authority

1. Resolve executables without ambient `PATH` lookup.
2. Verify tool versions and platforms before running the adapter.
3. Prompt interactively for exact approval when required.
4. Fail clearly in non-interactive execution when approval is absent.
5. Apply declared environment, network, and writable-path constraints.
6. Record pack and toolchain identity in managed evidence.

### Phase 5: Integrate capabilities and diagnostics

1. Feed relevant inventory, selection, required coverage, and validated discovery
   identity into capability requests, with explicit full/focused semantics.
2. Keep findings within validated capability-specific diagnostic scope and format
   edits within selected write authority; test unchanged dependent diagnostics
   and generated-source non-writability directly.
3. Make architecture evidence include Code Polishy's module graph and the
   pack's ecosystem analysis scope.
4. Make dependency capabilities identify the manifests, locks, or resolved
   graph they actually evaluated without imposing exactly-one ownership.
5. Extend `pack verify` with mode-specific protocol, capability, and installed
   integration certification. Exercise format check and write separately and
   report each layer independently.
6. Make `doctor --strict` report mode, approval, toolchain, platform, provider
   conflicts, capability coverage, and bounded discovery summaries.

### Phase 6: Freeze and cut over the required contract

Only after every affected current language passes its declared conformance
boundary and the reference-versus-pack behavior matrix:

1. freeze the required manifest and protocol schemas with explicit versions;
2. freeze the authoring kit, error format, examples, CLI help, and permanent
   documentation from the same production-checked contract;
3. build and verify exact pack artifacts and their authenticated catalog;
4. verify engine upgrade and explicit pack-policy migration as separate
   transactions, plus install, selection, doctor, format, focused diagnostics,
   gates, rollback, and installed execution on supported platforms;
5. prepare but do not publish the exact engine, pack set, catalog, schemas, and
   authoring kit while any native ownership remains;
6. remove protocol v3 and native language ownership in the same candidate, with
   no translation layer, alias, fallback, or automatic pack-pin migration;
7. obtain release authorization only after migration and rollback evidence proves
   repositories can make the explicit hard cutover to that candidate.

Future ecosystem prototypes remain necessary evidence for expanding evaluated
or target-specific support; their completion is not a gate on the four-language
migration.

No mixed-ownership compatibility release is planned. Development may stage the
new contract before native removal, but the first public replacement release is
a hard cutover and accepts only its current manifest and protocol versions.

## Verification

Add observable boundary coverage for:

- all three discovery modes and their allowed capabilities;
- missing, stale, and changed evaluated-discovery approvals;
- self-contained and host-toolchain executable resolution;
- rejected tool versions, platforms, ambient lookup, environment, and network;
- deterministic inventory transport above the current file and response caps;
- read context, required coverage, diagnostic scope, and write authority for full,
  focused, metadata-only, and empty-selection operations;
- generated-source effective package ownership and unchanged physical paths;
- missing packs and invalid metadata preserving unrelated useful diagnostics;
- canonical paths, links, special files, missing files, and escaping paths;
- deterministic analysis scopes, cross-capability scope-handle consistency, and
  a single durable identity only at report or receipt boundaries;
- aggregate workspaces, shared locks, overlapping targets, optional locks, and
  conditional configuration;
- provider conflicts across built-in, repository-owned, and pack-owned checks;
- format check and write modes never rewriting `scope.data`;
- generated executable source remaining governed;
- pack tampering before, during, and after discovery;
- rejection of supplemental profiles and test-suite declarations in packs;
- contained per-execution outputs and exact receipt invalidation after pack,
  discovery, toolchain, source, and unrelated documentation changes;
- repeated capabilities reusing evidence without duplicate adapter execution;
- schema/production-decoder drift, unknown fields, wrong casing, invalid
  locations, oversized text, and collection errors that identify the exact field;
- protocol, capability, framework, format-write, and installed-integration
  failures remaining separately attributable;
- Unix and native Windows behavior for each supported execution type.

Use temporary repositories and fake adapters for ordinary coverage. Credentialed,
destructive, live-provider, and supplemental probes remain behind their explicit
gates.

## Completion criteria

- Every pack explicitly declares `file-scoped`, `static`, or `evaluated`.
- Project discovery is required only when a declared capability needs it.
- Evaluated discovery and host toolchains cannot run without exact authority.
- Large repositories receive a complete logical inventory within explicit
  resource limits.
- Capabilities reuse matching validated discovery facts while retaining their
  own required coverage, diagnostic scope, and write authority.
- Ecosystem-specific structure stays in the owning pack instead of becoming a
  lossy universal project model.
- A pack author can implement and locally validate the contract without reading
  core source, using production-checked schemas, examples, and reference vectors.
- Validation errors identify the exact field and constraint without leaking
  unbounded source, request, or environment content.
- Data safety, supplemental isolation, installation integrity, evidence, and
  gates remain engine-owned and cannot be weakened by a pack.
- The current four-language migration proves all required discovery and execution
  models through shared conformance fixtures before its protocol cutover.
- Cargo, Gradle, Bundler, and CMake prove any later claim of support for their
  additional discovery models.
- Permanent documentation, schemas, authoring kit, CLI help, fixtures, and
  platform checks agree at the public cutover.
