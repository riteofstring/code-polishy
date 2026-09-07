# Code Polishy robustness and framework integration

Status: proposed implementation plan. No implementation or release is authorized by this document.

## Outcome and boundaries

Improve deterministic detection of code defects, remove confirmed false positives, and make incomplete analysis visible. Use an existing Astro project to prove framework integration while preserving Code Polishy's policy, tool identity, and evidence guarantees.

Each implementation change must name a reproducible failure or missing check, its owning code, and an observable acceptance test. An AI may help author code and review a design; ordinary analysis, provider selection, coverage acceptance, exceptions, and gate outcomes must remain deterministic. Existing explicitly selected human or agent reviews retain their current authority. This work introduces no new mandatory AI judgment.

The development branch is `beta`. Stable releases and other repositories' exact locks remain independent. Periodic upstream integration uses a verified release commit. This plan does not publish a beta, change another repository, or change the stable release process.

## Assessed baseline

The intended implementation baseline is `v0.24.10`, source commit `754d8d1b87b4aad6147368230fc509f8c278f2d1`. At drafting, `beta` is still at `v0.24.8`, commit `2f897bc7f3c4df0d270305309ad9a4a8bc66c527`. Both source snapshots lock repository governance to Code Polishy `0.24.2`, digest `faff6137fcd6993b4e6779628fce2a68e1c71ca07cd677b303ed5deca84e51df`. Source version and governing release are separate identities.

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

### 3. Prove framework coverage in the existing Astro project

Record the actual project revision, package manager, dependency lock, build/type configurations, and required checks before making integration decisions. Read existing package metadata and configuration as inputs. A script named `check`, an installed package, or a recognized extension does not establish analysis coverage. Do not add a duplicate project fragment or infer successful coverage with an AI.

Build a concise capability inventory from actual behavior: formatting, parsing/linting, type checking, complexity where applicable, source comments/directives, imports/architecture, and dead-code analysis. Record unsupported or incomplete capabilities explicitly. A project-wide clean result requires all applicable required capabilities; focused checks must describe their bounded scope.

The 0.24.10 baseline recognizes `.astro` as TypeScript but cannot supply its full analysis. Import and comment paths report incomplete coverage; the lint/complexity grouping silently skips extensions outside its JS/TS source set. The acceptance inventory must expose this silent omission and require either actual analysis or an explicit coverage failure. Recognizing `.vue` or `.svelte` similarly is not evidence that those frameworks are completely supported.

Reassess the earlier Astro implementation for authored imports versus compiler-generated imports, source-location mapping, literal route filenames such as `[slug].astro`, JSON data modules, and generated-source handling. Reuse verified behavior through the current JS/TS tooling contracts. Do not assume the earlier branch proves complete Astro type or template coverage.

The proposed beta implementation puts Astro integration in the existing sealed JS/TS bundle. Pin the selected compiler and source-mapping dependencies exactly, including `@astrojs/compiler` and `@jridgewell/trace-mapping` when reusing the earlier implementation. Identify any additional tool needed for a required capability before claiming that capability works; a compiler transform alone does not establish formatting, type, template, comment, or complexity coverage. Project facts select the applicable analysis, while the complete bundle keeps its existing provenance and integrity checks.

This choice adds Astro tooling to that beta's JS/TS bundle, including installations used in non-Astro projects. It is the bounded packaging tradeoff recommended for the experiment. It creates no standalone Astro pack, target-supplied analyzer injection, optional-bundle loader, or partial-provenance mode. Stable distribution and independently removable framework tooling remain future decisions. If carrying these pinned tools is unacceptable, defer the Astro integration milestone and retain explicit incomplete coverage; do not replace this decision with an implicit platform project.

Keep compiler and framework semantics inside the JS/TS implementation. Update its language-specific Go adapters where necessary, while keeping generic selection, graph policy, reports, and gates free of Astro-specific decisions.

| Acceptance case                                                    | Required result                                                                                                               |
| ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------- |
| Valid authored Astro source                                        | The applicable checks analyze it successfully with known tool/configuration identity.                                         |
| Real syntax, type, or rule violation                               | A stable finding points to the original source and correct location; required unsupported analysis cannot pass.               |
| Authored and compiler-generated imports                            | Actual dependencies retain their checks; compiler artifacts do not create false authored dependencies.                        |
| Literal route filenames                                            | The exact selected file is analyzed; brackets are not expanded as glob syntax.                                                |
| Missing import or prohibited module edge                           | The defect remains visible through the normal architecture checks.                                                            |
| Parse-only data and generated output                               | Data bytes are preserved; generated executable source retains its applicable checks.                                          |
| Missing tool, ignored input, crash, timeout, or malformed response | Analysis is explicitly incomplete or operationally failed, never accepted as a clean check.                                   |
| Full and focused selection                                         | Reported coverage matches what actually ran; necessary project context does not become permission to rewrite unrelated files. |

Use the real project as acceptance evidence and reduced fixtures as permanent regressions. Record the exact installed beta identity during downstream verification. Passing isolated fixtures does not certify the complete project.

### 4. Preserve extension boundaries without expanding the delivery

Stage 3 uses the current JS/TS fact boundary. Refactor a private helper only when a concrete acceptance case requires it, recording the smallest change and its effect on existing analysis. Returning source locations, imports, and coverage through existing validated models remains part of that integration.

The following are future provider-contract concerns, not scheduled implementation in this delivery:

- Deterministic provider ownership for an affected file and capability, including rejection of conflicts and missing providers. Built-in extension recognition must not silently override an explicitly supported provider.
- Passing required policy settings and necessary project context without delegating policy authority to repository scripts or configurations.
- Returning original-source locations, import facts, and explicit successful/incomplete coverage through the existing finding and source-graph models.
- Binding any optional tool/provider to an exact identity and retaining the existing bounded execution and integrity requirements.

Protocol v1's evidence strings do not prove structured coverage, and its finding wrapper cannot represent every native rule identity or graph fact. Do not claim that a thin executable wrapper supplies complete language support. Optional framework tooling and replacement of built-in providers would require a separate bounded proposal with exact inputs, outputs, affected callers, failure cases, and a coherent cutover. If that work becomes necessary, report the unmet requirement and defer it instead of expanding this implementation.

Keep new implementation helpers separate from generic engine policy so a future Rust, Java, Zig, or C provider can supply its own interpretation. Do not add those implementations, an ecosystem allowlist, a universal build-system model, or speculative discovery modes. This delivery improves the JS/TS integration; it does not claim that the current public pack interface is ready to express arbitrary complete language providers.

Acceptance: Astro uses existing engine guarantees and the sealed JS/TS provider; no new generic engine branch encodes Astro semantics. No optional-tooling or public pack-protocol mechanism is introduced. Any changed existing contract has one coherent final form and no obsolete fallback or transitional API unless explicitly requested.

### 5. Address bounded workflow friction after correctness

These are deferred follow-ups. Schedule one only after a reproduced interaction warrants it; none is a prerequisite for Astro support:

- Permit design-context lookup for an uncreated path through existing ownership mappings, with containment and ambiguity checks. Do not expand normal source selection to accept nonexistent files.
- Give a direct recovery suggestion for close invalid options such as `--modules`, without silently accepting a different command.
- Explain a surprising test selection from the actual selection/dependency decisions. Reuse the existing plan/report model; do not build a parallel scheduler or narrative generated by an AI.

The invalid gate-identity report needs its original input or a reduced reproduction before a fix is scheduled. Generated/test ownership reports need case-by-case confirmation. Broad session-resume redesign, complexity-policy changes, and automated assessment of complete product acceptance are deferred. Bad test observers and incomplete agent follow-through remain engineering responsibilities; improve the affected tests or workflow rather than adding a new acceptance oracle.

Acceptance: each chosen improvement has a concrete before/after interaction and a narrow deterministic boundary test. No new mandatory review stage is introduced.

## Diagnostics and explicit non-goals

Retain 0.24.9's local diagnostic records and improve only concrete failure paths that prevent diagnosis or misrepresent analysis. A tool exit and the validated semantic result remain distinct. Missing or malformed required evidence still fails closed.

Do not weaken strict response validation merely because a field is called telemetry. The new Vulture timing requirement deserves examination only if a concrete failure or unnecessary coupling is demonstrated; it is not a confirmed regression and is not scheduled work here.

This delivery excludes dashboards, remote telemetry, aggregate analytics, OpenTelemetry exporters or speculative hooks, a new event journal, a report-explorer product, and a new AI acceptance system. It also excludes complete extraction of built-in languages, a marketplace/catalog, a generalized pack lifecycle/scaffolder, a sandbox program, and a broad performance or complexity-policy redesign.

The existing language-pack and observability plans remain broader proposals. This plan does not activate their phases or completion criteria. New requirements enter this delivery only when tied to a reproduced failure or missing required check and explicitly reconciled with its scope.

## Verification and repository delivery

Follow the exact locked `agent-workflows` throughout. Before each governed source boundary, retrieve current design-context for the exact paths/modules and read its returned current documents. Update mapped rationale only when a consequential design decision changes. Preserve dependency direction and `quality.allowComments`; keep non-local rationale in mapped design documents.

Pin direct dependencies and package-manager versions exactly. Normal setup uses frozen locks. A dependency update first generates its candidate lock without lifecycle scripts, runs `code-polishy dependency-review --base REF` against the explicit integration target, and only then installs the accepted candidate.

Use the smallest behavior-focused exact suite after a coherent runnable change. Tests use temporary state and demonstrate actual violations, successful analysis, scope, or preserved bytes. Avoid filename-presence tests, implementation mirrors, fabricated usage, blanket exclusions, and pass-with-no-tests acceptance.

Choose broader verification from the governing event and current repository configuration. For a completed source boundary, use the required checkpoint/changed-scope workflow without duplicating an immediately following final gate. For the final source candidate, the currently configured owner runs one local base-aware merge gate; re-read that ownership after upstream integration. Supplemental suites run only when an explicit request, checked-in event workflow, or the locked release checklist selects them. Do not add automatic AI review or independent duplicate gates.

For this ordinary Markdown plan and its advisory review record, run `code-polishy format --git-changes`, fix task-owned findings, and skip application tests. Preserve the unrelated issue reports. Commit completed task-owned documents after the required checks. Source implementation, upstream integration, publication, and downstream upgrades are separate from this planning delivery.

Completion of an implementation slice means its stated defect and regression cases are resolved. Completion of the Astro milestone additionally requires the actual project's recorded applicable coverage. A passed checkpoint must not be presented as broader product acceptance.
