# Language-Pack Discovery and Universal Capabilities

Status: proposed

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

| Code Polishy owns                                     | A language pack owns                                   |
| ----------------------------------------------------- | ------------------------------------------------------ |
| Exact pack selection and receipt verification         | Recognizing its ecosystem layouts                      |
| Complete governed file inventory and active selection | Interpreting manifests, locks, and build metadata      |
| File classifications and repository modules           | Resolving packages, targets, imports, and dependencies |
| Module dependency direction                           | Running ecosystem-specific tools                       |
| `scope.data` parse-only and no-rewrite rules          | Producing structured findings and evidence             |
| Ordinary and supplemental execution boundaries        | Declaring supported capabilities and discovery mode    |
| Timeouts, isolation, artifacts, reports, and receipts | Declaring any host toolchain requirements              |
| Checkpoint and merge gates                            | Shipping conformance fixtures                          |

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

Every adapter request receives two bounded file sets:

- `inventory`: all governed, pack-relevant files needed to understand the
  repository consistently;
- `selection`: the files in scope for the current command or change.

Each inventory entry includes engine-owned classifications such as executable
language source, module, generated output, parse-only data, control file, and
dependency input. Paths are canonical repository-relative paths. The adapter
cannot expand either set by walking the repository.

This lets a changed-file command understand a workspace-level manifest without
allowing findings or rewrites outside the active selection. Format commands
continue to exclude `scope.data` from writes.

### 4. Keep discovery results ecosystem-specific and bounded

Discovery returns structured evidence and an opaque, canonical analysis-scope
document owned by the pack's protocol schema. Code Polishy validates generic
properties only:

- strict JSON shape, protocol version, size, count, and depth limits;
- canonical paths contained in the supplied inventory;
- deterministic ordering and an `analysisScopeDigest`;
- no unknown fields, extra JSON values, or evidence-free success;
- no capability result referring to a different scope digest.

Code Polishy does not require a universal `{root, sourceRoots, manifests,
locks}` project object, exactly-one file ownership, or non-overlapping projects.
A Cargo adapter may describe workspaces and packages; a Gradle adapter may
describe builds and projects; a CMake adapter may describe configured targets.

The same validated scope and digest are reused by every capability in one
engine invocation. Packs may return a concise display summary for diagnostics,
but the engine does not infer policy from display text.

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

The [v0.21.6 and v0.22 roadmap](v0.21.6-and-v0.22.md) owns the corresponding
artifact, receipt-reuse, final-gate, and suite-deduplication design. This pack
protocol supplies exact inputs to that engine policy; it does not create a
second scheduler.

## Scale requirements

The current 10,000-file request limit and 1 MiB response limit are too small for
large real repositories. Protocol design must solve scale before a public v2 is
frozen.

The chosen transport must:

- preserve a complete logical inventory without one unbounded JSON message;
- stream or page deterministically with a final content digest;
- put explicit byte, entry, depth, and time budgets on both sides;
- avoid repeated full-inventory transfer for every capability in one run;
- keep adapters unable to enumerate files outside the governed inventory;
- provide useful typed failures when a repository exceeds a limit.

This plan does not prescribe streaming, a temporary read-only inventory file,
or another transport until prototypes measure the tradeoffs.

## Implementation sequence

### Phase 0: Prove the boundary before freezing it

Build disposable adapters for four deliberately different systems:

1. Cargo for a mostly static workspace model;
2. Gradle for evaluated, conditional multi-project configuration;
3. Bundler for executable Ruby manifests and optional lockfiles;
4. CMake for configuration-dependent targets and compiler context.

Each prototype must exercise a small repository and a large or deeply nested
fixture. Record the minimum inventory, toolchain, authority, output, and scale
requirements. Use the results to decide the transport and final v2 schema.

Do not remove protocol v1 or require migration during this phase.

### Phase 1: Define the manifest contract

1. Add required discovery mode and command execution type declarations.
2. Add host-toolchain identity, platform, environment, network, and writable
   path fields.
3. Define which standard capabilities are legal for each discovery mode.
4. Define exact stored approval for evaluated discovery and host toolchains.
5. Update `docs/adding-a-language.md` with ecosystem-provider guidance and
   examples for all three modes.

### Phase 2: Add governed inventory transport

1. Build the complete pack-relevant inventory once per engine invocation.
2. Classify entries at the existing authoritative file-policy boundary.
3. Add the active selection separately for each operation.
4. Implement the bounded transport selected from Phase 0.
5. Validate the complete logical input before adapter execution.

### Phase 3: Add discovery and scope consistency

1. Add strict discovery requests, responses, typed failures, and evidence.
2. Validate returned paths against inventory and enforce resource limits.
3. Calculate or verify the canonical `analysisScopeDigest`.
4. Reuse one validated scope for every capability in the invocation.
5. Reject capability evidence tied to another digest.
6. Keep the scope in memory; do not add persistent caching in this change.

### Phase 4: Enforce evaluated and host-toolchain authority

1. Resolve executables without ambient `PATH` lookup.
2. Verify tool versions and platforms before running the adapter.
3. Prompt interactively for exact approval when required.
4. Fail clearly in non-interactive execution when approval is absent.
5. Apply declared environment, network, and writable-path constraints.
6. Record pack and toolchain identity in managed evidence.

### Phase 5: Integrate capabilities and diagnostics

1. Feed inventory, selection, and the validated scope digest into capability
   requests.
2. Keep findings and format writes within their existing allowed scopes.
3. Make architecture evidence include Code Polishy's module graph and the
   pack's ecosystem analysis scope.
4. Make dependency capabilities identify the manifests, locks, or resolved
   graph they actually evaluated without imposing exactly-one ownership.
5. Extend `pack verify` with mode-specific conformance fixtures.
6. Make `doctor --strict` report mode, approval, toolchain, platform, provider
   conflicts, capability coverage, and bounded discovery summaries.

### Phase 6: Freeze and cut over protocol v2

Only after the four prototypes pass the same conformance boundary:

1. freeze manifest and protocol version 2;
2. convert the bundled fixture and permanent documentation;
3. publish reviewed v2 releases of supported packs;
4. verify install, selection, doctor, format, changed-scope checks, gates, and
   native Windows behavior where supported;
5. make the engine accept v2 packs in one coherent public release;
6. remove v1 only when every release surface and supported pack is ready for
   the atomic cutover.

No dual-protocol translation layer is planned. Delaying the cutover is cheaper
than maintaining a permanent compatibility path for an unproven contract.

## Verification

Add observable boundary coverage for:

- all three discovery modes and their allowed capabilities;
- missing, stale, and changed evaluated-discovery approvals;
- self-contained and host-toolchain executable resolution;
- rejected tool versions, platforms, ambient lookup, environment, and network;
- deterministic inventory transport above the current file and response caps;
- inventory versus selection behavior for changed-file operations;
- canonical paths, links, special files, missing files, and escaping paths;
- deterministic analysis scopes and cross-capability digest consistency;
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
- Every capability in one run uses the same validated analysis scope.
- Ecosystem-specific structure stays in the owning pack instead of becoming a
  lossy universal project model.
- Data safety, supplemental isolation, installation integrity, evidence, and
  gates remain engine-owned and cannot be weakened by a pack.
- Cargo, Gradle, Bundler, and CMake prove the contract before v2 is frozen.
- Permanent documentation, schema, CLI help, fixtures, and platform checks agree
  at the public cutover.
