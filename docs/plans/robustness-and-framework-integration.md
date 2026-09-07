# Code Polishy robustness and framework integration

Status: proposed implementation plan. No implementation or release is authorized by this document.

## Outcome and boundaries

Improve deterministic detection of code defects, remove confirmed false positives, and make incomplete analysis visible. Extend the existing pack system so separately installed providers can analyze frameworks and languages without adding their semantics or tool dependencies to the core release. Use the existing Astro project as one acceptance case, while preserving Code Polishy's policy, tool identity, and evidence guarantees.

Each implementation change must name a reproducible failure or missing check, its owning code, and an observable acceptance test. An AI may help author code and review a design; ordinary analysis, provider selection, coverage acceptance, exceptions, and gate outcomes must remain deterministic. Existing explicitly selected human or agent reviews retain their current authority. This work introduces no new mandatory AI judgment.

The development branch is `beta`. Stable releases and other repositories' exact locks remain independent. Periodic upstream integration uses a verified release commit. This plan does not publish a beta, change another repository, or change the stable release process.

## Assessed baseline

The intended implementation baseline is `v0.24.10`, source commit `754d8d1b87b4aad6147368230fc509f8c278f2d1`. The current `beta` source remains based on `v0.24.8`, commit `2f897bc7f3c4df0d270305309ad9a4a8bc66c527`; later planning commits do not integrate 0.24.10. Both source snapshots lock repository governance to Code Polishy `0.24.2`, digest `faff6137fcd6993b4e6779628fce2a68e1c71ca07cd677b303ed5deca84e51df`. Source version and governing release are separate identities.

Version 0.24.9 already adds local execution timings, scope counts, cache diagnostics, bounded focused Python checks, and type-only cycle handling. Version 0.24.10 changes file-length defaults to a review warning at 1,000 physical lines and a blocking maximum above 2,500; stricter configured limits remain effective. Function-complexity limits have not changed. Preserve these improvements without creating a second implementation.

The earlier `fix/pronunciation-launch-policy-blockers` branch contains Astro and runner-label changes absent from 0.24.10. Inspect the individual fixes and their regression cases against the new baseline; do not merge that older implementation wholesale.

The three reports under `docs/issues/` are historical evidence, including observations from other repositories and older releases. Their presence does not prove every reported defect still reproduces. The broader language-pack plans describe proposed capabilities, not current APIs or requirements for this delivery.

## Current architecture to preserve

| Responsibility                 | Current owner and implication                                                                                                                                                                                      |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Policy and repository scope    | Go policy/repository code owns module direction, classifications, required checks, exact exceptions, and selected paths.                                                                                           |
| JavaScript/TypeScript analysis | A sealed tool bundle translates analysis into validated facts. Its ESLint configuration is policy-owned; project ESLint configuration cannot replace enforcement.                                                  |
| Architecture                   | The engine consumes source-graph evidence and enforces ownership, dependency direction, and cycles. A framework adapter supplies facts, not a replacement architecture policy.                                     |
| Community packs                | Protocol v1 accepts contained executable adapters and validates bounded requests/responses. Built-in analysis still dispatches directly. Installing a pack does not replace those built-in providers.              |
| Evidence and execution         | Existing runners, findings, reports, identities, receipts, and test selection remain authoritative. A successful process exit alone is not successful analysis.                                                    |
| Repository development         | Locked workflow guidance, current design-context, exact dependency pins, narrow behavior tests, and the configured final gate govern changes. `verification.finalGateOwner` is `local` in the assessed repository. |

## Delivery sequence

### 1. Establish the implementation baseline

Before source implementation, restore the exact governing release if necessary, read its `agent-workflows`, and capture the original implementation request at the clean task base through the supported intent-capture command. This planning-only change does not itself require non-documentation intent capture.

Integrate `v0.24.10` into `beta` while preserving unrelated work. The local `main` branch is older than `origin/main`; resolve the explicit release commit rather than assuming local `main` is current. A clean integration requires no tests. A manually resolved source conflict requires one affected exact test; a prose-only conflict follows the Markdown rule.

Record the resulting source base separately from the governing lock. Installations and future prerelease versions must preserve exact per-repository release identity. Publishing and changing downstream locks are separate caller-authorized delivery operations.

Acceptance: the implementation base is explicit, the governing launcher resolves the exact lock, and unrelated files and release selections are preserved.

### 2. Repair confirmed analyzer integration defects

Use reduced temporary fixtures derived from the reported behavior. Preserve a valid example and a real violation for every repair. If a report does not reproduce, retain that status and investigate before adding new policy or compatibility paths.

| Work item                         | Owning boundary to inspect                                                  | Required behavior                                                                                                                                                                                                                                                           |
| --------------------------------- | --------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Local CSS imports                 | `tools/javascript/imports.mjs`, `internal/architecture/javascript_graph.go` | Resolve supported asset imports to actual contained targets. Missing files, escaping paths, and applicable module violations remain findings. Do not accept arbitrary unresolved imports.                                                                                   |
| Declared runner labels            | `internal/workflow/facts.go` and its caller                                 | Read the contained supported actionlint label declaration and supply it to semantic validation. Accept declared labels, reject undeclared labels, and diagnose malformed declarations while retaining workflow security rules.                                              |
| Runtime-provided Electron imports | `internal/architecture/javascript.go` and Node package inventory            | Recognize the precise runtime/source boundary using verifiable project metadata, or an exact reviewed declaration only when metadata is insufficient. An Electron dependency must not exempt arbitrary shipped source, browser code, or unrelated development dependencies. |

For CSS, explicitly distinguish asset existence from executable-source analysis. Repair resolution and canonical-graph target handling together: a resolved local stylesheet currently still falls outside the executable graph. Keep the target available to dependency/ownership checks; do not invent executable coverage for stylesheet bytes. If import-fact shape or kind changes, update its strict Go validator in `internal/javascript/imports.go` in the same coherent change. Assert that a prohibited cross-module CSS import produces `architecture.moduleDependency`, while a missing stylesheet remains an import-coverage failure.

For runner labels, cover all consumers in `internal/supplychain/supplychain.go`, `internal/supplychain/monitoring.go`, and `internal/engine/final_gate_owner.go`, including generated-workflow validation. Keep `workflow` independent of `repository`; callers can supply contained bytes or a bounded reader callback. Reuse the earlier configuration validation where appropriate. Accept the supported YAML/YML location, reject ambiguous dual configuration, bound exact non-glob labels, and reject diagnostic-suppression settings. Tests must also show that declared labels do not cause false missing-monitoring or missing-gate findings.

For Electron, establish main/preload/renderer semantics from the actual project before choosing a representation. A package name in `devDependencies` or a `main` path alone does not prove every importing source has Electron runtime access. Prefer verifiable manifest, entry-point, and source facts; retain findings for ambiguous roles, unrelated development dependencies, and invalid renderer imports. Do not introduce a user-configurable runtime-package allowlist. An exact new declaration is considered only if a reproduced case cannot be represented by existing facts, with a separate bounded design justification. This stage does not create a general runtime-contract language.

Acceptance: each confirmed defect has an executable regression that fails on the appropriate pre-fix base and passes with the repair, plus checks showing nearby invalid cases still fail. Use the existing proof workflow when selected by governing rules; do not introduce mandatory review or proof machinery for every test.

### 3. Make the existing pack boundary sufficient for real analysis

Evolve the installed pack manifest and protocol in place. A pack is the distribution and exact tool identity; its adapters implement analysis operations. A JS/TS provider can contain multiple framework adapters, just as its current implementation covers React. There is no requirement for a separate Astro pack or framework-specific project configuration. The core gains a general provider contract, with no new Astro-specific dispatch, policy, parser, or bundled tool requirement.

General provider participation is a required outcome of this delivery. It includes the routing, policy inputs, facts, coverage, and runtime identity needed by a real provider. It does not require extracting every built-in language or implementing several new toolchains. Keep existing native implementations and adapt their selection boundaries; installable packaging can remain unchanged for those implementations.

#### One authoritative analysis plan

Resolve a deterministic owner for every selected file, applicable capability, and execution profile before running analysis. Use the governed inventory, exact selected pack manifests, and existing policy configuration. Distinguish language classification from provider ownership. An extension match or installed dependency is not evidence of successful analysis.

An explicit pack claim replaces the corresponding native route. Two selected packs claiming the same work fail with a provider conflict. Missing, invalid, or failed selected providers never trigger a hidden native fallback. Unowned required work produces coverage findings. Native defaults remain available only for work the resolution step assigns to them.

Use this plan for quality checking, format writes, comments, architecture facts, tool prerequisites, coverage reporting, and applicable command/profile selection. Updating a few JS input filters alone is insufficient. Module coverage must be derived from actual owned paths and required profiles; assigning a command to a module must not credit unexamined files elsewhere in that module. The current pack-to-command compilation and module-based coverage accounting need reconciliation at this boundary.

Project-wide checks may need an entire TypeScript program, workspace, or compilation unit. Resolve that scope coherently, rather than pretending independent file claims isolate type checking or reachability. A provider may own a broader JS/TS project containing framework files. Separate selected diagnostic/write targets from necessary read context. Context must have contained paths and verified input identities; reading it does not authorize formatting it or reporting unrelated findings. Preserve the existing command's documented full or focused reporting scope.

Keep exact pack selection in the existing `packs` configuration. Provider declarations describe supported inputs and operations. Read package manifests, lockfiles, and existing project configuration for ecosystem facts; do not create a second project fragment. If a real workspace needs an explicit scope restriction, add only that exact provider binding to the existing configuration, rather than duplicating dependency or framework facts. Metadata does not select, install, or authorize arbitrary executable tooling.

#### A bounded, policy-preserving protocol

Design one coherent successor to protocol v1. Existing fields for operation, capability, project root, selected files, modules, mode, and profile remain useful. The following additions are required before an external provider can satisfy applicable baseline checks:

| Contract element          | Required behavior                                                                                                                                                                                                                                                                |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Effective policy inputs   | Pass the locked baseline and effective stricter settings, including relevant rule policy, complexity limits, generated-source treatment, and comment/directive rules. The provider cannot substitute target lint configuration or lower these requirements.                      |
| Selected work and context | Distinguish analyzed targets from bounded project context; account for the exact project and configuration inputs used. Validate duplicate, unselected, escaping, and changed inputs.                                                                                            |
| Coverage                  | For each requested capability, account for every selected path exactly once as analyzed or unsupported with a reason. A missing entry, false capability claim, contradictory result, or unsupported required input cannot produce a clean check.                                 |
| Findings                  | Preserve a stable provider rule identifier, capability, original-source path and location, subject, and message. Namespace provider diagnostics deterministically; reserve core policy rule identities for core decisions. Keep exact exceptions and their existing constraints. |
| Source facts              | Supply resolved dependencies and typed edges to the common graph, plus source comments/directives and metrics where core policy must decide the outcome. Findings are not a substitute for facts needed by other checks.                                                         |
| Execution identity        | Bind the response and reusable evidence to the exact pack, tool/runtime, protocol, effective policy, selected scope, and relevant input identities. Preserve bounded execution and distinguish operational failure from code findings.                                           |

Successful coverage is a validated claim by a trusted analyzer, not mathematical proof that its implementation is correct. Conformance must exercise real valid and invalid source, ignored inputs, missing evidence, policy thresholds, and failure handling. Existing fixtures that compare only response status are insufficient; an operational failure cannot stand in for detection of a seeded code defect.

Keep source-map implementation inside the provider. Require diagnostics and facts to refer to original governed paths with validated coordinate conventions and locations. If mapping is unavailable or ambiguous, report incomplete analysis rather than invented coordinates. Comment facts need enough lexical and syntactic information for the policy-owned directive rules; a provider-supplied `allowed` flag or generic `kind: directive` does not independently establish permission. Baseline and comment participation are prerequisites whenever those checks apply, not optional later enhancements.

Reuse the existing source graph and its dependency/cycle decisions. The engine derives module ownership, generated/test classification, and acceptance from policy. Providers supply source interpretation and resolution evidence, including dependencies into native-analyzed files. Do not require generic consumers to import the JS-specific fact types.

Generalizing graph evidence requires more than allowing a new analyzer string. In 0.24.10, node language and edge ecosystem validation enumerate built-ins; fact inputs require Python-specific project manifests, paths, and analyzer protocols. Replace those assumptions with validated provider declarations and appropriately bound project/input identities. Retain containment, endpoint ownership, canonical ordering, deterministic graph identity, and evidence validation. Preserve existing native semantics. Keep shared fact types below their consumers without creating a dependency cycle between pack execution and architecture policy.

#### Exact tooling without framework dependencies in core

Let a pack declare its executable entry and any required policy-owned runtime. Resolve such a runtime through the locked installation and pass only its verified absolute executable and exact identity under the existing sealed environment. The core selects and verifies it; a target-provided path, ambient `PATH`, version-range match, or provenance string is insufficient. A runtime not available with the required identity produces a clear operational failure.

The first runtime reference can reuse the already pinned Node installation. The mechanism describes runtime identity, not framework names. Packs retain their own exactly pinned analyzer dependencies and integrity-checked installed tree. Do not expose the entire internal JS tool bundle as an undocumented import API, or treat a runtime reference as permission to load target compiler plugins or execute repository check scripts. Contained native executables remain viable without a host runtime reference.

Measure the actual provider tree, per-file sizes, response size, and verification cost before finalizing packaging. Current pack limits are 128 MiB per tree, 16 MiB per file, and 1 MiB per response. Sharing Node does not prove a provider's remaining dependencies fit those limits. Change only a bound demonstrated inadequate by the concrete acceptance case, with bounded validation and resource tests; do not add a general artifact service or toolchain downloader.

Publish a coherent manifest/protocol cutover. Update affected fixtures, callers, examples, and documentation together; reject unsupported protocol versions clearly. Do not add a degraded v1 path or other compatibility machinery without an explicit compatibility requirement. Private implementation steps may be incremental, but the exposed contract and native/provider ownership must agree at the completed boundary.

Acceptance: removing, conflicting, breaking, or omitting a selected provider changes deterministic ownership or coverage results appropriately. Required policy cannot be weakened through provider configuration. Existing native checks keep their observable behavior for their assigned work. No engine path independently claims success outside the shared plan.

### 4. Prove the contract with real source

Use two bounded proofs. First, create a small provider test that exercises routing, malformed output, conflicts, and graph integration without built-in language assumptions. Then use one real language outside native Go/JS/Python/shell analysis for one meaningful operation through a pinned analyzer, with valid and invalid source. This is contract evidence, not a new shipped language pack or a claim of complete language support. Missing other required capabilities must remain visible. A made-up fixture language alone is not sufficient evidence that real compiler integration works.

Use the existing Astro project for the substantial framework acceptance case. Record its actual revision, package manager, dependency lock, build/type configurations, and applicable required checks. A script named `check`, an installed package, or a recognized extension does not establish coverage. Maintain a capability inventory for formatting, syntax/linting, typing, complexity, comments/directives, imports/architecture, and dead code.

Implement the necessary framework interpretation in a separately selected provider, preferably as an adapter within an appropriate JS/TS provider. Package metadata and existing configuration supply factual context. There is no mandatory standalone Astro pack, Astro-specific core configuration, or Astro tooling added to the core bundle. Reuse earlier compiler/import/source-mapping behavior only after checking it against the actual project and current contracts.

The 0.24.10 source does not provide complete framework coverage. Some paths already fail closed, while others can skip unrecognized source extensions. For example, dead-code inventory explicitly reports unsupported TypeScript-classified files when that analysis is selected. Establish full and focused behavior per capability instead of claiming every framework check silently passes or every recognized extension is supported.

| Acceptance case                                                    | Required result                                                                                                                           |
| ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------- |
| No selected provider, or selected provider unavailable             | Every affected required capability remains visibly incomplete; no implicit native substitute claims success.                              |
| Valid authored source and real syntax/type/rule violations         | Applicable checks analyze valid inputs; violations produce stable findings at original locations.                                         |
| Authored and compiler-generated imports                            | Actual dependencies retain normal checks; generated compiler artifacts do not become false authored dependencies.                         |
| Imports across provider/native boundaries                          | The canonical graph contains the real edge; prohibited module direction still produces `architecture.moduleDependency`.                   |
| Literal route filenames, data, and generated output                | Exact paths are preserved, data bytes remain intact, and generated executable source retains applicable checks.                           |
| Missing tool, ignored input, crash, timeout, or malformed response | Required analysis is incomplete or operationally failed, never a clean check.                                                             |
| Full and focused runs, including formatting                        | Coverage matches the work performed; context does not become authority to rewrite unrelated files.                                        |
| Provider selection changes                                         | The same core binary and policy work throughout; adding support requires provider changes rather than framework-specific engine branches. |

Use reduced fixtures for permanent regressions and the real project for acceptance, recording its exact installed prerelease/provider identities. A compiler transform alone does not establish complete formatting, template, type, comment, complexity, or dead-code support. Stop a capability's acceptance when its actual tool or input semantics are unresolved; retain incomplete coverage and do not label the project fully supported.

Stop expanding the mechanism once these cases work. Additional language implementations, extraction of current built-ins, pack dependency resolution, registries, scaffolding, and a universal build-system model remain separate work. If the real acceptance case exposes a missing general contract requirement, fix that bounded requirement; do not work around it with a privileged framework path.

### 5. Address bounded workflow friction after correctness

These are deferred follow-ups. Schedule one only after a reproduced interaction warrants it; none is a prerequisite for the provider contract:

- Permit design-context lookup for an uncreated path through existing ownership mappings, with containment and ambiguity checks. Do not expand normal source selection to accept nonexistent files.
- Give a direct recovery suggestion for close invalid options such as `--modules`, without silently accepting a different command.
- Explain a surprising test selection from the actual selection/dependency decisions. Reuse the existing plan/report model; do not build a parallel scheduler or narrative generated by an AI.

The invalid gate-identity report needs its original input or a reduced reproduction before a fix is scheduled. Generated/test ownership reports need case-by-case confirmation. Broad session-resume redesign, complexity-policy changes, and automated assessment of complete product acceptance are deferred. Bad test observers and incomplete agent follow-through remain engineering responsibilities; improve the affected tests or workflow rather than adding a new acceptance oracle.

Acceptance: each chosen improvement has a concrete before/after interaction and a narrow deterministic boundary test. No new mandatory review stage is introduced.

## Product boundary

Code Polishy's responsibility is to analyze and polish code, identify defects, and report trustworthy check results. Application observability belongs with established libraries and services. Encourage projects to use those tools for dashboards, remote telemetry, aggregate analytics, and tracing. These capabilities are outside Code Polishy's product scope; this plan contains no observability implementation, exporter, integration-hook, or future-phase work.

Local check findings, failure diagnostics, and execution evidence remain part of reliable code analysis. Reuse the existing reports to explain what was checked and why analysis failed or remained incomplete. A tool exit and the validated semantic result remain distinct. Missing or malformed required analysis evidence still fails closed.

This delivery also excludes a new event journal, a report-explorer product, a new AI acceptance system, complete extraction of built-in languages, a marketplace/catalog, a generalized pack lifecycle/scaffolder, a sandbox program, and a broad performance or complexity-policy redesign.

Earlier combined language-pack and observability proposals do not define this roadmap. The provider work above is the selected code-analysis scope. New requirements must directly improve detection, correctness, or reliable enforcement of code-quality checks.

## Verification and repository delivery

Follow the exact locked `agent-workflows` throughout. Before each governed source boundary, retrieve current design-context for the exact paths/modules and read its returned current documents. Update mapped rationale only when a consequential design decision changes. Preserve dependency direction and `quality.allowComments`; keep non-local rationale in mapped design documents.

Pin direct dependencies and package-manager versions exactly. Normal setup uses frozen locks. A dependency update first generates its candidate lock without lifecycle scripts, runs `code-polishy dependency-review --base REF` against the explicit integration target, and only then installs the accepted candidate.

Use the smallest behavior-focused exact suite after a coherent runnable change. Tests use temporary state and demonstrate actual violations, successful analysis, scope, or preserved bytes. Avoid filename-presence tests, implementation mirrors, fabricated usage, blanket exclusions, and pass-with-no-tests acceptance.

Choose broader verification from the governing event and current repository configuration. For a completed source boundary, use the required checkpoint/changed-scope workflow without duplicating an immediately following final gate. For the final source candidate, the currently configured owner runs one local base-aware merge gate; re-read that ownership after upstream integration. Supplemental suites run only when an explicit request, checked-in event workflow, or the locked release checklist selects them. Do not add automatic AI review or independent duplicate gates.

For this ordinary Markdown plan and its advisory review record, run `code-polishy format --git-changes`, fix task-owned findings, and skip application tests. Preserve the unrelated issue reports. Commit completed task-owned documents after the required checks. Source implementation, upstream integration, publication, and downstream upgrades are separate from this planning delivery.

Completion of an implementation slice means its stated defect and regression cases are resolved. Completion of general provider support requires the external-language proof and the actual framework project's recorded applicable coverage. A passed checkpoint must not be presented as broader product acceptance.
