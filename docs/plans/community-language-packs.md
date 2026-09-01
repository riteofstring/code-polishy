# Community Language Packs Plan

## Outcome

Let developers add first-class Code Polishy support for another language or
environment without making Code Polishy responsible for that ecosystem.

A language pack is a self-contained local directory that connects language
tools to Code Polishy's existing quality, architecture, build, test, and supply
chain contracts. Code Polishy owns one small pack contract, installation
boundary, and verifier. Pack authors own their tools, platform support, and
releases.

The first release has no marketplace, registry, discovery service, remote
download, recommendation system, or automatic trust. Users obtain a pack by
their own means and install it from a local directory.

## First-class support

An installed pack is first-class when Code Polishy treats its evidence like
built-in evidence:

- source files and dependency manifests are detected;
- formatting works through the normal format command;
- lint, type, complexity, dead-code, and architecture findings participate in
  normal checks and gates;
- dependency policy, lock consistency, release age, and security checks run in
  the normal supply-chain profiles when the ecosystem requires them;
- findings use normal paths, locations, subjects, and messages;
- missing tools, unsupported platforms, corrupt packs, and incomplete evidence
  fail visibly;
- `doctor --strict` rejects missing capability coverage; and
- the project still declares its own modules, capabilities, builds, tests,
  external inputs, artifacts, and exact exceptions.

The pack supplies language understanding. It does not guess the architecture or
behavior requirements of an individual product.

## Keep the existing engine contract

Reuse the capabilities Code Polishy already requires:

- `format`;
- `lint`;
- `typecheck`;
- `complexity`;
- `dead-code`;
- `architecture`;
- `build` when the pack can supply a language-wide build provider; and
- `dependency-policy`, `lock-sync`, `release-age`, and `security` for dependency
  ecosystems.

At repository load, resolve the selected packs and compile their declared
providers into the same managed command model used by built-in conditional
policy modules. Existing coverage checks, change selection, gates, reports,
timeouts, environment isolation, and exception handling remain authoritative.

Do not add a language-specific Go interface or load pack code into the Code
Polishy process. Every adapter runs as a contained child process, so authors can
write it in Ruby, Rust, Java, shell, or any other executable environment.

## Pack directory contract

Use one required manifest and otherwise allow the language to choose its own
layout:

```text
code-polishy-rust/
  code-polishy-pack.json
  README.md
  bin/
  fixtures/
```

`code-polishy-pack.json` is the only machine-owned pack surface. Its first
schema should contain only:

- pack name and semantic version;
- pack protocol version;
- supported operating systems and architectures;
- built-in language identifiers or custom source patterns;
- dependency manifest patterns owned by the pack;
- contained adapter commands;
- capabilities provided by each command;
- profiles in which each capability runs;
- bounded command timeouts;
- explicitly named environment variables, if any; and
- fixture cases used by pack verification.

The manifest must not contain project module names, project paths, credentials,
ambient executable searches, remote installation instructions, or policy
exceptions. Commands and fixture paths must be relative, regular, contained
files. Reject symlinks and duplicate ownership.

`README.md` explains tool choices, supported language versions, platform limits,
installation provenance, known limitations, and contributor workflow. Markdown
is guidance; the manifest, fixtures, and verifier are enforcement.

## Adapter process protocol

Define one versioned JSON request and response protocol over standard input and
standard output. Code Polishy sends a typed request containing:

- protocol version;
- operation and capability;
- exact project root;
- selected repository-relative files;
- module ownership and allowed dependency direction when required;
- check or write mode for formatting;
- active profile; and
- the bounded output directory when an operation produces private evidence.

The adapter returns:

- protocol version;
- pass, findings, or operational-failure status;
- structured findings with capability, path, optional location, subject, and
  message; and
- bounded notes that contain no credentials.

Require exactly one JSON response, reject unknown protocol versions, escaping
paths, oversized output, malformed findings, fake success, and a success result
that omits required evidence. Keep adapter standard error in the managed command
log rather than parsing it as policy evidence.

The protocol is the cross-language API. Do not create language SDKs in the first
release.

## Local installation and selection

Add a local-source command:

```sh
code-polishy pack install --source ./code-polishy-rust
```

Installation must:

1. Read and validate the entire source directory before writing.
2. Reject unsupported platforms, unsafe paths, symlinks, missing files, and
   malformed manifests.
3. Materialize a private, dereferenced candidate directory.
4. Hash every installed file and create an installation receipt.
5. Atomically publish the immutable candidate under the user data directory.
6. Print the exact pack name, version, and digest for project selection.

The command does not fetch a URL, search for packs, run package-manager
lifecycle scripts, edit a project, or trust a publisher. Obtaining and reviewing
the source remains the user's responsibility.

Projects select an exact installed pack in `.code-polishy.json` using name,
version, and content digest. Keep the Code Polishy release lock unchanged; it
continues to own only the engine release. Pack selection is an explicit project
policy fact and must not depend on an absolute user path.

A missing selected pack is an unavailable external integration, never an empty
or successful result. The error should show the exact required identity and the
local install command shape without suggesting a registry.

## Storage

Keep packs outside repositories and outside versioned Code Polishy release
directories so engine upgrades neither remove nor silently replace them.

Use the existing installation data root with a separate `packs` subtree:

- Unix: `${XDG_DATA_HOME:-$HOME/.local/share}/code-polishy/packs/`;
- Windows: `%LOCALAPPDATA%\CodePolishy\packs\`; and
- an explicit caller-selected user data root when Code Polishy already supports
  one.

Store each pack by name, version, and digest. Never resolve an unversioned
`latest` directory. Add a read-only command that prints the effective pack root
for diagnosis without requiring callers to reproduce platform rules.

## Verification boundaries

Do not install shell startup hooks and do not verify packs when a terminal
session starts. Code Polishy should perform work only when its CLI is invoked.

- `pack install` performs full structural and integrity validation.
- `pack verify --source PATH` runs the manifest's conformance fixtures without
  installing the pack. Pack authors use this before publishing.
- Normal commands resolve the exact installed receipt and verify the digest of
  each adapter immediately before executing it.
- `doctor --strict`, checkpoint gates, and merge gates verify the complete
  selected pack tree against its installation receipt.
- Reinstalling or changing a selected version creates and validates a new
  immutable directory. It never edits the old installation in place.

This avoids repeated session work while ensuring that changed adapter bytes are
never executed silently.

## Contributor workflow

Ship a versioned CLI document, retrievable through:

```sh
code-polishy docs read adding-a-language
```

The document gives an AI or human contributor this workflow:

1. Inventory the language, dependency ecosystem, native toolchain, platforms,
   and existing project conventions.
2. Map real tools to every required capability and identify honest unsupported
   cases.
3. Pin or bundle every pack-owned dependency with source and license
   provenance. Declare target-owned tools as explicit compatibility inputs.
4. Implement the JSON adapter boundary without auto-installing during checks.
5. Add passing and deliberately failing fixtures for every provided
   capability.
6. Run `pack verify`, install the local candidate, select its exact digest in a
   disposable project, and run `doctor --strict` plus the normal gates.
7. Publish the directory as an independently owned Git repository if desired.

Provide one source-only example pack for contract development. Label it as a
fixture rather than official support for its example language. Code Polishy
does not list, endorse, update, or provide support for community repositories.

## Generic validation

Extend generic validation only where the existing engine cannot prove the pack
contract. Do not add conditionals for Ruby, Rust, Java, or another individual
language.

At minimum, validation must prove:

- every detected executable file has exactly one project module owner;
- every detected language has all required providers in the required profiles;
- pack and project providers cannot both claim authoritative ownership of the
  same capability and source;
- every dependency manifest has the required supply-chain providers;
- every provided command is contained, installed, compatible, and bounded;
- each declared capability has a fixture that passes and one that produces an
  expected finding or typed operational failure; and
- project tests and builds remain real, deterministic evidence rather than pack
  placeholders.

## Security and ownership

A pack is executable third-party input. Local installation means the user chose
the bytes; it does not mean Code Polishy endorses them.

- Never auto-discover, auto-download, auto-update, or auto-enable a pack.
- Never execute pack code during installation unless the user explicitly runs
  `pack verify`.
- Record hashes for every installed byte and verify before execution.
- Run adapters through the governed runner with existing resource, timeout,
  output, and environment limits.
- Pass only explicitly declared environment variables.
- Keep credentials and registry authentication outside manifests and receipts.
- Treat pack updates like dependency updates: review the candidate, install it
  separately, then change the selected exact digest.
- Keep pack failures distinct from Code Polishy engine failures in reports.

## Verification

### Manifest and installation tests

- valid installation, idempotent reinstall, and atomic publication;
- duplicate identity with different bytes;
- traversal, absolute paths, symlinks, special files, missing commands, and
  oversized files;
- unsupported platform and architecture;
- deterministic tree digest and receipt validation;
- user data resolution on Unix and Windows; and
- missing, corrupt, and partially installed packs.

### Protocol tests

- every operation and capability;
- malformed, multiple, oversized, and unsupported-version responses;
- contained and escaping finding paths;
- pass, findings, and typed operational failures;
- check and write formatting modes;
- bounded notes and standard-error capture;
- timeout, cancellation, resource limits, and environment filtering; and
- adapter tampering between receipt resolution and execution.

### Repository integration tests

- a disposable project using only one community-style pack;
- a mixed built-in and packed-language project;
- custom source extensions;
- module dependency-direction findings from a pack;
- dependency coverage in offline and online profiles;
- exact impact selection and normal gate reporting;
- a missing locally installed pack;
- a project selecting the wrong version or digest;
- `doctor --strict` and merge-gate full-tree verification; and
- proof that opening a new shell or terminal performs no pack work.

## Explicit non-goals

- a marketplace, registry, catalog, or search command;
- Git, HTTP, or package-registry downloads;
- publisher accounts, signatures, ratings, recommendations, or endorsements;
- automatic trust, installation, updates, or language selection;
- official ownership of community language tools;
- language-specific SDKs or Go interfaces;
- background services, daemons, or terminal startup hooks;
- silently adding dependencies to a target project; and
- replacing project-specific architecture, tests, builds, or exceptions.

## Delivery sequence

1. Commit the public pack manifest, process protocol, storage, and trust
   contracts in permanent documentation and schema fixtures.
2. Add typed manifest loading, validation, pack identity, tree hashing, receipt,
   and platform-specific user data resolution.
3. Add atomic local-directory installation and read-only pack inspection.
4. Add the adapter process protocol through the governed runner and compile pack
   providers into the existing managed command model.
5. Add exact project pack selection and generic coverage validation.
6. Add `pack verify` with passing and failing conformance fixtures.
7. Integrate complete pack verification into `doctor --strict`, checkpoint
   gates, and merge gates while keeping normal execution limited to the selected
   adapter digest.
8. Add the versioned `adding-a-language` workflow and the source-only example
   pack fixture.
9. Run installed-release tests on Unix and Windows, changed tests, the final
   merge gate, and hosted CI.
10. Remove this temporary plan only after permanent documentation owns every
    public contract.
