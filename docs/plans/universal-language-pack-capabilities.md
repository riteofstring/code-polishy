# Universal Language-Pack Capabilities

Status: proposed

## Outcome

Give every language pack the same project-aware policy boundary that built-in
Python support uses, without copying Python tools or ecosystem rules into the
pack contract.

After this work, a pack can discover its language's projects once and reuse
that validated inventory for formatting, quality, architecture, build, and
supply-chain checks. Code Polishy continues to own selection, module direction,
data safety, execution profiles, evidence validation, installation integrity,
and gates.

## Product boundary

| Code Polishy owns | A language pack owns |
| --- | --- |
| Exact pack selection and receipt verification | Recognizing its project layouts |
| Governed repository file inventory | Interpreting language manifests and locks |
| Project and module containment validation | Running language-specific tools |
| Module dependency direction | Resolving language import semantics |
| `scope.data` parse-only and no-rewrite rules | Producing structured findings and evidence |
| Ordinary and supplemental execution boundaries | Declaring supported standard capabilities |
| Timeouts, sealed environments, logs, and reports | Shipping conformance fixtures |
| Checkpoint and merge gates | Publishing through an independently owned channel |

Language packs remain language integrations. GitLab CI and similar repository
services remain built-in policy. A broader integration-pack or plugin model is
a separate product decision.

## Current state

Protocol version 1 gives an adapter:

- the repository root;
- selected files;
- project modules and their allowed dependency direction;
- the operation, mode, profile, and capability.

A manifest declares source patterns, dependency-manifest patterns, commands,
profiles, and capabilities. An adapter returns pass evidence, findings, or an
operational failure.

This already gives packs standard `format`, `lint`, `typecheck`, `complexity`,
`dead-code`, `architecture`, `build`, `dependency-policy`, `lock-sync`,
`release-age`, and `security` entry points. The missing shared concept is a
validated project inventory. Each adapter currently has to rediscover project
boundaries privately, and Code Polishy cannot prove that the same boundaries
were used by every capability.

## Contract decisions

### 1. Make protocol version 2 an atomic cutover

Bump both `manifestVersion` and `protocolVersion` to 2. Do not add a dual v1/v2
runtime or translate v1 responses. Repository release locks prevent an engine
upgrade from silently changing existing projects, and pack selections already
name an exact version and digest.

The first Code Polishy release containing this contract accepts v2 packs only.
Existing v1 packs must publish a reviewed v2 release before a project upgrades
to that engine release.

### 2. Add one discovery command per language

Each manifest language entry references exactly one declared discovery command.
Discovery is a protocol operation rather than a quality capability. It runs
once for a resolved pack against the complete governed repository inventory,
before change-specific command selection.

The discovery request contains:

- protocol version and `operation: "discover"`;
- the exact repository root;
- the language identifier;
- all governed regular files matching that language's source and manifest
  patterns;
- declared modules and dependency direction;
- generated and parse-only data classifications relevant to those files.

Discovery receives no format mode, output directory, credentials, ambient tool
search, or supplemental execution trigger.

### 3. Return a small, ecosystem-neutral project model

A successful discovery response returns non-empty evidence and zero or more
projects. Each project contains:

```json
{
  "language": "rust",
  "root": "services/search",
  "sourceRoots": ["services/search/src"],
  "manifests": ["services/search/Cargo.toml"],
  "locks": ["services/search/Cargo.lock"]
}
```

Code Polishy derives project identity from pack identity, language, and root.
The adapter does not provide an opaque project ID. The initial contract does
not include arbitrary metadata, tool arguments, environment values, or remote
coordinates.

Project roots may be `.` or exact contained repository-relative directories.
Source roots, manifests, and locks must be canonical contained paths present in
the governed inventory. Arrays are sorted and unique before use.

### 4. Validate discovery fail-closed

Reject the complete discovery response when any project:

- escapes the repository or names a link, special file, or missing path;
- claims a file outside the manifest's declared language patterns;
- overlaps another project from the same language ambiguously;
- claims a manifest or lock already owned by another selected pack;
- duplicates a project root;
- exceeds count, depth, string, or response-size limits;
- returns unknown fields, extra JSON values, or evidence-free success.

Every selected source file and dependency manifest must resolve to exactly one
project or to an explicitly allowed root-level standalone project. Missing and
ambiguous ownership are normal policy findings. Operational failures remain
distinct from policy findings.

Validate the entire response before publishing any project into repository or
engine state.

### 5. Reuse the inventory in every pack command

Store the validated inventory once in engine-owned state. Every later adapter
request receives only the projects intersecting its selected source or
dependency files.

All capabilities for one pack must use this inventory:

- quality and build commands receive project roots and source roots;
- architecture commands receive projects plus Code Polishy modules and allowed
  module dependencies;
- dependency commands receive the exact manifests and locks belonging to each
  project;
- findings remain restricted to selected governed files;
- format commands continue to exclude `scope.data` paths and use explicit check
  or write mode.

Adapters still produce findings. Protocol v2 does not move language-specific
import parsing, type systems, lockfile interpretation, or compiler behavior into
Code Polishy.

### 6. Keep engine guarantees outside packs

The following v0.21 behavior already applies universally and remains outside
the pack protocol:

- JSON, JSONC, YAML, and YML under `scope.data` are parsed without byte
  rewriting;
- generated executable source remains governed;
- supplemental mutation and risk suites run only through an explicit trigger
  or the stable-release-candidate workflow;
- language packs cannot declare test suites or a `supplemental` command
  profile;
- install and publish remain content-addressed, atomic, read-only, and verified
  before and after adapter execution;
- checkpoint and merge gates keep their normal evidence and report rules.

### 7. Keep ecosystem policy in the adapter

The standard capabilities define outcomes, not specific tools:

- a Python provider may use Ruff, ty, Vulture, and uv;
- a Rust provider may use rustfmt, Clippy, rustc, Cargo metadata, and Cargo.lock;
- another language may use different tools or honestly omit an unsupported
  optional capability;
- any declared dependency manifest still requires complete
  `dependency-policy`, `lock-sync`, `release-age`, and `security` coverage.

Exact Git-source validation, immutable dependency identity, import-graph
construction, and project-layout rules are implemented by the owning adapter
and reported through the shared capabilities.

## Implementation sequence

### Phase 1: Define the permanent v2 contract

1. Update `docs/adding-a-language.md` with discovery, project ownership, and the
   v2 request and response shapes.
2. Change `schema/code-polishy-pack.schema.json` to manifest version 2 and add a
   required discovery command reference to each language.
3. Update the project schema only if selected pack configuration changes. Keep
   the existing exact name, version, and digest selection if it does not.
4. Document the atomic v1-to-v2 release boundary in the changelog and release
   checklist.

### Phase 2: Add validated project types

1. Add protocol DTOs for discovery requests, discovery responses, and project
   declarations under `internal/pack`.
2. Add an engine-owned immutable project-inventory type. Keep JSON DTOs at the
   adapter boundary rather than exposing them as repository domain types.
3. Add canonical path, inventory membership, uniqueness, overlap, ownership,
   and size validation.
4. Give every failure a typed operational or policy category and stable finding
   location.

### Phase 3: Run discovery once

1. Resolve and verify every selected pack before discovery.
2. Build one complete governed input set for each declared language.
3. Execute each discovery command through the existing structured runner and
   sealed environment.
4. Validate all pack discovery responses before adding any inventory to engine
   state.
5. Verify the installed pack receipt again after discovery.
6. Keep the inventory in memory for the lifetime of the engine invocation. Do
   not add a persistent cache in this change.

### Phase 4: Feed projects into capability requests

1. Add relevant validated projects to check and format requests.
2. Select source files, manifests, and locks through their project ownership.
3. Preserve existing module and path selectors.
4. Make architecture providers receive both project boundaries and the module
   graph.
5. Make supply-chain provider coverage operate per discovered project rather
   than only per matching manifest path.
6. Reject conflicting built-in, repository-owned, or pack-owned providers at
   the same authoritative boundary used today.

### Phase 5: Strengthen verification and diagnostics

1. Extend `pack verify` so every fixture runs discovery before its declared
   capability.
2. Require fixtures for flat, nested, and deliberately malformed project
   layouts when a pack declares discovery.
3. Show discovered project counts and roots in bounded diagnostic output.
4. Make `doctor --strict` verify discovery coverage, provider completeness,
   platform support, receipts, and project ownership.
5. Keep normal check and gate output concise; detailed discovery evidence stays
   in managed logs and reports.

### Phase 6: Cut over examples and release surfaces

1. Convert `tools/fixtures/language-pack` to manifest and protocol version 2.
2. Update CLI help, installed-release checks, Windows CI, and documentation
   catalog expectations.
3. Verify one disposable nested-project pack through install, selection,
   `doctor --strict`, format check, changed-scope testing, and the normal gate.
4. Remove every v1-only type, fixture, schema branch, and protocol message in
   the same public cutover.

## Verification

Add observable boundary coverage for:

- strict v2 manifest and protocol parsing;
- deterministic discovery independent of filesystem traversal order;
- root, nested, and multiple-project layouts;
- missing, duplicate, overlapping, escaping, linked, and ungoverned paths;
- source, manifest, and lock ownership conflicts across packs;
- partial discovery and evidence-free success rejection;
- bounded project, file, string, and response counts;
- reuse of one validated inventory by quality, architecture, build, and
  supply-chain commands;
- architecture findings using the target repository's module direction;
- complete supply-chain provider requirements per discovered project;
- format check and write modes never selecting `scope.data`;
- generated executable source remaining selected;
- pack tampering before, during, and after discovery;
- fixture verification on Unix and installed-release behavior on native
  Windows;
- rejection of supplemental profiles and test-suite declarations in packs.

Use temporary repositories and fake adapters. Network, credentialed, live
provider, and destructive probes remain outside ordinary tests.

## Completion criteria

- One validated project inventory is the authoritative boundary for every
  command from a selected pack.
- A language pack can support nested projects without rediscovering different
  roots for each capability.
- Module architecture and dependency-provider coverage operate on those exact
  projects.
- Core data, supplemental, installation, evidence, and gate guarantees cannot
  be weakened by a manifest or adapter.
- GitLab CI remains correctly described as repository policy rather than a
  language pack.
- Protocol v1 has no remaining runtime path in the release that introduces v2.
- Permanent documentation, schema, CLI help, fixtures, installed-release tests,
  and Windows checks all describe the same v2 contract.
