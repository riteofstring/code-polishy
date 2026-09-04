# Standards Parsing and Evidence Hardening

Status: proposed

Target release: v0.23.0

## Outcome

Keep the policy direction established from v0.18 through v0.22 while reducing
the amount of standards machinery implemented directly by Code Polishy.

Code Polishy continues to own repository boundaries, policy decisions,
execution authority, evidence requirements, and failure behavior. Mature
external components may parse standardized formats and produce bounded facts;
they do not decide whether a repository passes.

The resulting implementation should be smaller, more interoperable, and easier
to validate without weakening any existing standard. Receipt reuse becomes
explicitly dependent on a suite's complete, enforceable input model, and
release evidence becomes independently verifiable rather than merely
well-formed.

v0.23.0 does not ship until every phase and completion criterion in this plan
is complete. Private implementation commits may land incrementally, but the
public release is one coherent cutover with no old parser fallbacks, partially
active receipt contract, or unauthenticated substitute for required publication
evidence.

## v0.23 release boundary

This entire plan is the v0.23 scope. No required phase may be deferred to a
later release while v0.23 is declared complete.

Where the plan calls for an evaluation rather than unconditional adoption,
v0.23 must finish that evaluation and implement its resulting decision. An
external component is not required when the evaluation proves that a smaller
existing boundary is stricter or more interoperable. That decision and its
verification evidence must be recorded before release rather than left as
future work.

## Release-direction assessment

| Release direction                                       | Keep                                                                                      | Improve                                                                                                                        |
| ------------------------------------------------------- | ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| v0.18-v0.19 repository quality and source analysis      | Deterministic local checks and exact findings                                             | Replace handwritten Markdown, shell, and other standards parsers with maintained syntax implementations                        |
| v0.20 behavior evidence and language-pack extensibility | Strict bounded protocols, exact identities, fail-closed execution, and engine-owned gates | Keep the simple process boundary; add a restricted WASI tier only if untrusted packs become a supported use case               |
| v0.21 language and ecosystem expansion                  | Ecosystem-specific facts and consistent policy outcomes                                   | Use canonical ecosystem parsers and existing analyzer output instead of recreating Python packaging and source semantics in Go |
| v0.21 workflow and monitoring evidence                  | Separation between static repository checks and external operational evidence             | Parse workflow structure semantically and retain provider APIs for facts that static files cannot prove                        |
| v0.22 suite-receipt reuse                               | Exact release, command, toolchain, environment, ownership, and input identities           | Make reusable suites explicitly cacheable and hermetic; fail closed when their input boundary cannot be enforced               |
| v0.22 SBOM and provenance                               | Deterministic release identity and digest binding                                         | Use official data models, enumerate complete dependency inventories, and authenticate provenance at the publication boundary   |

These are implementation changes, not permission expansions. A more capable
parser must not turn unsupported, ambiguous, malformed, or unevidenced input
into a clean result.

## Ownership rule

Adopt external components only for standardized mechanics:

- syntax parsing and abstract syntax trees;
- version, requirement, marker, and license-expression grammar;
- published schema and attestation data models;
- ecosystem-native metadata interpretation;
- workflow syntax validation.

Keep the following inside Code Polishy:

- which files and modules are governed;
- which capabilities and evidence are required;
- dependency direction and policy thresholds;
- exception ownership and expiry;
- command, environment, network, and write boundaries;
- whether evidence is sufficient to pass;
- suite selection, reuse eligibility, and final-gate behavior.

Every adapter returns strict, size-bounded, versioned facts. Code Polishy
validates those facts and applies policy itself. No library diagnostic, parser
recovery, or omitted result becomes an implicit pass.

## Recommended components

### Runtime configuration schema

Use
[`github.com/santhosh-tekuri/jsonschema/v6`](https://github.com/santhosh-tekuri/jsonschema)
to validate target configuration against the shipped JSON Schema before typed
decoding. Retain handwritten validation only for defaults, repository state,
and semantic relationships that JSON Schema cannot express clearly.

The shipped schema remains the structural source of truth. Tests must prove
that runtime acceptance cannot drift from it.

### Python metadata and source facts

Replace the handwritten TOML, PEP 440/508, marker, token, and import machinery
with one batched Python facts adapter using:

- CPython `tomllib` for `pyproject.toml`;
- CPython `tokenize` and `ast` where the carried runtime supports the selected
  source syntax;
- PyPA [`packaging`](https://packaging.pypa.io/) for requirements, versions,
  specifiers, markers, metadata, and direct references;
- Ruff's existing dependency-graph output when it already supplies the
  required architecture facts.

The adapter receives only the engine-selected inventory and emits strict,
bounded JSON. One process should analyze a batch of files to avoid per-file
startup cost.

Before relying on `ast`, align the carried CPython parser with the highest
accepted Python syntax target or narrow the declared target. The current
carrier must not silently treat newer valid syntax as malformed or absent.

This implementation belongs to the Python owner. If Python becomes an
installable first-party pack, the adapter and its dependencies move in that
same atomic cutover rather than leaving Python semantics in core.

### Intentional computed Python imports

Add `scope.pythonComputedImports` for intentional computed imports such as
`importlib.import_module(configured_module)`. This is an architecture-evidence
contract and remains separate from `scope.pythonDynamicReferences`, which only
describes exact symbol reachability for dead-code analysis. Neither declaration
may satisfy the other check.

Each computed-import declaration binds all of the following:

- the exact contained `pyproject.toml` project;
- the exact governed importer path and resolved importer module;
- the exact containing callable, or an explicit module-scope callsite;
- the recognized callee, limited initially to `importlib.import_module` and
  `builtins.__import__` including statically proven aliases;
- the source line, column, canonical call shape, and argument expression;
- exactly one target contract: a contained namespace or a validated PEP 621
  entry-point group;
- every governed configuration input used to select a module name when the
  target is configuration-driven.

The source location is diagnostic as well as identifying. The engine binds the
declaration to the current parsed call expression and source digest so moving,
rewriting, deleting, or changing the argument makes the declaration stale.

For a namespace target, Code Polishy accepts no wildcard, empty, repository-root,
or unconstrained top-level namespace. Every configured value must be a valid
absolute Python module name contained by the declared namespace. The argument
must be statically traceable through a supported bounded shape to the declared
governed configuration values or to an exact in-source enumeration. Ambient
environment, network, installed-package enumeration, arbitrary user input, and
unparsed executable configuration remain unproven.

For an entry-point target, the group name is exact and resolves only through
the already validated contained project metadata. The group must exist and
contain at least one current entry. Each entry's module and symbol remain
subject to the normal PEP 621 validation and project containment rules.

Code Polishy resolves every possible in-repository target, derives the importer
and target module owners, and checks every resulting edge against the existing
module `dependsOn` graph. The declaration never authorizes an architecture
edge itself and cannot create a second dependency allow-list. If possible
targets span several modules, every edge must already be permitted.

Reject an undeclared call, an ambiguous or duplicate callsite, an overly broad
target, an escaping or unresolved local target, an unmodeled configuration
source, a disallowed architecture edge, a declaration that matches no current
call, or a declaration whose target set has become empty. A failure remains
`architecture.importCoverage` or `architecture.moduleDependency` as
appropriate; it never degrades to an informational warning.

### Markdown and shell syntax

Use [`github.com/yuin/goldmark`](https://github.com/yuin/goldmark) for
CommonMark and GFM structure. Apply Code Polishy's link, fragment, prose, and
artifact rules to its syntax tree.

Use [`mvdan.cc/sh/v3/syntax`](https://github.com/mvdan/sh) for shell syntax,
comments, heredocs, substitutions, and quoting. Keep ShellCheck and Code
Polishy policy findings separate from parser diagnostics.

Markdown remains a core repository concern. Shell parsing follows shell
language ownership and moves with the shell pack if that plan is implemented.

### Workflow and schedule validation

Use the already sealed `js-yaml` runtime to produce a typed, bounded GitHub
Actions workflow representation. Add pinned
[`actionlint`](https://github.com/rhysd/actionlint) execution for workflow
syntax, expression, and schedule validation.

If policy must calculate schedule gaps rather than only validate cron syntax,
evaluate [`github.com/robfig/cron/v3`](https://github.com/robfig/cron) for that
one bounded responsibility.

Static analysis may prove that an active workflow contains a reachable gate or
schedule. It cannot prove branch protection, that a schedule is enabled, or
that a recent provider run succeeded. Those facts remain external evidence
obtained through an explicitly configured provider API.

### SPDX expressions

Evaluate
[`github.com/github/go-spdx/v2/spdxexp`](https://github.com/github/go-spdx)
for SPDX identifier and expression semantics. Adoption requires differential
tests for `AND`, `OR`, `WITH`, `+`, parentheses, exceptions, and `LicenseRef`
handling because Code Polishy's allow-list decision must remain exact.

Keep the current bounded subset if the intended policy deliberately rejects
valid SPDX constructs that the external package accepts.

### SBOM and provenance

Use
[`github.com/CycloneDX/cyclonedx-go`](https://github.com/CycloneDX/cyclonedx-go)
for CycloneDX models and serialization. Build the component and dependency
graph from authoritative inventories for:

- Go modules;
- the pnpm lock and sealed JavaScript bundle;
- Python distributions and their metadata;
- standalone bundled executables;
- first-party language packs and their carried tools.

Use official
[`github.com/in-toto/attestation`](https://github.com/in-toto/attestation)
types rather than map-shaped in-toto and SLSA statements.

Generate authenticated provenance at the CI publication boundary with OIDC and
Sigstore-compatible signing or the hosting provider's artifact-attestation
service. The verifier must bind the artifact digest to the expected repository,
workflow, source revision, builder identity, and release event. A statement
created and stored beside its artifact without authenticated issuer identity is
deterministic metadata, not independent trust evidence.

Syft or another independent SBOM generator may run as an explicitly selected
supplemental completeness comparison. It does not replace the canonical
deterministic release inventory.

### Untrusted language packs

Keep the current one-shot JSON-over-stdio native pack protocol for trusted
first-party tools. Do not replace it with a long-lived RPC plugin framework.

If arbitrary third-party code becomes a supported product boundary, prototype
an optional WASI execution tier using
[`wazero`](https://github.com/wazero/wazero). The tier must expose only declared
inventory, temporary output, environment, clock, and network capabilities.
Native analyzers that cannot run under WASI remain an explicitly trusted mode;
WASI is not a label applied to unsandboxed execution.

## Receipt reuse changes

No general-purpose library can discover every input consumed by an arbitrary
test command. Improve v0.22 reuse through an execution contract:

1. A suite explicitly declares whether it is reusable across candidates.
2. Reusable suites declare their complete repository, control, environment,
   toolchain, pack, and external inputs.
3. Where practical, the runner exposes only those inputs through a read-only
   execution view and provides isolated writable temporary and artifact paths.
4. Undeclared repository access fails the suite or marks it unbounded; it never
   produces a reusable receipt.
5. A platform-specific audit mode may trace file access to diagnose incomplete
   declarations, but tracing is supporting evidence rather than the portable
   policy boundary.
6. Suites that cannot be made hermetic continue to run for every candidate.

Do not introduce Bazel, Nix, or another build system solely to obtain caching.
Code Polishy must continue to govern ordinary repositories without requiring
them to adopt a new build graph.

## Dependency admission

This plan does not select floating or latest versions. For each adopted
component:

1. choose an exact version that satisfies the repository's minimum release-age
   policy;
2. generate the candidate lock without lifecycle scripts;
3. run `code-polishy dependency-review --base <merge-target>` before
   installation;
4. review licenses, maintainership, published vulnerabilities, transitive
   dependencies, supported platforms, and release provenance;
5. carry or resolve the component through the same sealed tool boundary in
   local and CI execution;
6. record it in the release inventory and SBOM.

Prefer a standard-library facility or an already carried tool when it provides
the required semantics. A dependency is justified when it materially reduces
grammar, interoperability, or security risk beyond the supply-chain surface it
adds.

Several recommended projects may have a latest release younger than the
minimum age at implementation time. Select the newest admitted release rather
than weakening the age rule.

## Components not recommended

Do not adopt the following as part of this plan:

- OPA or CUE as a replacement policy language: Code Polishy's typed policy and
  exact product semantics remain clearer without another runtime and DSL;
- Cobra solely for CLI construction: the existing exact help, command, and
  error contracts do not justify its dependency graph;
- HashiCorp go-plugin or gRPC for language packs: process longevity and RPC
  complexity do not improve the current bounded protocol or sandboxing;
- GoReleaser as the release authority: the sealed multi-runtime assembly and
  exact release identity remain product-specific;
- Syft as the canonical SBOM generator: use it only as independent
  supplemental evidence;
- a generic glob package as a direct replacement for policy overlap analysis:
  ordinary matching libraries do not provide the same static ambiguity proof.

## Implementation sequence

### Phase 0: Freeze observable contracts

1. Inventory every handwritten standards parser and its policy consumers.
2. Separate syntax facts from Code Polishy decisions at each boundary.
3. Build accepted, rejected, malformed, ambiguous, and adversarial fixture
   corpora from current observable behavior and the governing standards.
4. Record performance and resource ceilings for large bounded inputs.
5. Resolve each parser's future core or language-pack owner.
6. Inventory every current computed Python import and classify its argument
   source, possible targets, and architecture owners.

### Phase 1: Remove configuration drift

1. Compile and validate the shipped JSON Schema at runtime.
2. Retain typed decoding, defaults, and explicit semantic checks.
3. Test every schema conditional and unknown-field boundary against runtime
   behavior.
4. Remove duplicate handwritten structural validation once equivalence is
   proven.

### Phase 2: Replace high-risk Python and syntax implementations

1. Introduce the batched Python facts adapter and resolve supported syntax
   versions.
2. Add the strict `scope.pythonComputedImports` schema and AST-backed callsite
   validation.
3. Resolve bounded configured targets and derive every architecture edge from
   the governed module graph.
4. Keep dead-code dynamic references and architecture computed imports separate
   in configuration, validation, findings, documentation, and receipts.
5. Replace Markdown structure parsing with goldmark.
6. Replace shell lexical and syntax interpretation with `mvdan/sh`.
7. Compare old and new facts over the frozen corpus and fuzzed inputs.
8. Remove each old implementation and its fallback in the same coherent
   cutover after equivalence or an intentional tightening is documented.

### Phase 3: Strengthen standardized evidence

1. Parse workflows structurally and add actionlint validation.
2. Decide whether complete SPDX semantics or the deliberate subset is the
   product contract.
3. Move CycloneDX and in-toto output to official models.
4. Produce a complete dependency graph from release inputs.
5. Add authenticated publication provenance and independent verification.

### Phase 4: Make reuse authority explicit

1. Add the reusable and hermetic suite contract.
2. Bind every declared input and execution capability into receipt identity.
3. Enforce declared read and write boundaries where supported.
4. Keep incomplete or unbounded suites non-reusable.
5. Add diagnostics showing exactly why a receipt was reused or rejected.

### Phase 5: Evaluate optional isolation

Prototype WASI only after third-party pack execution is an approved product
goal. Measure analyzer compatibility, startup cost, filesystem mediation,
diagnostic quality, and platform support before defining a public contract.

## Verification

Every parser replacement must demonstrate:

- identical policy outcomes for intentionally preserved behavior;
- explicit tests and release notes for intentional tightening;
- fail-closed behavior for parser errors, recovery, truncation, and unsupported
  syntax;
- deterministic bounded facts independent of ambient files and environment;
- property or fuzz coverage at grammar and resource-limit boundaries;
- no old parser, fallback, configuration switch, or dual implementation after
  cutover;
- native Windows, macOS, and Linux behavior where the capability is supported.

Computed-import coverage must additionally prove:

- undeclared computed imports continue to fail closed;
- an exact declaration accepts only its current callsite and bounded targets;
- moved, removed, duplicated, or rewritten calls make declarations stale;
- malformed, wildcard, empty, ambiguous, escaping, and external target domains
  are rejected;
- every possible governed target produces an architecture edge checked against
  the existing `dependsOn` graph;
- a changed configuration value invalidates evidence and cannot escape its
  declared namespace or entry-point group;
- `pythonDynamicReferences` cannot satisfy import coverage and
  `pythonComputedImports` cannot preserve a Vulture symbol.

Release-evidence work must additionally prove:

- the SBOM conforms to the selected CycloneDX version;
- every shipped dependency and executable is represented once with stable
  identity and relationships;
- attestation verification rejects the wrong repository, workflow, revision,
  builder, event, or artifact digest;
- local deterministic metadata is not accepted as authenticated publication
  evidence.

Receipt work must prove that every modeled input change invalidates reuse,
unrelated changes preserve only eligible receipts, undeclared access cannot
produce reusable evidence, and unbounded suites always execute.

## Completion criteria

- Runtime configuration structure is governed by one shipped schema.
- Python packaging, Markdown, and shell syntax use maintained standards
  implementations at their correct ownership boundaries.
- Intentional computed Python imports require exact, non-stale, bounded
  declarations whose possible targets produce ordinary enforced architecture
  edges.
- Workflow checks parse active structure and do not overclaim external state.
- SPDX handling is either standards-complete or explicitly documented as a
  narrower fail-closed policy.
- Release SBOMs contain the complete shipped dependency graph.
- Published provenance is authenticated and independently verifiable.
- Cross-candidate receipt reuse is limited to explicitly reusable suites with a
  complete enforceable input contract.
- External components cannot weaken thresholds, suppress required evidence, or
  decide pass and fail.
- Every added dependency is exactly pinned, admitted, reviewed, inventoried,
  and included in release evidence.
- Every phase and completion criterion is delivered together in v0.23.0.

## Related plans

- [Language-Pack Discovery and Universal Capabilities](universal-language-pack-capabilities.md)
- [Installable First-Party Language Packs](installable-first-party-language-packs.md)
