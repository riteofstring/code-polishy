# Installable First-Party Language Packs

Status: proposed

## Outcome

Move Code Polishy's built-in language support into official packs that users can
install, select, update, and remove independently from the core engine.

The engine becomes language-neutral. It continues to own repository safety,
file selection, modules, execution limits, evidence, reports, and gates. Official
packs own the tools and rules for Go, Python, JavaScript and TypeScript, and
shell.

This is a future packaging and ownership change. It does not move the current
built-in implementations yet.

## User experience

Installing a pack and selecting it are separate actions:

- **Installed** means an exact pack release exists on the current machine.
- **Selected** means a repository pins that pack's exact name, version, and
  digest in `.code-polishy.json`.

A normal setup flow is:

1. Code Polishy scans governed filenames and extensions using trusted catalog
   metadata. It does not run pack code.
2. It recommends matching official packs.
3. The user approves the exact packs and downloads.
4. Code Polishy verifies and installs them atomically.
5. The repository records the exact selections.
6. `doctor --strict` confirms that every selected pack is present and usable.

No pack is downloaded, updated, selected, or granted evaluated-discovery
authority silently.

Removing a pack deletes only that exact installed pack release. A repository
that still selects it fails with a clear installation command. Code Polishy does
not silently reduce checks or substitute another version.

## Product boundary

| Core engine owns                                             | First-party language pack owns                 |
| ------------------------------------------------------------ | ---------------------------------------------- |
| Repository and governed-file boundaries                      | Language and ecosystem file patterns           |
| Active file selection and module direction                   | Formatting and linting tools                   |
| Data, generated, control, and external-input classifications | Type, complexity, and dead-code analysis       |
| Pack verification, installation, and execution isolation     | Import and package graph interpretation        |
| Capability names and structured evidence validation          | Build and language dependency checks           |
| Supplemental-suite authority                                 | Language-specific conformance fixtures         |
| Reports, artifacts, suite receipts, checkpoints, and gates   | Supported platforms and toolchain declarations |
| Markdown and repository-service policy                       | Pack release notes and support policy          |

The core must not import language-specific parsers, invoke language tools, or
contain language-specific policy thresholds after the final migration. Generic
path handling is allowed; knowing that `pyproject.toml` describes Python or that
`go.mod` describes Go belongs to a selected pack or trusted catalog metadata.

Repository-wide rules stay in core when their meaning is independent of a
language. Examples include parse-only data safety, artifact integrity,
supplemental execution rules, GitLab policy, workflow policy, test selection,
and evidence validation.

The engine also owns verification scheduling and reuse. A pack cannot trigger a
supplemental suite, invalidate unrelated receipts, or write test output outside
the engine-provided execution directory. Pack installation, selection,
toolchain, or capability changes invalidate only receipts whose exact identity
depends on the changed input; an incomplete identity runs again.

The [v0.22 plan](v0.22.md) defines managed artifacts,
receipt reuse, final-gate ownership, and suite deduplication. Pack work must use
those engine boundaries rather than reintroducing duplicate full runs.

## Official pack set

The initial first-party set preserves today's supported behavior:

| Pack                    | Initial responsibility                                                                                                         |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `go`                    | Go formatting, vetting, static analysis, architecture, build, modules, and dependency evidence                                 |
| `python`                | Static project discovery, Ruff, Vulture, ty, architecture, uv, and Git dependency evidence                                     |
| `javascript-typescript` | JavaScript and TypeScript formatting, linting, type analysis, architecture, framework activation, and Node dependency evidence |
| `shell`                 | Shell syntax, formatting policy if supported, ShellCheck, and shell portability evidence                                       |

JavaScript and TypeScript begin as one pack because they share the current
runtime, bundle, resolution rules, and common project ecosystems. Splitting them
later requires evidence that separate ownership improves the product without
duplicating tools or producing conflicting project graphs.

Each pack uses the discovery contract from the language-pack discovery plan.
Python initially declares `static`. Other modes are chosen from their real
behavior and proven before migration.

## First-party trust and distribution

Official packs use the same runtime protocol and safety boundary as any other
pack. “First-party” adds a distribution and support promise; it does not grant
broader runtime authority.

Each Code Polishy release publishes a signed or otherwise release-authenticated
catalog containing:

- pack name, version, digest, size, and artifact URL;
- compatible engine and protocol versions;
- supported operating systems and architectures;
- discovery and toolchain execution modes;
- complete file and dependency inventory references;
- source and license provenance.

The engine trusts only catalog data bound to its installed release trust root.
Downloaded bytes must match the catalog digest before installation. Installation
uses the existing immutable, content-addressed pack store and executes no pack
code.

Manual local installation remains available for packs obtained through another
channel. It does not acquire first-party status merely by using an official
name.

## Version and repository contract

Repositories continue to pin every selected pack by exact name, semantic
version, and digest. They do not select `latest`, a channel, or an engine-relative
floating version.

The engine and pack protocols declare explicit compatibility. A newer compatible
pack is still an intentional repository change. `pack update` prepares an exact
candidate and shows changed capabilities, discovery authority, toolchains, and
provenance before changing project policy.

An engine upgrade does not rewrite pack selections. If the new engine cannot run
a selected pack, the upgrade fails before policy execution with the exact
compatible choices. A pack update does not upgrade the engine.

## Commands

Provide a small explicit lifecycle:

```text
code-polishy pack catalog
code-polishy pack install --official NAME@VERSION
code-polishy pack update NAME --to VERSION
code-polishy pack remove NAME@VERSION
code-polishy pack verify --source PATH
code-polishy pack list
```

Repository adoption uses the normal adoption command rather than making global
installation imply repository selection. Commands that change
`.code-polishy.json` show the exact before-and-after selection and follow normal
repository verification.

`pack list` distinguishes installed, selected in the current repository,
missing, incompatible, and corrupt releases. `doctor --strict` reports the same
states without repairing them automatically.

## Behavioral guarantees

Moving a language into a pack must preserve or strengthen its observable
coverage:

- the same governed source remains selected;
- the same required capabilities remain active;
- policy-owned settings and thresholds cannot be weakened by target config;
- malformed or incomplete analysis remains a finding or operational failure;
- no missing pack, tool, graph, or diagnostic becomes a clean result;
- findings retain stable checks, paths, subjects, and exception behavior;
- local and CI runs resolve the same exact pack and tools;
- removal never leaves hidden language-specific fallbacks in core.

A migrated language has one implementation owner. Its former core parser,
runner, tool installer, rules, configuration, tests, and documentation are
removed in the same public cutover unless the final core contract still needs
them generically.

## Migration strategy

Migrate one complete language boundary at a time. Each language cutover is
atomic even though the full program spans several releases.

### Phase 0: Inventory the current ownership

1. Map every Go, Python, JavaScript and TypeScript, and shell behavior to its
   parser, tool, configuration, capability, test, documentation, and release
   asset.
2. Classify each behavior as core policy, pack behavior, or shared protocol.
3. Identify cross-language coupling and give each shared concept one owner.
4. Record current observable fixtures that every migrated pack must preserve.
5. Resolve the discovery plan and host-toolchain contract before extracting a
   project-aware language.

### Phase 1: Build official distribution

1. Define the authenticated catalog schema and release trust root.
2. Publish platform-specific pack artifacts with exact digests and inventories.
3. Add official install, update, remove, catalog, and list commands.
4. Keep installation transactional and code-free.
5. Add offline and unavailable-catalog diagnostics without silent fallback.

### Phase 2: Prove the model with shell

Use shell as the first extraction because its project-discovery needs are small.

1. Move shell patterns, tool declarations, checks, and fixtures into an official
   `shell` pack.
2. Prove install, selection, execution, update, removal, and missing-pack
   behavior on every supported platform.
3. Remove the built-in shell implementation and release assets in the same
   cutover.
4. Confirm core gates consume only standard pack capabilities and evidence.

### Phase 3: Prove project-aware extraction with Python

1. Move the existing validated Python project inventory behind the static
   discovery contract without changing its behavior.
2. Move Ruff, Vulture, ty, carried CPython, uv and Git policy, architecture, and
   fixtures into the official `python` pack.
3. Preserve project-local `.venv` as explicit dependency input rather than an
   ambient runtime.
4. Verify nested projects, generated sources, dynamic references, module
   direction, malformed evidence, and supply-chain coverage.
5. Remove every Python-specific core path in the atomic language cutover.

### Phase 4: Extract JavaScript and TypeScript

1. Move the sealed JavaScript runtime and tool bundle into the
   `javascript-typescript` pack.
2. Move project resolution, formatting, linting, type analysis, architecture,
   dependency evidence, and framework-specific language checks together.
3. Separate repository-wide workflow or artifact rules that remain core.
4. Preserve target configuration limits and lifecycle-script isolation.
5. Remove the built-in JavaScript and TypeScript implementation and bundle.

### Phase 5: Extract Go

1. Define the exact host Go toolchain contract or ship self-contained tools.
2. Move module discovery, formatting, vetting, static analysis, architecture,
   builds, dependency checks, and fixtures into the `go` pack.
3. Preserve nested-module behavior, workspace rules, build tags, environment
   limits, and vulnerability evidence.
4. Remove built-in Go language behavior while retaining Go only as an engine
   implementation detail where needed.

### Phase 6: Make the core language-neutral

1. Search schemas, configuration, commands, reports, release scripts, docs, and
   tests for language-specific ownership left in core.
2. Replace only genuinely shared behavior with protocol-level types.
3. Remove obsolete language tools and dependencies from the engine release.
4. Verify a core-only installation and installations with every supported pack
   combination.
5. Update the README and website only when the installable model becomes the
   released user experience.

## Existing repository transition

Before each language cutover, provide a deliberate migration command that:

1. detects current use of that built-in language;
2. shows the exact official pack release and new repository selection;
3. installs the pack only after approval;
4. updates project policy atomically;
5. verifies equivalent capability coverage;
6. leaves the repository unchanged if any step fails.

The engine release that removes built-in support does not keep a hidden legacy
path. Repositories that have not migrated receive a clear blocking instruction.
This keeps the public cutover coherent and avoids permanent dual ownership.

## Verification

Add observable boundary coverage for:

- authenticated catalog metadata and rejected forged, stale, or mismatched
  artifacts;
- exact install, update, selection, removal, and reinstall behavior;
- installed versus selected versus missing state;
- offline setup and CI bootstrap from exact pins;
- unsupported engines, protocols, operating systems, and architectures;
- transactional multi-pack setup with no partial policy update;
- no implicit network, toolchain, discovery, or execution authority;
- language detection that recommends without executing or auto-selecting packs;
- identical findings and capability coverage before and after each migration;
- exact pack and toolchain changes invalidating affected receipts without
  invalidating unrelated suites;
- pack commands unable to schedule supplemental work or escape the managed
  artifact directory;
- missing, corrupt, or incomplete packs failing visibly;
- multiple selected packs with disjoint ownership and rejected conflicts;
- core-only operation for repositories containing no selected pack languages;
- no language-specific implementation residue after each atomic cutover;
- native Windows, macOS, and Linux behavior for supported packs.

Use temporary repositories and local fixture catalogs for ordinary tests.
Credentialed publishing, destructive probes, and live distribution checks stay
behind named external approval gates.

## Completion criteria

- Go, Python, JavaScript and TypeScript, and shell support are official,
  independently installable packs.
- Repositories pin exact pack releases and never depend on machine-global
  defaults.
- Users can remove unused language tooling without weakening selected
  repository policy silently.
- Every migrated language preserves its current observable guarantees.
- The core contains no language-specific policy, parser, runner, tool bundle, or
  fallback.
- First-party status is authenticated distribution metadata, not broader runtime
  trust.
- Setup, local runs, CI, doctor, checkpoints, and merge gates agree on pack
  identity and availability.
- Permanent documentation and product surfaces describe only the released
  ownership model.
