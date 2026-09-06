# Language packs and observability: readiness and delivery plan

Status: assessment and proposed plan

Assessment date: 2026-09-06

## Assessment boundary

The primary source is upstream `main` at
`436c7a9855f030874878263a89807f51f402bf89`, also the commit selected by tag
`v0.24.6`. Its source version is 0.24.6 and its repository governance lock selects
0.24.2. Those are distinct identities: the version being developed and the
installed release governing work on that version.

The caller's checkout was on `fix/pronunciation-launch-policy-blockers` at
`e5fea6a9103d3923b801d9ea8e736f99059f7685`, declaring source version 0.23.1 and
locked to 0.23.0. After fetching upstream, the branches had seven checkout-only
commits and 37 upstream-only commits. The checkout contains Astro fixes absent
from this upstream snapshot. Neither branch was switched, merged, or rebased
for this assessment.

This is a source and checked-in contract assessment. Implementation, tests,
schemas, documentation, release scripts, and CI configuration were inspected.
Test presence is evidence of intended coverage, not a fresh passing result.
Application tests, performance probes, external pack installation, and live CI
run verification were not performed. Unpublished cofounder work is outside this
snapshot. Repository evidence links below pin the assessed upstream commit.

## Executive assessment

Code Polishy already has working community-pack plumbing, including executable
adapters. It does not yet have independently removable implementations of its
built-in languages. The remaining work changes ownership across discovery,
analysis, policy configuration, reporting, tool installation, and release
packaging. It is a substantial staged program, not a missing adapter interface.

The desired decentralized model fits the existing local-directory installer.
Authors can publish repositories; an AI can obtain a reviewed pack, verify it,
install it, and record its exact selection. No marketplace is required.
Reproducible acquisition, selection changes, removal, and CLI-assisted pack
creation still need product work. In the target design, projects choose the
languages and frameworks they need. The core accepts conforming providers
without a built-in support allowlist or prescribed language/framework bundles.

Observability is strongest at explaining findings and reconstructing governed
gate commands. Version 0.24 substantially improves that foundation with managed
canonical reports, JSON, SARIF, scope evidence, and remediation. A broad claim
of complete runtime observability would be premature: startup failures,
internal analysis operations, pack evidence, interruptions, and CI retention
have material gaps.

Deliver this in two phases. First, build the generic pack contract, CLI creation
workflow, lifecycle, and execution evidence so a developer's AI can add an
unfamiliar ecosystem without changing core. Second, use that same public
workflow to move all currently built-in language support into packs and prove
equivalent behavior. The existing language inventory supplies real acceptance
cases; projects and pack authors continue to choose their own ecosystems and
pack boundaries. Treat observability as part of the first phase's contract.

## 1. What the latest code already provides

| Area               | Source-confirmed state                                                                                                                                   | Implication                                                                                         |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| Policy engine      | Strict configuration schemas, module and test ownership, bounded selection, generated-source policy, exceptions, and capability coverage                 | A substantial governance foundation already exists.                                                 |
| Analysis           | Native Go analysis, sealed JavaScript tooling, Python facts and inference, source dependency graphs, cycle evidence, and architecture review             | Extract existing behavior; do not replace it with shallow tool wrappers.                            |
| Community packs    | Local install, immutable digest-addressed storage, manifest validation, protocol v1 adapters, conformance fixtures, provider checks, and normal findings | Adapters are implemented and integrated today.                                                      |
| AI workflows       | Versioned docs, capability discovery, scoped task-start context, operational handoffs, and behavior/architecture review packets                          | Pack setup should extend these entry points.                                                        |
| Diagnostics        | One finding model, semantic identities, remediation, selection relationships, complete managed reports, JSON and SARIF                                   | Reuse the current report model for packs.                                                           |
| Execution evidence | Gate identities, command attempts, bounded logs, timing, failure categories, test artifacts, and exact receipt reuse                                     | Reuse the current runner and evidence ownership.                                                    |
| Releases and CI    | Native release manifests, dependency/provenance artifacts, Ubuntu/macOS gates, and selected native Windows contracts                                     | Distribution building blocks exist; this is not proof that every language pack works on every host. |

The newer Python surface is especially important. Version 0.24.5 adds pinned
Astroid inference and repository-owned runtime contracts. Version 0.24.6 adds
operator-controlled loader boundaries and explicit delegated architecture
authority for unknown runtime targets. A Python extraction based on the older
Ruff/Vulture/ty inventory alone would omit current behavior.

The separate Astro branch also matters. Its authored-import, literal-filename,
compiler, and JSON data-module fixes should be reconciled with the agreed
implementation base before freezing parity fixtures for that behavior. These
are existing implementation facts; their current location does not determine
which future pack supplies Astro support. This assessment does not assume they
have already landed upstream.

Evidence: [current changelog](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/CHANGELOG.md),
[capability discovery](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/docs/capabilities.md),
[Python contract design](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/docs/design/python-contract-evidence.md).

## 2. Adapters exist; language extraction does not

There are three different meanings of adapter in the current implementation:

1. **The community-pack adapter protocol:** a contained executable receives
   JSON and returns validated status, findings, or failure.
2. **Built-in language fact adapters:** the JavaScript bundle and Python facts
   processes translate ecosystem-specific analysis into facts consumed by Go.
3. **A complete language provider:** discovery, source interpretation, tools,
   policy behavior, dependency evidence, and packaging owned by an installable
   pack. This last boundary is still proposed.

Protocol v1 is real. It receives selected files, repository modules, capability,
profile, and formatting mode. It rejects malformed responses, unknown fields,
extra JSON, unselected finding paths, and evidence-free success. Exact installed
trees are verified before and after adapter execution. Pack commands enter the
ordinary check profiles, and missing selected packs produce visible findings.

The current lifecycle commands are exactly:

```sh
code-polishy pack verify --source ./pack-directory
code-polishy pack install --source ./pack-directory
code-polishy pack root
```

Installation validates and copies a pack without executing it. Verification
executes fixtures separately. Repository selection remains a manual exact
`name`, `version`, and `digest` entry in `.code-polishy.json`. The newer
`capabilities` command reports selected packs and their capabilities, but it is
not an installed-pack inventory or a remote pack catalog.

There are no lifecycle implementations for `pack remove`, `pack update`,
`pack list`, official download, or repository acquisition. The checked-in sample
is a synthetic marker-checking adapter, not an extracted shell, Python, Go, or
JavaScript implementation.

Evidence: [pack authoring contract](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/docs/adding-a-language.md),
[CLI](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/cmd/code-polishy/pack_meta.go),
[protocol](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/pack/protocol.go),
[sample adapter](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/tools/fixtures/language-pack/bin/adapter).

## 3. The concrete extraction blockers

### 3.1 Native providers still run directly

Quality orchestration explicitly invokes Go, shell, JavaScript, and Python
checks before running configured pack commands. Architecture builds its graph
by directly composing Go, JavaScript, and Python graph producers. Installing a
pack does not transfer ownership of those built-in paths.

Provider-conflict checks compare configured commands when a pack participates.
They do not replace all direct native dispatch. A single authoritative provider
resolution step must eventually govern both planning and execution, including
coverage, formatting, architecture, dependency checks, and doctor.

### 3.2 The pack protocol cannot preserve the current analysis contract

| Missing or incomplete boundary                     | Why it matters                                                                                                                                                 |
| -------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Full inventory separate from active selection      | Changed-file checks need related manifests and project context without treating all context as selected output.                                                |
| Discovery mode and reusable scope identity         | Capabilities need a consistent project interpretation; static parsing and build-system evaluation have different authority.                                    |
| Effective baseline and pack-specific configuration | Current requests contain no policy thresholds or typed language settings. Existing built-in rules must not become pack-selected defaults or disappear.         |
| Rich findings and graph evidence                   | V1 findings collapse into `pack.<capability>` and cannot carry the newer semantic identities, related locations, graph inputs, and review evidence faithfully. |
| Structured successful coverage                     | Nonempty evidence strings do not establish which inputs, rules, or tool versions were actually covered.                                                        |
| Host toolchain and execution policy                | The manifest cannot describe compiler identity, accepted versions, evaluated discovery, network policy, or writable roots.                                     |
| Artifact transport and scale                       | The optional output-directory field is not populated by normal `requestFor`; there is no complete large-analysis transport.                                    |
| Repository restoration                             | Exact selection has no acquisition locator, so another machine knows what is missing but not how to retrieve those exact bytes.                                |

The new generic source graph is useful existing work: it represents source
nodes, typed edges, input identities, and external composition evidence.
Packs should provide validated graph facts into that boundary. They should not
each invent a separate cycle detector, architecture-review protocol, or suite
scheduler. Ecosystem discovery can remain pack-specific while graph evidence
crosses a small common contract.

Preserve stable rule identities and exception behavior during extraction. A
Go violation becoming only `pack.lint` is not equivalent even if both commands
exit unsuccessfully. The engine must validate which rule namespace a provider
owns and attach exact provider identity to its evidence.

### 3.3 There are two different scale problems

Protocol v1 caps requests at 10,000 files and 1,000 modules, responses at 1 MiB,
and findings at 4,096. Separately, installation caps the entire pack at 128 MiB,
individual files at 16 MiB, and file count at 10,000. The installer reads source
file bytes into memory, and runtime integrity checks reread installed files.

Both boundaries need measurement against actual Node, CPython, compiler, and
tool artifacts. Increasing a JSON response limit alone does not make a runtime
bundle installable or inexpensive to verify. The existing source graph permits
much larger evidence than the pack response can carry. Python already has
partitioning and smaller-response retries worth learning from.

### 3.4 Core services currently depend on the JavaScript bundle

Markdown formatting calls the JavaScript formatter. Parse-only data checks call
the bundle's parser. GitLab inspection calls the bundle's GitLab operation.
These remain core responsibilities in the proposed ownership model.

Removing a pack that supplies JavaScript support must not remove core Markdown,
data, or repository-service checks. Give these services explicit core APIs and
independent packaging before removing the present bundle. Evaluate a small
core utility runtime versus native implementations using actual dependencies;
do not preserve the entire language bundle as a hidden fallback.

### 3.5 Integrity is not a sandbox or a correctness proof

Current pack execution provides validated inputs, contained executable
selection, environment restrictions, timeouts, and installation integrity.
It does not establish OS-enforced filesystem or network isolation for arbitrary
pack code. A child process can still attempt its own file access or subprocess
execution. The sample itself invokes absolute system utilities.

The discovery plan's claims that adapters cannot enumerate outside inventory
and can write only approved paths need an enforcement design, or narrower
documented guarantees. Keep executable-pack trust distinct from approval to
evaluate repository-controlled build logic.

Conformance also needs strengthening. Today verification compares declared
response status, and a capability's negative fixture may be an operational
failure. That proves failure handling, not detection of invalid language code.
Production packs need meaningful passing and violating fixtures with expected
rules, scope, coverage, and format-write outcomes. Installation success must
not be presented as proof that those fixtures ran.

Evidence: [native quality dispatch](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/quality/quality.go),
[architecture dispatch](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/architecture/architecture.go),
[source graph](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/architecture/sourcegraph/model.go),
[pack resolution](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/pack/runtime.go),
[installer](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/pack/install.go),
[fixture verifier](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/pack/verify.go).

## 4. Recommended decentralized product model

A pack is an independently owned repository that produces exact installable
artifacts. Its implementation language is independent of the language it
analyzes. Official packs use the same manifest, protocol, conformance, and
runtime authority as community packs.

Pack authors declare supported languages, frameworks, source patterns,
discovery, and capabilities. Projects select providers for their own source and
required checks. The engine validates those declarations and enforces the
generic baseline; it does not choose an approved set of ecosystems. Adding an
unfamiliar language or framework must work through the public pack contract
without an engine release, built-in identifier, or central registration.

For example, a project needing Astro support can select a dedicated pack or a
broader pack whose declared capabilities cover that project. The contract must
support either arrangement. Any cooperation between packs uses explicit,
versioned interfaces and exact dependency identities. Provider ownership stays
unambiguous. Recognizing TypeScript alone does not establish Astro coverage.

Discovery uses selected pack metadata and explicit project classifications.
Unclassified or uncovered source remains visible for resolution; an existing
extension list must not silently define the limit of supported projects.

Keep these concepts separate:

| Concept                  | Recommended meaning                                                                           |
| ------------------------ | --------------------------------------------------------------------------------------------- |
| Source repository        | Where a person or AI obtains source and authoring documentation.                              |
| Acquisition record       | Exact repository commit or artifact URL, package subdirectory, platform, and expected digest. |
| Installed identity       | Immutable name/version/content digest on this machine.                                        |
| Repository selection     | Exact identity required by project policy.                                                    |
| Execution authority      | Exact toolchain and evaluated-discovery permissions, when needed.                             |
| Publisher authentication | Optional configured trust that binds artifact bytes to a publisher; naming alone grants none. |

Use release artifacts for ordinary consumers. A clone/build workflow remains
useful for authors and locally created packs, but building a repository executes
code and needs a separate explicit step. Resolve mutable tags to immutable
commits and verified artifact identities. Preserve the current local installer
as the final acquisition-independent installation boundary.

An AI setup workflow should be able to:

1. Inspect governed source and current capability gaps without running candidate
   pack code.
2. Select a user-supplied repository or create a local pack from documented
   schemas and a real reference pack.
3. Obtain or build the exact candidate through an explicit workflow.
4. Run conformance in temporary projects and inspect capabilities, toolchains,
   platforms, and authority requirements.
5. Install the verified candidate and transactionally update exact project
   selections and restoration metadata.
6. Verify required coverage and run only the workflow-selected checks.

Make pack creation a normal CLI workflow with machine-readable scaffolding,
command-adapter helpers, schema examples, a conformance harness, and actionable
errors. These are useful to both human authors and AI agents. Do not require
registration, central publication, telemetry, or a marketplace account.

Lifecycle semantics should distinguish deselecting a pack from deleting an
installed artifact. Deselecting cannot silently leave governed source without
required coverage. Removal addresses an exact digest, not only `NAME@VERSION`,
because storage permits different content identities for the same version.
References in other repositories must remain intact and fail clearly if their
installation is removed. An update prepares and verifies a new exact selection;
it never follows a floating release during normal checks.

The existing first-party plan makes an authenticated official catalog an early
phase. Make that optional distribution metadata rather than a prerequisite for
the pack runtime. An official repository or signed release index can provide
the same information without becoming a mandatory centralized registry. The
existing release capability catalog serves a different purpose and should
remain distinct.

### Create a pack from project needs

A developer should be able to ask their AI to create a pack for the project's
language and build system. Code Polishy's CLI supplies the creation workflow;
the AI maps that ecosystem's tools and evidence into the generated contract.
The core needs no built-in knowledge of the requested language.

For example, a Rust/Cargo project can drive creation of a locally owned pack
that invokes Cargo and the selected Rust toolchain's CLI commands. Rust/Cargo
is one example of the generic workflow. A new ecosystem uses the same steps.

Proposed authoring flow; `pack create` is new work, while local verification and
installation already exist:

```sh
code-polishy pack create --directory ./code-polishy-cargo
code-polishy pack verify --source ./code-polishy-cargo
code-polishy pack install --source ./code-polishy-cargo
```

The developer's AI completes the generated pack between creation and
verification. The creation command should provide:

- A manifest, adapter entry point, documentation, and executable fixture layout.
- A machine-readable description of required capabilities, missing mappings,
  protocol schemas, and next authoring steps for the target project.
- Reusable support for argument-array CLI invocation, explicit tool resolution,
  working directories, bounded output, timeouts, and result normalization.
- Fixture helpers that exercise passing code, real violations, tool failures,
  incomplete coverage, and format check/write behavior.

The pack owns the small ecosystem-specific layer: which CLI operations supply
which capabilities, how project metadata and compiler results are interpreted,
and what proves complete coverage. Prefer native structured output when the
tool provides it. Tool exit status, diagnostics, and analysis coverage must be
interpreted together; a successful process alone does not establish a policy
pass. Generated scaffolding must expose unfinished capabilities and cannot pass
conformance through placeholder success responses.

CLI invocation belongs to the existing governed execution boundary. Pack
authors should not have to reimplement process management, report storage,
installation, or the wire protocol. Code Polishy may provide a generic command
adapter runtime; it must not accumulate Rust, Cargo, Astro, or other ecosystem
special cases inside that runtime. Custom adapters remain available for tools
whose analysis cannot be expressed by the common command contract.

After verification, normal selection records the exact pack and toolchain
contract for the project. The developer can keep the pack local or publish its
repository and artifacts for others to install. The same creation workflow
should support project-specific needs as well as reusable public packs.

## 5. Is observability good today?

The defensible answer is: **good diagnostic reporting and gate auditability;
incomplete observability across all runtime operations.**

For this local CLI, judge observability by whether a user or maintainer can
explain a failure, delay, scope decision, or unexpected pass using the emitted
evidence. That follows OpenTelemetry's emphasis on understanding behavior and
diagnosing unfamiliar problems from instrumentation. It does not require a
hosted dashboard or remote collection.
See the [OpenTelemetry observability primer](https://opentelemetry.io/docs/concepts/observability-primer/).

### Existing strengths

| Question                                            | Current evidence                                                                                               |
| --------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| What failed, where, and what should change?         | Canonical findings, semantic fingerprints, related evidence, remediation, and JSON/SARIF.                      |
| Why is this finding in a focused run?               | Requested selection, analysis context, and selected/related/context/global relationships.                      |
| Did filtering hide failures?                        | Complete stored reports and summaries remain separate from display filtering.                                  |
| Which gate candidate and release ran?               | Gate identity binds base, candidate, release, platform, configuration, commands, and environment fingerprints. |
| Which governed command failed or waited?            | Attempts record exit/failure category, execution duration, resource wait, log digest, and truncation.          |
| Was a test executed, retried, diagnosed, or reused? | Separate attempt and reuse evidence, typed test artifacts, and receipt references.                             |
| Why are checks or packs unavailable?                | Doctor and capability discovery expose availability and coverage findings.                                     |

The latest ordinary command report is a real improvement over the older
checkout: handled command results are normalized, schema-validated, and stored
under a content-derived path. JSON and SARIF consume that same model. Existing
tests cover report filtering, deterministic storage, schema validity,
consumer-facing output, failed-command findings, and tampered gate artifacts.

### Gaps that prevent the broader claim

1. **Early and operational failures can bypass machine reports.**
   `runPolicyCommand` prints an engine-open error directly and returns.
   `FinalizeReport` runs only when the handler returns no error. Many tool
   failures become findings and are reported correctly, but configuration,
   initialization, and handler-error paths do not consistently produce the
   requested JSON/SARIF result. These are often precisely the failures an AI
   needs to diagnose.

2. **Result identity is not invocation identity.**
   Ordinary reports are content-addressed and have no universal run ID,
   start/end times, release identity, or operation timeline. Identical ordinary
   results intentionally share a path. Gate reports do have execution IDs and
   timing; extend that model without confusing report deduplication with run
   history.

3. **Internal analysis lacks uniform execution records.**
   JavaScript `Bundle.exchange` uses the host-process boundary directly, outside
   the gate artifact runner. Python facts use `exec.CommandContext` with private
   buffers. They return results or errors, but their individual operations do
   not receive the same timing, wait, log, and attempt records as planned gate
   commands. Source-graph input digests identify evidence, not runtime cost.

4. **Pack evidence stops at validation.**
   `RunAdapter` returns findings only. Successful `evidence` and `notes` are not
   propagated into the canonical report. During a gate, stdout may survive in
   the command log, but it is not a typed coverage record. An adapter may exit
   zero while reporting findings, operational failure, or malformed JSON; the
   process attempt and semantic outcome need separate linked fields.

5. **Interrupted runs have limited durable progress.**
   Gate command output is buffered and written when its log closes; the final
   report is published at finalization. A hard interruption during a long
   command can leave no persisted output for that command and no final report.
   Completed command logs are useful but are not a durable in-progress journal.

6. **CI does not retain the managed evidence explicitly.**
   The inspected CI workflow runs gates and native contracts but contains no
   managed-report/log upload step. Platform job logs remain useful, but the
   generated structured files are not explicitly preserved for later analysis.
   Add retention on success and failure, with explicit handling for hidden
   `.code-polishy-reports` paths and referenced artifacts.

7. **Sharing and retention need a complete policy.**
   Gate logs preserve raw bounded stdout/stderr; the inspected capture path
   has no redaction transform. Restricting child environments and hashing
   environment inputs does not sanitize emitted source, arguments, or tool
   output. Test-artifact pruning exists, but ordinary and gate report stores
   lack an equivalent visible lifecycle. Export must preserve useful evidence
   without automatically exposing private source or credentials.

8. **There is no demonstrated aggregate runtime view.**
   No OpenTelemetry instrumentation/exporter or metrics aggregation was found
   in the reviewed runtime. Gate timings could support duration and failure
   trends, but those views are not currently implemented or measured. Missing
   OTLP is not itself a blocker; incomplete operation coverage is.

Evidence: [report model](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/engine/report_types.go),
[report storage](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/engine/report_storage.go),
[CLI failure paths](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/cmd/code-polishy/main.go),
[gate evidence](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/gaterun/types.go),
[log persistence](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/internal/gaterun/log.go),
[CI](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/.github/workflows/ci.yml).

## 6. Delivery sequence and acceptance boundaries

The following phases describe proposed work, not currently available commands.
Each public provider cutover has one implementation owner. Private development
can be incremental; release surfaces must agree before old ownership is removed.

### Phase 1: Build the generic pack system and creation workflow

Deliver a usable authoring and runtime system for arbitrary conforming packs.
Establish the contract with independent CLI-backed packs and bounded discovery
prototypes. Existing core language implementations remain until their Phase 2
cutovers; extracting them is the next phase's acceptance exercise.

#### 1.1 Establish the baseline and ownership map

Use an agreed current upstream commit, then reconcile the separate Astro
behavior before extracting its current implementation. Inventory each
language's rules, configuration, parsers, tool assets, host assumptions, report identities,
dependency evidence, and executable fixtures. Mark every item as core, pack,
or shared protocol.

Resolve three decisions first: the optional role of official distribution
metadata; how core document/data/workflow services lose their dependence on the
full JavaScript bundle; and where pack-specific schemas and effective baseline
settings are owned and validated. Choose pack boundaries from declared provider
responsibilities and composition needs, independently of the current core's
language names and framework groupings.

Acceptance: every currently supported behavior has an owner and an observable
parity case. The map includes current Python contracts, generated-source rules,
test ownership, architecture review, and the agreed Astro behavior.

#### 1.2 Extend the existing execution and report boundaries

Give every policy invocation a run identity and every analysis operation a
linked record. Cover initialization, inventory, discovery, adapter execution,
validation, formatting, and report finalization. Keep volatile timing and run
IDs outside deterministic semantic fingerprints and reusable-input identities.

Retain effective engine/pack/tool identities, capability, scope digest,
selection and coverage counts, duration, resource wait, typed failure,
truncation, and artifact references. Keep process exit distinct from validated
analysis status. Route internal language adapters through the observed runner
or its common observer rather than building another scheduler.

Publish bounded progress records during execution. An unfinished run must be
identifiable as incomplete and never qualify as accepted gate evidence.
Return a small structured operational-error envelope even when full engine
initialization or report storage fails. Preserve exit semantics.

Add controlled CI artifact retention. Export only a reviewed allowlist with a
documented redaction strategy; do not upload the complete report tree blindly,
because review/context artifacts may include source or original intent text.
Keep raw private evidence immutable and identify sanitized exports as derived
artifacts. Data minimization should precede collection; see
[OpenTelemetry's sensitive-data guidance](https://opentelemetry.io/docs/security/handling-sensitive-data/).

Acceptance: a deliberately failed direct check, malformed configuration,
timed-out adapter, zero-exit analysis failure, canceled gate, and missing pack
can each be explained from bounded emitted records without adding debug code.
Private fixture secrets are absent from approved exported artifacts.

#### 1.3 Prove generic CLI adaptation and discovery

Use the creation workflow to build an independent pack around an ecosystem's
CLI, such as Rust/Cargo. Exercise actual metadata, diagnostics, passing code,
and violations through the public adapter contract. Verify installation size,
exact selection, runtime evidence, policy coverage, removal, and failure
behavior without depending on an existing core language implementation.

Use the discovery plan's contrasting Cargo, Gradle, Bundler, and CMake
prototypes to test discovery, execution authority, scope, and transport
conformance. These are contract test cases, not an approved language list or
commitments to ship four production packs. Exercise a small case and a
scale/conditional case for each. Measure actual bundle and inventory costs
rather than guessing new caps.

Acceptance: a pack for an ecosystem absent from core performs meaningful
analysis, and prototype evidence supports the discovery and transport
decisions. V1 remains the released contract until the coherent public cutover
is ready.

#### 1.4 Complete manifest/protocol v2 and decentralized lifecycle

Define separate inventory and selection, mandatory discovery mode, validated
scope identity, explicit host-toolchain requirements, typed baseline inputs,
rich findings, graph facts, successful coverage, and bounded artifact transport.
Reuse generic source-graph, report, and receipt types wherever their semantics
already fit. Allow explicit unsupported capabilities; do not turn missing
analysis into successful coverage.

Unify provider resolution before native dispatch is removed. Preserve engine
authority over required capabilities, effective baseline, exception validation,
test scheduling, data protection, and report acceptance. Packs own ecosystem
interpretation and language rule implementation.

Provide declarations for language identifiers, source patterns, and
ecosystem-specific coverage requirements so Phase 2 can move those decisions
out of core. Resolve project requirements against provider declarations through
the same generic boundary for every pack. Define explicit dependency and
shared-fact contracts where providers cooperate, including compatible schemas,
exact identities, and rejected cycles or overlapping authority.

Finish exact install/restore, installed-state listing, selection, update,
deselection, and removal. Add the generic pack-creation command and command
adapter helpers so an AI can author and verify a pack using ecosystem CLIs.
Keep acquisition metadata reproducible and separate from policy authority.
Multi-pack selection updates must either succeed
completely or leave repository policy unchanged; verified downloaded artifacts
may remain as unselected cache entries.

Acceptance: an independent repository can publish a pack, another machine can
restore its exact selection without a marketplace, and missing, tampered,
incompatible, ambiguous, or incomplete providers fail with actionable output.
Conformance verifies actual violations and coverage, not status alone. Prove
that a language or framework absent from engine source can be added through a
pack, and that project-specific framework coverage can be supplied by either a
dedicated provider or a broader provider without a special engine branch.
Exercise the complete creation workflow with an ecosystem CLI: scaffold, supply
the tool mappings and fixtures, verify, install, and select the resulting pack
in a temporary project. No engine patch or handwritten process-management layer
may be required.

Phase 1 is complete when a developer's AI can create, verify, install, select,
update, and remove a meaningful pack through the generic workflow, with usable
diagnostics and exact restoration on another machine. Phase 2 then tests that
system against the full complexity of Code Polishy's own current support.

### Phase 2: Move all existing core language support into packs

Use the Phase 1 creation, adapter, verification, installation, and selection
workflow for every extraction. Code Polishy's own packs get the same contracts
and execution authority as a developer-created pack. A migration that requires
a privileged adapter path or calls back into retained core language analysis
has not proved the system works.

The scope is exhaustive: move language-specific detection, parsing, rules,
configuration defaults, tools, framework integrations, and analysis adapters
found in the ownership inventory. Include partial language support outside the
four primary implementations listed below. Keep generic document, data,
repository, and gate responsibilities in core under their explicit boundaries.

#### 2.1 Extract complete providers and prove parity

This is the current implementation inventory and a suggested extraction order.
It is not a required pack set, a supported-language allowlist, or a decision to
bundle particular frameworks with a language. Assign each existing behavior to
its selected provider boundary while preserving its observable guarantees.

| Order                                        | Complete responsibility to transfer                                                                                                                                              | Distinct parity risks                                                                                                                     |
| -------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| Shell                                        | Detection, syntax/parser behavior, ShellCheck, comment/directive interpretation, applicable portability rules, tools, fixtures                                                   | Shebang-only scripts, generated/data classification, host Bash, supported dialects and platforms.                                         |
| Python                                       | Project and dependency inventory, carried runtime, packaging/Astroid facts, Ruff/ty/Vulture, architecture, runtime contracts, uv/Git evidence                                    | Nested projects, local environments as inputs, dynamic loaders, external contracts, inference partitioning, graph and receipt identities. |
| Current JavaScript/TypeScript implementation | Runtime and language tools, project resolution, existing framework checks assigned to explicit providers, imports/types/dead code, generated ownership, Node dependency evidence | Authored versus compiler-generated edges, literal route filenames, workspace resolution, lifecycle isolation, and core utility consumers. |
| Go                                           | Module/workspace interpretation, source graphs, formatting, vet/static analysis, build and dependency/vulnerability evidence                                                     | Build tags, nested modules, toolchain identity, environment, and distinction between target Go tooling and the engine's build language.   |

Start with shell as a bounded extraction, then use the more project-aware
implementations to test the contract's depth. State the actual Bash/ShellCheck
platform and toolchain contract; engine support alone does not establish native
Windows support for those tools. Finish every remaining language-specific item
in the inventory, regardless of whether it appears in the table.

When an extraction exposes a missing generic capability, improve the public
contract and conformance suite before completing that cutover. The resulting
capability must be available to independent pack authors on the same terms.
Do not solve the gap by adding an ecosystem exception to core.

For each release, remove the former implementation, installers, configuration
ownership, and fallback dispatch together. Keep equivalent negative and positive
fixtures in the owning pack and generic boundary tests in core. Do not retain
tests that merely demand old filenames or packaging.

Acceptance: the same invalid source still fails with equivalent rule identity,
scope, evidence, and exception semantics; the same valid source passes. Removing
an unused pack removes its optional tooling. A repository still selecting a
removed pack fails visibly. Core services remain usable without that pack.

#### 2.2 Adopt the packs and establish operational confidence

Make Code Polishy's own repository and CI select their required exact packs.
Exercise them through ordinary commands and gates after the corresponding
native implementations and release assets have been removed. This verifies
that the product depends on the same pack system offered to other developers.

Verify clean-machine and offline restoration, core-only use, supported pack
combinations, changed-file operations, update rollback, and native supported
platforms. Update adoption docs, examples, capability metadata, installer help,
and release guidance together.

Use the new records to baseline operation duration, resource wait, timeout and
operational-failure rates, evidence completeness, and justified receipt reuse.
Keep policy violations separate from operational reliability failures. Choose
performance targets after representative measurements; this assessment does
not establish an SLO or measured overhead budget.

An optional OTLP exporter or local report explorer can follow if users need
cross-run analysis. Its failure must not alter policy results or invalidate
otherwise valid evidence. No remote telemetry is necessary for the initial
pack release.

Acceptance: a maintainer can diagnose representative installation, discovery,
analysis, and gate failures from retained records, including interrupted runs,
without requesting a new instrumented build. The documented pack model matches
what a new user can actually install and remove.

Phase 2 is complete only when all inventoried core language support has moved
to independently installable/removable packs, existing behavior and evidence
remain equivalent, and Code Polishy's own verification uses those packs. A
core-only installation retains its generic responsibilities and contains no
hidden language-analysis fallback. Passing one reference pack alone does not
complete this phase.

## 7. Verification and work coordination

Use temporary repositories and exact, behavior-focused suites at each coherent
source boundary. A compact acceptance matrix should cover:

| Scenario                                       | Required observable result                                                                                                                         |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Exact pack restored on a clean machine         | Same verified identity and declared capabilities.                                                                                                  |
| Unfamiliar language or framework               | A conforming external pack supplies required coverage without an engine change or registry entry.                                                  |
| Developer requests a pack for an ecosystem CLI | Creation scaffolds the contract; tool mappings and real fixtures produce a verifiable, installable pack without reimplementing execution plumbing. |
| Existing core support is extracted             | Every inventoried language behavior runs through ordinary packs with equivalent results and no retained native fallback.                           |
| Code Polishy verifies its own repository       | Required packs are selected explicitly and execute through the same public workflow as independently authored packs.                               |
| Project requires framework-specific analysis   | Declared provider coverage is verified; language recognition alone cannot satisfy it.                                                              |
| Removed or corrupted selection                 | Blocking diagnosis; no fallback or silent reduction.                                                                                               |
| Two providers claim the same authority         | Deterministic conflict before affected analysis.                                                                                                   |
| Changed source needs a workspace manifest      | Complete context with bounded active selection.                                                                                                    |
| Large discovery or graph evidence              | Complete digest-bound transport, or an explicit typed limit failure.                                                                               |
| Adapter exits zero with invalid evidence       | Failed semantic operation, never an accepted pass.                                                                                                 |
| Real language violation                        | Expected stable rule, location, and coverage evidence.                                                                                             |
| Format meets data/generated files              | Current ownership and no-rewrite guarantees preserved.                                                                                             |
| Process is interrupted or loses storage        | Incomplete/operational outcome remains diagnosable; no reusable pass.                                                                              |
| CI finishes or fails                           | Approved reports and referenced logs remain available.                                                                                             |
| Pack/toolchain changes                         | Affected identities invalidate; unrelated evidence is reused only when its full identity remains valid.                                            |

Do not run supplemental suites merely because pack work touches many modules.
Follow the governing release's event rules and the repository's final-gate
owner. Use meaningful negative conformance fixtures during normal development;
reserve selected broader platform and hardening work for its authorized event.

To reduce interference with concurrent product development, keep initial work
at the generic pack, report, and runner boundaries. Coordinate later extraction
around an agreed committed language baseline, especially Python contracts and
Astro. After a provider's public cutover, subsequent fixes belong to its owning
pack.

The existing [universal-capabilities plan](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/docs/plans/universal-language-pack-capabilities.md)
and [first-party extraction plan](https://github.com/riteofstring/code-polishy/blob/436c7a9855f030874878263a89807f51f402bf89/docs/plans/installable-first-party-language-packs.md)
remain useful proposed designs. Before implementation, reconcile them with the
newer graph/report/Python behavior, installation-size constraints, decentralized
acquisition, and the distinction between declared and enforced execution
restrictions. This assessment does not change the active protocol or governance
baseline.
