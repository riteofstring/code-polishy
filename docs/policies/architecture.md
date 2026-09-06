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

Every governed production executable belongs to exactly one module and the module graph must
be acyclic. Omitting `dependsOn` means the module may use only itself and
external packages. That makes foundational domain modules independent by
construction.

Tests declare their production boundary independently through exactly one
`tests.ownership` entry naming a `module` and its primary quick `focusedSuite`.
Production module paths do not infer test ownership. `tests.paths` adds
unconventional test paths, and the named suite must explicitly include its
owned tests. Test imports are omitted from the production graph: a test may
exercise collaborators or span modules without authorizing its production
owner to depend on them.

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

Generated JavaScript and TypeScript may declare one
`scope.generatedJavaScript[].sourcePackage`. The output inherits that real
package's workspace, manifest, lockfile, TypeScript resolution, dependency
context, and module owner. It stays generated and non-rewritable; no synthetic
package boundary is accepted. Missing, overlapping, stale, non-generated, or
cyclic ownership fails before import evidence is trusted.

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
the validated source roots, including `src` when present, asks Ruff to include
imports under type-checking branches, and parses the bounded graph output once.
Arbitrary string literals are not import evidence. Proven dynamic loader calls
come from the bounded Python facts resolver and add their exact targets to the
graph. Target Ruff configuration cannot change this evidence. Code Polishy,
rather than Ruff or a target command, then decides file and module ownership,
allowed `dependsOn` edges, and coverage.

The canonical source graph records one `python-facts/v3` input per analyzed
Python project, covering its exact source paths. Its identity binds the
normalized facts, the streamed source snapshot, and cross-file resolution
evidence. The bounded resolver returns its compact source facts for independent
digest and callsite validation without sending that model through a second
Python process. Missing, duplicated, or mismatched coverage withholds the graph.
Cycle and architecture-review topology identities depend on semantic
dependencies and ownership, so a source-body change can invalidate graph
evidence without changing those semantic identities.

The built-in resolver covers flat, direct `src`, and in-tree PEP 517 backend
layouts, nested projects with overlapping import names, regular and namespace
packages, package `__init__.py` re-exports, `.py` and `.pyi` modules, absolute
and valid relative imports, and exact one-argument calls through statically
proved aliases of `importlib.import_module` and `builtins.__import__`.
Standard-library and third-party imports create no repository module edge.

Module loaders and their supporting JSON, path, and entry-point calls resolve
through the complete compact project, including imported aliases and re-exports
across source partitions. Bounded collection and conditional-choice facts
preserve every possible target. Exact start and end positions distinguish
nested calls with the same starting location. Binding activation and branch
facts preserve definition-time inputs and lexical shadowing; wildcard or
ambiguous bindings cannot prove a loader. Resolved `typing.TYPE_CHECKING`
guards and stub files retain type-only dynamic edges.

A plain literal target is resolved directly. Any other recognized computed
call must have one exact `scope.pythonComputedImports` declaration. The Python
facts adapter binds the current source digest, project, importer module,
containing callable or module scope, callee, one-based line and column,
canonical AST shape, and canonical argument expression. Moving or rewriting
the call, changing its alias, adding another match, or changing the source makes
the declaration stale.

The target contract is either a contained namespace or one current PEP 621
entry-point group. A namespace declaration names an exact in-source target set,
one or more exact JSON configuration paths and non-root JSON pointers, or both.
Each JSON input carries its current digest and may select only one module name
or an array of module names beneath the declared non-top-level namespace. An
entry-point declaration resolves only the validated entries already present in
the same contained project. Empty, wildcard, escaping, external, ambiguous,
unresolved, environment-derived, or network-derived target domains fail
coverage.

Every possible target becomes an ordinary local import edge. It must resolve to
one governed file and its source module must already allow the target owner in
`dependsOn`; the declaration is evidence, not a second dependency allow-list.
`scope.pythonDynamicReferences` remains a separate exact Vulture reachability
contract and cannot satisfy computed-import coverage in either direction.

`pkgutil.resolve_name` object loads resolve over the compact facts for the whole
project, including loader aliases and re-exports across source partitions.
A literal `module:object` argument contributes a `proven-dynamic` edge to its
local runtime module. Its normalized absolute module and identifier-chain
object must satisfy `python-module-object/v1`; relative names, wildcards, and
import expressions are unproven. Ambiguous and wildcard loader bindings fail
coverage. A local parameter that shadows the loader is not an import.

For a registry object load, `scope.pythonComputedImports` must independently
bind the source and one governed JSON input. Use `callee: "pkgutil.resolve_name"`
and `shape: "module-object-call/v1"`, plus the exact source digest, module,
callable or module scope, line, column, canonical argument, and namespace.
Its single `configuration` entry names the registry path, JSON pointer, and
current SHA-256. It has no `targets` inventory or `entryPointGroup`.
The supported argument is a direct expression such as
`json.loads(Path('src/app/registry.json').read_text(encoding='utf-8'))['plugins'][name]`,
where the final dynamic index is one current callable parameter. A fixed
selector may name one string, and an empty pointer may select the JSON root.
The current bounded registry supplies the object targets; every module must
remain beneath the declared non-top-level namespace and resolve inside the
same project. Registry paths in source are relative to the project root;
configuration paths are relative to the repository root.

Registry files must be governed handwritten regular files of at most 2 MiB,
without symbolic links or changes during the read. Duplicate keys, unsupported
JSON constants, empty target sets, and malformed object names fail coverage.
The graph's resolution identity binds the actual registry bytes and derived
targets. Selecting a registry checks its consuming project; unrelated selected
documents do not activate configured Python consumers.

External object loads use `scope.pythonExternalPluginImports`. Each declaration
binds one exact loader consumer to a canonical distribution name, an explicit
owned import namespace, `inputGrammar: "python-module-object/v1"`, and a
runtime check. The current contained manifest and adjacent `uv.lock` must
identify the same direct runtime or optional dependency through an exact
registry pin or exact Git commit. Transitive-only and moving dependencies do
not satisfy the contract. Namespace ownership is an explicit repository
declaration; distribution spelling does not establish an import namespace.

The independent object-import resolver checks every literal or governed-registry
target against that namespace. Registry input must match its declared path,
JSON pointer, and current digest. Local modules and standard-library modules
cannot become external composition targets. The loaded object must then pass a
rejecting `isinstance` or `issubclass` check against the declared runtime type,
or a proved synchronous validator containing that check. An annotation, an
ignored boolean, a different checked value, or an unconstrained runtime input
does not establish this evidence. Invalid or missing evidence emits
`policy.pythonExternalPluginImport` and withholds a complete graph. Admission
applies only to that loader callsite; other calls keep their own coverage rules.

For a runtime parameter, a rejecting guard must call a governed predicate before
the loader uses the same unchanged parameter. The supported predicate has one
parameter and returns this conjunction, with the declared namespace substituted
in both prefixes:

```python
import keyword
import unicodedata


def permitted(value):
    return (
        type(value) is str
        and unicodedata.normalize("NFKC", value) == value
        and value.count(":") == 1
        and value.startswith(("third_party.plugins:", "third_party.plugins."))
        and all(
            part.isidentifier() and not keyword.iskeyword(part)
            for part in value.replace(":", ".").split(".")
        )
    )
```

Use `if not permitted(name): raise ValueError(...)` before loading `name`,
then perform the separately declared runtime protocol check on the loaded
object. The name predicate admits normalized Unicode identifiers while
rejecting keywords, empty components, expression syntax, extra colons, and
neighboring namespace prefixes. Function and parameter names may differ;
imports, aliases, and re-exports must resolve to the exact built-in and
standard-library operations. The predicate must be synchronous, undecorated,
and contain only that return expression. A swallowed or conditional rejection,
rebound input, different checked argument, or check after loading is unproven.
The graph binds the current predicate source digest as well as the loader's
source. Predicate resolution uses the whole compact project and executes no
application code.

Successful declarations appear in the graph's separate `externalCompositions`
collection and module summaries. Each entry retains dependency, manifest,
lockfile, loader input, and runtime-check evidence. Reports, SARIF, saved gates,
and architecture review packets preserve those entries. They neither create
local dependency permissions nor enter local cycle traversal. Review topology
tracks semantic external contracts, while proof digests remain part of the
complete graph identity. Selecting the manifest, lockfile, registry, or
configuration activates its declared consumer; unrelated documents do not.

Computed shapes outside the bounded enumeration, governed-JSON, and PEP 621
entry-point forms are unproven. Multi-argument calls and parenthesized
references to a known import function are also unproven instead of disappearing
from coverage.

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
Source cycles, source/import coverage, and architecture-review signals are not
suppressible. Architecture agent review is optional and explicitly requested.

## Architecture summary

`code-polishy architecture` always reports, per module, production-file count,
test-file count, incoming and outgoing declared dependencies, and quick focused
suite count. These facts make oversized or overly central modules visible for
review. Names, file counts, percentages, and dependency degree are not arbitrary
blocking thresholds; exact ownership, acyclic direction, import evidence, and
focused test coverage remain the enforced invariants.

`architecture --all` also reports deterministic review signals for one
production module, modules spanning independently discovered projects or Go
packages, disconnected production components, catch-all ownership across source
roots, and an empty declared graph hiding internal source dependencies. A
signal selects qualitative judgment without proving a defect or imposing a
minimum file or module count.

## Architecture review workflow

Architecture agent review is optional and runs only when the caller explicitly
requests it. Automated ownership, import coverage, cycle, and dependency checks
remain mandatory independently of agent review. For a requested review, capture
the request, retrieve current design context, and commit the candidate before
preparing its packet. Use one explicit trusted base for these commands:

```sh
code-polishy architecture-review status --base REVIEW_BASE
code-polishy architecture-review prepare --base REVIEW_BASE
code-polishy architecture-review finalize --base REVIEW_BASE
```

All three commands require a clean committed candidate. They obtain the complete
normalized source graph through its owning analyzers and run no tests or AI
provider. Resolve deterministic graph, import, ownership, and cycle failures
before preparation. `status` reports whether review is required and validates
existing evidence. It creates no review packet or receipt.

`prepare` writes a bounded `architecture-review/v1` packet under
`.code-polishy-reports/architecture-review/reviews/`. The packet contains exact
revisions, the declared module contract, explicit test ownership, the canonical
source graph and roots, cycle results, module summaries, structural signals,
the Git patch, complete committed source for every graph node (including
unchanged files), and only mapped current design documents. Source entries
provide paths, UTF-8 contents, and exact content digests so configuration-only
adoption can be reviewed using implementation evidence. Its topology diff
compares against the last valid accepted review; an unreviewed graph has an
explicit empty baseline. Packet size is limited to 128 MiB, the Git patch to
16 MiB, each source file to 8 MiB, total source contents to 64 MiB, and reviewer
output to 256 KiB. Missing, non-regular, non-UTF-8, or oversized source fails
preparation. Oversized evidence fails without a
truncated packet or an implied pass.

The harness starts a reviewer with no inherited conversation and supplies only
that packet. The locked instructions in `templates/architecture-review.md`
require concrete packet citations and an explanation of real concept ownership,
boundary depth, direction, disconnected responsibilities, and forwarding-only
rewrites. Findings include exact evidence and a corrected module/ownership graph.
The harness saves the strict JSON result at the packet's result path and runs
`finalize`. Empty evidence, invented citations, duplicate or unknown fields,
findings, and changed candidate material cannot produce acceptance.

The shipped `schema/code-polishy-architecture-review.schema.json` defines the
packet, preparation binding, receipt, and result structures. Duplicate keys and
nesting beyond 64 levels are rejected before object decoding. Schema resolution
uses only shipped resources. Artifact byte limits, exact source and candidate
bindings, topology identities, and citation validation remain independent of
structural validation.

Checkpoint and merge gates do not require, read, or fingerprint architecture
review artifacts. Explicit review commands continue to validate acceptance and
report structural signals. Reuse requires the same review base, module contract,
project/package roots, ownership map, semantic source topology, instructions,
and mapped design documents; the reviewed candidate must be an ancestor of the
current candidate. Changed or missing evidence cannot pass an explicit review.

Report only major or severe defects with concrete consequences and packet
citations. Minor issues, preferences, optional refactors, and speculative risks
are not findings. Return one review result. Present unresolved findings to the
caller instead of automatically repeating reviews after fixes. Every follow-up
requires an explicit caller request and stays within that scope.

The calling harness supplies the reviewer; Code Polishy embeds no AI SDK and
receives no provider credentials. Local hashes establish candidate consistency,
not reviewer identity or proof of clean context. AI review cannot replace any
separately required human approval.

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

## Operator-selected Python adapters

Use `scope.pythonRuntimeLoaders` when an operator supplies an open
`package.module:object.path` target at startup. This is an explicit delegation of
architecture authority, not a finite dependency inventory or proof that arbitrary
imports are safe. It does not retain dead-code exports; use `scope.pythonContracts`
for those independently.

Each declaration contains `project`, `consumer`, `inputGrammar`, `check`, and a
nonempty `reason`. The consumer identifies `kind: "callsite"`, exact `importer`,
`module`, `callable`, one-based `site` (line and column),
`callee: "importlib.import_module"`, `shape: "call"`, the module argument text,
and the importer's `sourceSha256`. The check identifies `kind: "isinstance"`,
the qualified local runtime protocol, and its exact one-based call site.

The initial `inputGrammar: "ascii-module-object/v1"` requires a module-level
`re.compile` with this pattern and no flags:

```text
[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*:[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*
```

The single-parameter synchronous loader must perform these statements in order:

1. Reject `PATTERN.fullmatch(target) is None` with a raise.
2. Split into module and object names with `target.split(":", maxsplit=1)`
   (positional `1` is also supported).
3. Assign `importlib.import_module(module_name)` to a local name.
4. Walk `object_path.split(".")`, assigning `getattr(loaded, attribute)` back
   to that name.
5. Reject `not isinstance(loaded, Protocol)` with a raise.
6. Return the checked name.

Names are arbitrary; annotations are optional. Protocols must resolve to a
supported local runtime-checkable type. Decorated loaders, extra statements,
shadowed operations, and other control-flow shapes require further analysis and
remain coverage errors. Changing source requires refreshing the declaration's
digest and any moved sites.

Accepted declarations emit an informational coverage finding naming the
operator-controlled boundary. Direct literal calls, including calls through
resolved imports and re-exports, supply ordinary local dependency edges.
Nonlocal targets are external; local-looking missing modules remain errors.
This does not claim to infer every constant expression or runtime target.
Repository dependency admission and vulnerability checks still run, but cannot
scan arbitrary packages installed only on an operator's machine. Import executes
before the protocol check.
