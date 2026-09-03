# Architecture Policy

Architecture policy exists to remove bug classes, not to reward a particular
directory diagram. The target is a codebase in which important invalid states,
ownership conflicts, partial mutations, and dependency inversions are either
unrepresentable or rejected at one narrow boundary.

## Required outcomes

Every code-bearing repository must be able to answer:

1. Who owns each important concept and contract?
2. Where does untrusted or weakly typed data become valid domain data?
3. Which module owns each state transition and multi-resource mutation?
4. Which dependencies are internal, locally substitutable, remotely owned, or
   truly external?
5. What stable interface hides the coupled implementation?
6. Which tests prove behavior through that interface?
7. Which executable rule prevents a new dependency from bypassing the design?

The answers should fit in a short repository architecture document and the
`.code-polishy.json` boundary contract. If understanding one concept requires
bouncing through many forwarding helpers and files, the module is too shallow.

## Make these failures impossible

| Error class                          | Architectural mechanism                                           | Executable evidence                                |
| ------------------------------------ | ----------------------------------------------------------------- | -------------------------------------------------- |
| Two owners disagree                  | One canonical owner; generated projections                        | Import boundary plus generator/contract tests      |
| Invalid input leaks inward           | Parse and validate once at ingress; expose a refined type         | Boundary tests with malformed and valid fixtures   |
| A new state is forgotten             | Discriminated union or enum with exhaustive handling              | Compiler/typecheck failure on an omitted case      |
| Illegal state combination            | State-specific types or a state machine, not independent booleans | Transition tests and inaccessible constructors     |
| Half a mutation is saved             | One transaction or atomic repository operation                    | Fault-injection test at the mutation boundary      |
| Domain code performs I/O             | Dependency direction toward domain; injected ports                | Import allowlist and in-memory adapter tests       |
| Cross-language contracts drift       | Generate consumers from the owner                                 | Contract fixture through production decoders       |
| Third-party details spread           | One adapter translates provider data and failures                 | Adapter contract tests; no provider imports inward |
| Implementation refactor breaks tests | Test observable outcomes at the deep interface                    | Old shallow tests replaced by boundary tests       |
| Silent fallback corrupts meaning     | Typed result/failure path; fail specifically                      | Negative-path tests assert explicit failure        |
| Sibling checkout changes a build     | Versioned input bundle or declared package dependency             | Empty-workspace consumer fixture                   |
| Concurrent writers lose data         | Serialized command boundary, compare-and-swap, or transaction     | Deterministic concurrency test                     |

Not every row applies to every repository. Every material error class that does
apply needs a mechanism and evidence; prose alone is not enforcement.

## One owner per concept

An owner defines validation, identity, state transitions, derived values, and
contract shape for a concept. Consumers may project or render it, but they do
not independently reconstruct its meaning.

- Keep shared constants at the owning domain boundary, not in a global
  constants drawer.
- Search for an authoritative type, helper, registry, or generator before
  adding a mirror.
- Generate TypeScript, Python, schemas, or clients from the owning contract when
  languages cross.
- Never hand-edit generated outputs.
- A consumer may render a canonical catalog index, for example, but must not
  silently become a second owner by rewriting the index.

When duplication is unavoidable because of latency, offline execution, or a
platform constraint, keep it minimal and record why generation or direct reuse
is impossible.

Separately owned repositories, directories, files, and services are external
inputs, not ambient filesystem facts. Declare their resolver precedence,
validate compatibility at the boundary, and expose warning/degraded behavior
through quick and full evidence. See
[Portability and External Inputs](portability.md).

## Validate at the boundary and refine types

External JSON, environment variables, database rows, provider responses, file
contents, and UI form values are not domain objects. Parse them at ingress and
return either a valid value or a specific failure.

Prefer:

- constructors that enforce invariants;
- opaque/branded value types for identifiers, normalized paths, money, and
  other constrained primitives;
- TypeScript discriminated unions, `satisfies` mappings, and `never`
  exhaustiveness assertions;
- Go types whose zero value is either valid or cannot escape construction;
- explicit state transition functions;
- result types or narrow errors instead of magic strings and broad catches.

Avoid independent booleans such as `isLoading`, `hasError`, `isComplete`, and
`isCancelled` when only a few combinations are legal. Model the states directly
and make transition ownership explicit.

## Deep modules over forwarding layers

A deep module has a small interface that hides substantial coupled machinery.
It is easier to understand, replace, and test than a cluster of shallow helpers
whose combined interface is as complicated as their implementation.

Deepen a cluster when:

- its files change together;
- callers must know call order or internal state;
- tests extract private functions or assert source text;
- errors occur in the seams between individually tested helpers;
- a concept requires navigation through many tiny files;
- helpers were extracted only so internal steps could be unit tested.

The new module should own the full concept, hide sequencing and representation,
and expose the few operations callers actually need. Replace shallow tests with
tests at that public boundary; do not layer a second suite on top indefinitely.

File length is only a diagnostic here. Splitting a 2,000-line module into 20
forwarding files can make the architecture worse.

## Dependency categories

Choose the testing and interface strategy from the dependency's real category.

### In-process

Pure computation or memory state. Merge tightly coupled pieces into one deep
module and test it directly.

### Local-substitutable

Filesystem, SQLite, an embedded database, or another dependency with a faithful
local implementation. Test the deep module with the real local substitute,
using temporary state.

### Remote but owned

An internal service across HTTP, gRPC, queues, or another deployment boundary.
Define a port owned by the calling domain. Use a production transport adapter
and an in-memory test adapter. Keep business logic on one side of the port,
instead of distributing it across transport handlers.

### Truly external

A third-party provider such as a payment, speech, email, or AI API. Mock only at
the adapter boundary. The adapter owns provider schemas, authentication,
timeouts, retry policy, and translation into domain results. Provider types do
not leak through the application.

Do not use a mock merely because the real internal module is inconvenient to
construct. That is often evidence of a missing deep boundary.

## Side effects and atomicity

Domain decisions should be pure where practical. Side effects occur after a
valid command is assembled and through one owned operation.

- Validate the complete mutation before the first write.
- Use a transaction for related records.
- For files, write to temporary paths, sync when durability matters, then
  replace atomically within the same filesystem.
- Treat Git operations and multi-repository publication as explicit workflows,
  not scattered subprocess calls.
- Make dry-run and execute modes share the same validation and plan. A dry run
  that prints missing inputs but exits zero is a broken contract.
- Tests use temporary directories and disposable repositories. They never
  mutate a developer's real history or application data.

## Delivery layers stay thin

HTTP handlers, IPC handlers, CLI parsing, UI event handlers, and queue consumers
translate delivery input into a domain command and translate the result back.
They do not own business rules, persistence choreography, or provider-specific
fallback heuristics.

UI state may have its own domain boundary. Components render state and emit
intent; they should not derive the same business status independently across
multiple screens. Avoid private third-party DOM or component internals as an
application API. If a library cannot expose the row, state, or event model the
product needs, own the component boundary.

Host differences belong behind one runtime boundary. Code that launches or
cancels work, waits for descendants, locks shared resources, or publishes a
release calls that boundary instead of branching on operating-system command
names. Unix process groups and advisory locks and Windows Job Objects and
`LockFileEx` are adapter details. Archive acquisition, checksum verification,
strict extraction, manifest verification, and atomic publication similarly
form one bundle-install transaction; no caller may observe a half-installed
release.

## Fail specifically

Add `try/catch` or `try/except` only for a failure the current layer can handle
or translate. Do not catch broadly and invent a success-looking result.

Prefer typed APIs, protocol fields, and deterministic parsing over keyword
heuristics. A fallback is acceptable only when its semantics are explicit,
tested, and owned. "Best effort" must not turn invalid publication, persistence,
security, or contract state into apparent success.

## Enforcing dependency direction

The target config names modules and their allowed direct dependencies:

```json
{
  "name": "publishing-delivery",
  "paths": ["internal/http/publishing/**", "web/src/publishing/ui/**"],
  "dependsOn": ["publishing-domain", "shared-values"]
}
```

Every executable file belongs to exactly one module and the module graph must
be acyclic. Omitting `dependsOn` means the module may use only itself and
external packages. That makes foundational domain modules independent by
construction.

The Go adapter parses imports with Go's parser, resolves every nested `go.mod`,
and rejects an internal import whose target module is not a declared direct
dependency.

JavaScript and TypeScript are decided the same way, from import facts the
sealed policy-owned bundle reads. A target declares no architecture provider,
pins no resolver, and installs none. Which file a written specifier names is
TypeScript's answer rather than a path rewrite: the extension a package omits
or rewrites, the entry its `exports` map selects, and the sibling package a
workspace link points at all resolve the way the ecosystem resolves them.
Static and dynamic imports, `export ... from`, type-only imports, and a literal
`require` are all edges. Resolution reads only inside the repository, so a
specifier that climbs out of it names nothing.

An import that names nothing the repository governs crosses no declared module
boundary: an external package resolves into an installed tree, and a package
the target has not installed resolves to nothing. What that import may reach is
still decided, one package at a time.

A package reaches only the dependencies its own manifest declares. The nearest
`package.json` above a file owns it, and an `architecture.packageDependency`
finding names an import that manifest does not admit:

- a package it declares nowhere, which the file reaches only because a
  neighbouring manifest happens to provide it; and
- a package it declares only in `devDependencies`, reached from source that
  ships.

A declaration in `dependencies`, `peerDependencies`, or `optionalDependencies`
admits an import from any file of the package, and a package importing itself
by name imports what it already declares. The name a manifest writes is the
name source imports, so an aliased declaration needs no interpretation.

An undeclared dependency is decided from what the target installed: an import
resolving into an installed tree names a package that really exists, while a
specifier resolving to nothing may be a project's own path alias, which policy
resolution does not apply. A module the pinned runtime provides is no installed
package, written with or without the `node:` prefix.

Source that never ships may import a development dependency. Tests already
never ship; `scope.development` declares the configuration, scripts, and
harnesses that also do not, because only the target knows which of its files it
ships:

```json
{
  "scope": {
    "development": ["frontend/*.config.ts", "tools/**"]
  }
}
```

A manifest the reader could not read is an `architecture.importCoverage`
finding on that manifest, reported once for the package rather than once for
every import decided against it.

Coverage fails closed instead of reading as clean. Governed source the reader
cannot parse without a compiler, a file it could not read, and a dynamic import
whose specifier is computed are each an `architecture.importCoverage` finding:
a boundary that was never read is not a boundary that was respected.

### Built-in Python evidence

Python is a built-in architecture capability. Do not declare an `architecture`
provider for Python source: `doctor --strict` and the normal architecture pass
already require and run the policy-owned evidence.

Before Python quality or architecture work, Code Polishy builds one inventory
from all governed files. Each `.py` and `.pyi` file belongs to its nearest
contained `pyproject.toml` project. The inventory records the project root, a
contained direct `src` root, every normalized in-tree PEP 517
`build-system.backend-path` root and its direct `src`, the exact project files,
regular and namespace-package candidates without requiring `__init__.py`, and
an existing project-local `.venv`. A nested project owns its own files and
environment; an ancestor cannot absorb it. The manifest is parsed once at this
boundary and the dependency and quality paths reuse the validated facts.

No project owner, ambiguous project ownership, unreadable or escaping paths,
non-regular source, unreadable manifests, and unsupported project layouts fail
closed. A project-level structural failure is reported once as
`policy.pythonProject`, and dependent quality and graph analysis stops instead
of producing one secondary failure per file and tool. Ambient `VIRTUAL_ENV`,
`PYTHONPATH`, shell startup files, and executable lookup do not select a project
or an environment.

For each selected project, the pinned Ruff runs `analyze graph` in isolated
mode. Code Polishy supplies the `project.requires-python`-derived target and
the validated source roots, including `src` when present, asks Ruff to detect
literal string imports and imports under type-checking branches, and parses the
bounded graph output once. Target Ruff configuration cannot change this
evidence. Code Polishy, rather than Ruff or a target command, then decides file
and module ownership, allowed `dependsOn` edges, and coverage.

The built-in resolver covers flat, direct `src`, and in-tree PEP 517 backend
layouts, nested projects with overlapping import names, regular and namespace
packages, package
`__init__.py` re-exports, `.py` and `.pyi` modules, absolute and valid relative
imports, and exact one-argument `importlib.import_module(...)` or
`__import__(...)` calls whose argument is one plain string literal.
Standard-library and third-party imports create no repository module edge.

Computed, formatted, concatenated, escaped, triple-quoted, or multi-argument
dynamic imports are unproven. Parenthesized references to a known import
function are also unproven instead of disappearing from coverage. Calls and
arguments may use ordinary whitespace or an explicit line continuation without
changing an otherwise exact literal into computed evidence.

Every local edge must resolve to one contained governed Python file in the same
project and to exactly one repository module. An omitted selected file,
malformed graph response, ambiguous or escaping resolution, unreadable source,
unresolved local-looking import, or dynamic import that cannot be proven emits
`architecture.importCoverage`; it never reads as a clean graph. A resolved
cross-module edge without the declaring module's `dependsOn` entry is the
ordinary `architecture.moduleDependency` finding. Code Polishy does not execute
project Python code to resolve imports.

Rust, Java, and other ecosystems still connect a target-native architecture
command through `checks`:

```json
{
  "name": "service-architecture",
  "provides": ["architecture"],
  "argv": ["uv", "run", "check-architecture"],
  "cwd": "service",
  "modules": ["service"],
  "runOn": ["check", "gate"]
}
```

`doctor --strict` fails if an executable language module lacks that provider.

What an external package is -- its pin, source, age, license, and known
vulnerabilities -- remains supply-chain policy; architecture decides only which
package may reach it. Generated paths
remain discoverable and their imports still follow module direction;
generation is not a boundary bypass. Dependency direction is still a guardrail
rather than the entire design: refined types, constructors,
generators, atomic operations, ports, and boundary tests make semantic error
classes impossible.

Architecture exceptions use the central exception list, matching the exact
architecture check, source path, and target subject. There is no permanent
per-module ignore list, and policy coverage itself cannot be exempted.

## Architecture review template

For a material component or workflow, record:

```text
Concept and owner:
Invalid states to exclude:
Ingress and validation boundary:
Public interface (1–3 common entry points when possible):
Hidden implementation:
Dependency categories:
Side-effect/transaction boundary:
Generated consumers:
Observable boundary tests:
Import rules:
Superseded paths and tests to delete:
```

Architecture work is complete only when callers use the new interface, the
superseded implementation and shallow tests are removed, and the executable
boundary is green.
