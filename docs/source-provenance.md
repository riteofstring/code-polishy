# Source Provenance

Code Polishy owns its runtime and policy implementation in this repository.
Separately owned application repositories are external inputs, never hidden
runtime dependencies or public integration contracts.

## Public design references

These public projects informed specific policy concepts:

- [Acceptance Pipeline Specification](https://github.com/unclebob/Acceptance-Pipeline-Specification)
  distinguishes executable Gherkin acceptance from deliberate acceptance-data
  mutation.
- [crap4go](https://github.com/unclebob/crap4go) combines complexity and
  missing coverage to identify risky functions.
- [mutate4go](https://github.com/unclebob/mutate4go) demonstrates focused
  differential mutation and explicit handling of surviving mutations.
- [CycloneDX 1.6](https://cyclonedx.org/specification/overview/) defines the
  published native-release SBOM shape.
- [SLSA provenance 1.0](https://slsa.dev/spec/v1.0/provenance) and the
  [in-toto statement](https://in-toto.io/Statement/v1) define the deterministic
  native-release metadata shape. That local metadata authenticates no identity;
  OCI image attestations are emitted separately by Docker Buildx.

Code Polishy implements its own policy engine and uses the independently
maintained Apache-licensed Gremlins release for supplemental Go mutation tests.

## Toolchain origins

Every carried tool is version-pinned in a checked-in file. The online
supply-chain gate resolves each standalone executable's publication timestamp
from its fixed official metadata source and applies the same 30-day admission
rule used for dependency graphs. Installation fetches tools from their official
distribution origins, verifies checked-in checksums, and probes each installed
version before staging a release.

- Go is the policy-engine toolchain. Code Polishy computes cyclomatic
  complexity directly from the standard Go AST.
- Node and pnpm run the sealed JavaScript tool bundle. They are Code Polishy
  runtime dependencies, not target-project toolchain requirements.
- The JavaScript bundle contains Prettier, ESLint,
  `@typescript-eslint/parser`, `eslint-plugin-react-hooks`,
  `eslint-plugin-jsx-a11y`, TypeScript, Knip, `js-yaml`, and `@types/node`.
  `tools/javascript/pnpm-lock.yaml` locks the complete graph and
  `tools/javascript_bundle_inventory.txt` records installed packages and
  licenses. Its bounded `js-yaml` operation reads GitLab control files as data;
  it never executes pipeline configuration or project code.
- ShellCheck, Ruff, `ty`, OSV-Scanner, and Gremlins are downloaded from their
  official release origins and checked against repository-owned versions and
  archive digests. Ruff supplies isolated Python lint, complexity, and
  import-graph facts; `ty` supplies structured Python type diagnostics.
- The release carries Vulture `2.16` and the pinned CPython
  `3.12.13+20260728` distribution from python-build-standalone as separate
  policy inputs. The Vulture PyPI wheel is checksum-verified and unpacked into
  the carried runtime. Their exact pins and checksum inventories ship with the
  release, while the online supply-chain gate resolves their release age from
  fixed upstream metadata services; neither comes from a target environment.
- Trivy is copied from an exact official image digest into the minimal scanner
  image. `artifact-security/scanner-policy.json` records its source,
  configuration, and integrity digests; `artifact-security/scanner.openvex.json`
  records reviewed vulnerability applicability.
- Portable Linux releases use one digest-pinned Ubuntu base only for the OCI
  transport. The image installs Git and CA certificates, copies an already
  verified native release tree, runs as non-root, and does not replace the
  archive's internal release identity.

Two JavaScript packages intentionally remain on constrained release lines.
Knip stays on the last selected release before its parser and resolver required
native `oxc` add-ons. `eslint-plugin-react-hooks` stays on the selected line
before React Compiler rules introduced the Babel toolchain. These constraints
keep the sealed bundle portable and minimal.

## Implementation boundaries

- Go remains the policy-engine implementation runtime. For Python policy work,
  the release carries CPython `3.12.13+20260728` from
  python-build-standalone, PyPA `packaging` `26.3`, and Vulture `2.16`.
  `packaging` comes from one exact hash-verified wheel represented by the
  policy-owned `pyproject.toml` and frozen `uv.lock`; the carried runtime has no
  pip or ensurepip. Code Polishy never executes target Python to discover
  imports, select a project, or perform dead-code analysis.
- Python fact analysis and Vulture pass their embedded policy program as the
  first standard-input record. The isolated interpreter receives a fixed small
  bootstrap argument; its program record is a JSON string bounded to 1 MiB,
  including the required terminating newline. Remaining input contains the
  separately bounded analysis request. Truncated and oversized program records
  fail before execution. Target source remains analysis data.
- The repository boundary builds one validated Python project inventory from
  contained `pyproject.toml` files and reuses it for dependency, quality, and
  architecture work. Bounded `python-facts/v3` requests use CPython 3.12
  `tomllib`, `tokenize`, and `ast` plus the carried `packaging` release.
  Complete source files are partitioned deterministically. Compact type facts
  resolve TypedDict reads, Pydantic members, and module imports across the validated union through
  `python-type-project/v3`, and
  Vulture uses the same parser, AST extractor, and semantic resolvers. Consumer
  target resolution also accepts bounded compact records through
  `python-reachability-project/v1`. Independent object-import resolution uses
  `python-object-import-project/v1` over the same compact facts and only declared
  registry inputs. It bounds the registry header and each source record to
  16 MiB, the compact project to 256 MiB, resolution depth to 128, and loader
  binding visits to two million. The graph binds separate identities for the
  normalized facts, partition records, and combined type and object-import
  resolution, including current registry bytes and exact source coverage. Compact call facts
  retain lexical scopes, argument and keyword shapes, UTF-8 byte spans,
  assignment-to-call locations, direct statement positions, and rejecting
  guards. Loader evidence also records bounded collections and conditional
  choices, lexical branch identities, binding activation sites, type-only
  guards, and the canonical call AST shape. Loader aliases and supporting
  calls resolve across the entire compact project. Each argument's canonical text is bounded to
  64 KiB; malformed call evidence fails project validation. Its
  project, direct `src`, and in-tree PEP 517 backend roots are passed explicitly
  to consumers. A project-local `.venv` is passed only to `ty` when dependencies
  require it; Vulture always uses carried CPython and its pinned built-in
  whitelists. Ambient Python paths and environments are not tool provenance.
- `python-runtime-check-project/v1` resolves exact runtime checks over the same
  compact project union. It binds the loader and check to current source spans,
  traces consecutive local assignments and aliases, and resolves governed
  runtime classes and protocols through project imports and re-exports.
  Supported checks are rejecting `isinstance` or `issubclass` guards and direct
  synchronous validators whose body performs one such guard. Annotations,
  ignored booleans, another checked value, uses or exits before the check,
  exception suppression, and asynchronous or generator validators provide no
  evidence. Data protocols cannot satisfy a class check; unknown metaclasses,
  decorators, and external bases remain unsupported. Header, source-record,
  response, and aggregate limits match the object-import transport, and value
  and inheritance resolution are bounded to 128 steps. Exact source coverage,
  request identity, call spans, and loaded-value traces are checked at the Go
  boundary. This query executes no target code and establishes no input grammar,
  namespace ownership, or dependency admission by itself.
- Python manifests and `uv.lock` are target-owned inputs. Exact Git repository
  and commit facts remain source facts, not a fabricated PyPI age or
  vulnerability result when registry evidence is unavailable.
  External plug-in dependency facts snapshot the current contained manifest
  and its adjacent `uv.lock`, each bounded to 4 MiB and read as a regular file
  without following symlinks. They retain both byte digests, require a direct
  runtime or optional dependency, and match one authoritative lock package.
  Registry pins compare through the carried `packaging` release's PEP 440
  version normalization; Git pins bind the repository, exact commit, and
  subdirectory. Transitive-only, build-only, development-only, local, moving,
  or mismatched sources cannot acquire external dependency evidence. The
  repository's namespace declaration remains an explicit ownership contract;
  lockfile package names do not establish import namespaces. These facts do
  not replace vulnerability, license, release-age, or dependency-review policy.
- External composition joins independent object-import targets, runtime-check
  evidence, and current direct dependency facts at one exact loader callsite.
  Runtime input can instead supply a proved rejecting grammar and namespace
  guard. Compact function facts recognize its exact predicate expression;
  project resolution verifies its built-in and standard-library references,
  unchanged loader parameter, rejection order, and current predicate source
  digest. The predicate supports normalized Unicode module and object names
  without evaluating application code. Guard lookup and output validation
  index source calls and bindings, and resolution shares the two-million-visit
  object-import work boundary.
  The project resolution identity binds all three inputs; the canonical graph
  retains each successful contract and its proof digests in the separate
  `externalCompositions` collection. JSON, SARIF, saved gate results, and
  architecture packets retain the same evidence. Local graph traversal and
  module permissions do not consume these entries. Review topology retains
  the semantic dependency and runtime contract without proof-only digest or
  source-position changes. No composition declaration creates Vulture
  reachability evidence or authorizes another loader callsite.
- GitHub Actions structure comes only from the bounded `workflow-facts/v1`
  adapter over actionlint `v1.7.12`. Static workflow facts establish checked-in
  triggers, schedules, reachability, commands, and full action pins; provider
  APIs remain responsible for branch protection, enabled schedules, and recent
  successful runs.
- SPDX identifiers and exceptions come only from the embedded
  license-list-data `v3.28.0` snapshot at commit
  `c4a7237ec8f4654e867546f9f409749300f1bf4c`, whose source files and manifest
  are digest-reconciled before policy uses them.
- Generic JavaScript quality checks run through Code Polishy's sealed bundle,
  independent of target-local development dependencies.
- Target-specific commands, paths, external inputs, and exceptions live in the
  target repository's `.code-polishy.json`.
- Third-party tools and packages remain under their own licenses; their origins
  and complete dependency inventories are recorded alongside the release.

## Extension rule

New conditional modules and project-specific providers must preserve exact
inputs, fail-closed coverage, deterministic parsing, and native diagnostic
output. Add a tool only when it can be pinned, verified, and represented in the
release inventory.
