# Changelog

## 0.24.5

- Adopt pinned Astroid inference for contained Python ancestry and nested runtime
  exports without importing target dependencies.
- Add repository-owned Python runtime contracts for types, decorators, module
  bindings, and entry points. Pytest uses bundled declarations; Pydantic,
  pydantic-settings, Hypothesis, and other third-party integrations require
  explicit configuration.
- Keep React Hooks checks on authored source without applying them to generated
  bundles; retain other applicable generated-code checks.
- Include Astroid's license, corresponding source, and exact SBOM identity in
  native release artifacts.

## 0.24.4 - 2026-09-05

- Track SQLite connection construction at each write, including instance
  attributes and construction inside `try` blocks after `None` initialization.
  Reassignments and uncertain control-flow paths retain dead-code findings.
- Recognize inherited Hypothesis stateful registrations, buffered raw-I/O
  callbacks, and HTTP request-handler dispatch and logging methods, including
  their required callback parameters. Preserve unrelated same-named methods.

## 0.24.3 - 2026-09-05

- Recognize pytest autouse fixtures and module-level pytest marks, SQLite
  connection row factories, and Hypothesis state-machine teardown overrides
  automatically during Python dead-code analysis.
- Resolve framework imports, aliases, inheritance, and connection bindings
  without importing project dependencies. Keep unrelated and ambiguous
  same-named definitions visible.

## 0.24.2 - 2026-09-05

- Make signed Git CI assessments opt-in with `supplyChain.gitEvidence.required`.
  Local dependency review retains public registry scanning and minimum-age
  checks, and warns about unverified Git-source coverage without requiring a
  custom attestation service.

- Preserve TypedDict key-read evidence for Callable fields with empty, explicit,
  or ellipsis parameter lists, including aliases and nested annotations.
- Identify the source file, location, TypedDict, and field when a duplicate key
  or unsupported field expression prevents complete Python analysis. Report
  expected type-resolution errors without an internal traceback.

## 0.24.1 - 2026-09-05

- Allow non-promoting task sessions to finish after independent caller commits.
  Promotion still requires an unchanged clean caller and a contained candidate.
- Parse modern npm dependency range maps separately from legacy recursive
  lockfile records. Preserve modern inventory precedence and decoder causes
  in dependency-review failures.
- Split oversized Python fact responses and retry smaller source partitions,
  retaining complete fact coverage and existing project resource limits.

## 0.24.0 - 2026-09-05

- Require configuration schema 4. Declare executable tests through `tests.paths`
  and exact `tests.ownership` mappings to production modules and quick focused
  suites. Remove `scope.tests`, implicit test ownership, free-standing Python
  dynamic symbols, and the former flat external-attribute receiver shape.
- Return one canonical finding and report model across direct commands, gates,
  JSON, and SARIF, including semantic identities, selection relationships,
  remediation, warning counts, and preserved reviewed or suppressed outcomes.
- Detect source-level cycles before projecting dependencies onto modules.
  Preserve complete cycle evidence for production, generated, and test source,
  and exclude unrelated Go and JavaScript projects from focused analysis.
- Add architecture-review packets and receipts tied to the current graph and
  candidate. Include unchanged implementation source in adoption reviews so
  configuration-only changes can be assessed against the actual boundaries.
- Require exact producers for project-generated executable output. Preserve
  generated formatting while retaining non-style checks and reproducibility
  coverage, and run JavaScript dead-code analysis from its owning package root.
- Resolve Python imports and reachability across bounded source partitions.
  Preserve proven TypedDict and Pydantic contracts and support exact typed-local
  and instance attribute writes without suppressing unrelated assignments.
- Bind dynamic Python targets to exact callsites, governed registry inputs, or
  dependency-owned external contracts. Validate installed contract definitions
  without importing dependency code and invalidate gate acceptance when those
  dependency inputs change. Keep external plug-in composition evidence separate
  from local dead-code reachability.
- Accept historical dependency inputs when reviewing a repair while enforcing
  exact pins on the candidate. Support authenticated security and age evidence
  for exact Git commits, including private dependencies, and bind that evidence
  to reports and gate reuse.
- Preserve historical review obligations when comparing an older repository
  configuration with a schema 4 candidate, without admitting retired forms as
  active policy.
- Recommend exact direct versions from the current authoritative lock scope,
  reporting ambiguous or unavailable resolutions instead of guessing.
- Add repository-aware capability discovery, explicit behavior-feature lookup,
  bounded task-start context, and authenticated upgrade capability deltas.
  Surface declared operational handoffs through normal context workflows while
  keeping managed `AGENTS.md` canonical.
- Direct adopting agents to map useful design rationale and maintain it when
  consequential boundaries change. Reuse retrieved design context until its
  scope, mappings, or relevant documents change.
- Clarify that agents commit progress at milestones, roughly every one to two
  hours of active editing on long tasks. Checkpoints may contain unfinished work
  or known failures; final verification and atomic public cutovers apply at
  delivery, merge, or release.
- Route the Ubuntu CI gate through change classification so documentation-only
  candidates install only their documentation toolchain, while source,
  ambiguous, and failed classifications retain the full fail-safe platform
  workflow.

## 0.23.0 - 2026-09-04

- Validate runtime configuration against the shipped JSON Schema before typed
  decoding, while retaining explicit defaults and semantic relationship checks.
- Replace handwritten Python packaging and source interpretation with one
  bounded adapter backed by the carried CPython runtime and PyPA `packaging`,
  including exact computed-import declarations and a pinned Ruff graph
  protocol.
- Replace handwritten Markdown, shell, and GitHub Actions structure parsing
  with goldmark, `mvdan/sh`, and actionlint while keeping Code Polishy's policy
  decisions fail closed and removing duplicate parser authority.
- Pin the official SPDX license and exception data, retain a bounded
  fail-closed expression grammar, and reject identifiers outside the admitted
  snapshot.
- Generate complete release dependency graphs with official CycloneDX models
  and deterministic in-toto provenance metadata without presenting local
  metadata as authenticated publication evidence.
- Limit cross-candidate test-receipt reuse to explicitly reusable suites with
  complete enforceable inputs and a sealed read-only execution view.
- Require exact, independently approved, expiring vulnerability assessments
  and complete release-age and vulnerability coverage for newly adopted
  dependency forms.

## 0.22.0 - 2026-09-03

- Publish deterministic verified native archives with checksums, internal
  manifests, CycloneDX SBOMs, SLSA provenance, one five-host release index, and
  digest-pinned non-root Linux OCI images built from those same archives.
- Give every test execution a managed artifact directory, validate declared
  JUnit and Cobertura XML, retain digest-bound evidence in gate reports, and
  safely prune only older completed Code Polishy-owned runs.
- Reuse successful suites only when their complete release, platform,
  toolchain, command, configuration, environment, ownership, and file-input
  identities match; support authenticated CI receipt bundles, exact
  supplemental resume, already-passed gates, and one declared final-gate owner.
- Separate test ownership from the production dependency graph, require every
  governed test to have one module and focused suite, expose architecture
  concentration facts, and execute exact duplicate or explicitly covered suites
  only once while retaining evidence for every requirement.
- Let generated JavaScript and TypeScript inherit the real source package's
  workspace, toolchain, TypeScript, lint, dead-code, dependency, and module
  context without synthetic manifests or lockfiles.
- Model exact typed Python attribute writes consumed by external runtimes while
  rejecting stale or broad declarations and leaving adjacent unused attributes
  visible to Vulture.
- Let the native pnpm audit survive short registry outages with five bounded
  attempts while continuing to fail closed after retry exhaustion.
- Classify documentation-only CI changes in a cheap independent job so Ubuntu,
  macOS, and Windows verification can start in parallel for source changes.

## 0.21.6 - 2026-09-03

- Keep sealed installations immutable across all policy-owned Python and
  Vulture commands by using one isolated, no-bytecode execution boundary and
  verifying the complete release after sequential installed commands.
- Recognize statically proven Pydantic model fields, configuration, validators,
  serializers, and computed fields as reachable while leaving lookalikes and
  ordinary unused symbols visible to Vulture.
- Let `merge-gate` govern a first-adoption candidate when the exact base policy
  is absent. Candidate policy forces a full gate; malformed or removed policy
  and missing candidate locks still fail closed.
- Use base-aware CI selection, skip platform jobs for ordinary Markdown, split
  the old nine-command tooling bundle by affected paths, and state that clean
  Git operations and pre-gate `test --changed` reruns require no new evidence.
- Split Vulture mutation coverage from unrelated quality analysis so a bounded
  dead-code change can run and retry one exact supplemental suite.

## 0.21.5 - 2026-09-03

- Support root-owned Python packages below validated in-tree build-backend
  paths, and share those source roots across Ruff, ty, Vulture, and
  architecture analysis. Report an unsupported layout once at the project
  boundary instead of cascading tool failures.
- Keep Vulture's 60% dead-code baseline while recognizing its version-matched
  standard-library whitelists, standard syntax-bound Python hooks, and exact
  in-tree PEP 517 backend hooks. Prevent managed Python analysis from writing
  bytecode into the sealed installed release.
- Define the authority transition during a Code Polishy upgrade and replace
  the repository lock atomically before the incoming guidance takes effect.

## 0.21.4 - 2026-09-03

- Make recurring external security monitoring opt-in through
  `supplyChain.recurringSecurityMonitoring`, without changing local or CI
  vulnerability checks.
- Allow nonempty `#SBATCH` directives at column one before executable shell
  code when prose comments are disabled.

## 0.21.3 - 2026-09-03

- Derive each managed Ruff invocation's Python target from
  `project.requires-python`, pass detected package roots explicitly, and use a
  shared policy-owned line length of 88 for formatting and linting. Reject
  missing or unusable Python targets and conflicting Ruff line-length settings
  before formatting can write.

## 0.21.2 - 2026-09-02

- Expand the sealed Python Ruff lint baseline to `B`, `C4`, `E`, `F`, `I`,
  `PIE`, `RUF`, `SIM`, and `UP`, while keeping `C901` in its dedicated
  policy-owned complexity pass.

## 0.21.1 - 2026-09-02

- Make the policy-owned Vulture adapter safe for durable gate command
  identities, so Python checkpoint and merge gates can run dead-code analysis.
- Keep the stable launcher able to verify and run already-installed releases
  with manifest versions 2 and 3 after installing a manifest-version-4 release.

## 0.21.0 - 2026-09-02

- Keep a language-pack staging root writable through its atomic rename, then
  seal the published root so pack installation works on macOS 15.
- Add one validated Python project inventory for flat, `src`, namespace, and
  nested layouts, with built-in module-direction enforcement and fail-closed
  import coverage.
- Split Python lint from dead-code analysis: seal Ruff `E4`, `E7`, `E9`, and
  `F` lint plus C901, use policy-owned Vulture 2.16 at fixed 60% confidence
  through carried CPython `3.12.13+20260728` from python-build-standalone,
  infer PEP 621 entry points, and accept only exact validated
  `scope.pythonDynamicReferences`; report `ty` diagnostics separately and
  select each dependency-bearing project's contained `.venv` explicitly.
- Accept PEP 508 Git dependencies only at full commits, validate build-system
  requirements, and require matching Git source and commit evidence in
  `uv.lock` without inventing registry facts.
- Govern GitLab CI roots and recursive local fragments as control inputs,
  require immutable images and external includes, recognize built-in template
  includes separately, and define the external weekly-monitoring evidence
  contract that static YAML cannot prove.
- Recognize shell source by its shebang when an unfamiliar extension, including
  `.sbatch` and `.sbatch.template`, has no other language owner.
- Add `scope.data` for parse-validated hand-written JSON and YAML whose bytes
  formatting never rewrites, while keeping generated executable source under
  the documented quality and architecture checks.
- Keep supplemental mutation and risk suites out of ordinary adoption,
  development, lock upgrades, and gates; run them only for an explicit trigger
  or once on a frozen release candidate.
- Make stable-candidate supplemental retry evidence composable: run all suites
  once, then rerun only failed or invalidated suites; reserve full repeats for
  shared mutation infrastructure, toolchain, selection, or unbounded impact.
- Keep canonical agent guidance focused on repository-specific commands, scope
  boundaries, and expensive-operation warnings; leave longer rationale in
  on-demand policy documents.

## 0.20.0 - 2026-09-02

- Add locally installed, content-addressed language packs with exact
  project selection, conformance fixtures, structured adapter evidence, and
  normal quality, architecture, build, supply-chain, doctor, and gate coverage.
- Add `docs list`, `docs find`, and `docs read` so agents can retrieve bounded,
  deterministic documentation from the exact installed release without a
  network request or guessed file path.
- Remove the source-only Polishy skill, use versioned CLI documentation as the
  detailed agent reference, and make the managed `CLAUDE.md` use Claude Code's
  native `@AGENTS.md` import.
- Add optional feature-scoped behavior-regression review with pre-implementation
  intent capture, additive task requests, `on-request`, `merge`, and
  `checkpoint` policy, concise gate status, clean-context review packets, and
  independently replayed red/green proofs bound to the exact decision.
- Extend selected behavior review with ordered dirty-state correction capture,
  evidence-bound final-state checks for task narration and rejected-code
  residue, and explicit `FINAL STATE` gate reporting.
- Add `checkpoint-gate --base REF` for accepting one completed task on a
  long-lived branch. It no-ops for an unchanged candidate, runs the Markdown
  contract for documentation-only work, validates selected behavior evidence,
  runs affected checks and focused tests, and binds the accepted HEAD plus its
  behavior-review status to the exact durable passed run report.
- Add durable checkpoint and merge run reports with bounded command logs,
  fact-based failure evidence derived from matching exact-base suites, explicit
  changed-test comparisons, and merge-wide plus latest-task test reminders.
  Add `merge-gate --resume` to reuse only validated ordinary-test receipts from
  an identical failed run; every proof, check, build, and security phase runs
  again.
- Add command-local `--help`, `-h`, and `help COMMAND` pages with syntax,
  selectors, side effects, exits, examples, and targeted usage corrections.
- Make the `agents` workflow preserve project-owned ignore rules while
  transactionally installing `/.code-polishy-reports/`; make its check and
  `doctor --strict` reject a missing report-artifact rule.

## 0.19.0 - 2026-08-28

- Make the self-mutation runner enforce its declared efficacy and mutant-
  coverage thresholds, exclude host-inactive Go source, and canonicalize
  artifact-security temporary roots before containment checks on macOS.
- Remove tautological, private-choreography, and human-snapshot tests; replace
  them with independently calculated release and artifact evidence plus
  observable boundary coverage. Correct the copyable setup URL in the README.
- Preserve source comments by default and let repositories set
  `quality.allowComments` to `false` to activate fail-closed language scanners,
  a closed machine-directive registry, and mapped design rationale.
- Add the `design-context` command for bounded source-specific rationale.
- Select an authorization-free documentation merge level for ordinary Markdown
  with zero application tests, builds, supply-chain providers, or artifact
  checks.
- Enforce sealed Markdown formatting and cross-platform UTF-8, containment,
  local-link, image, fragment, deletion-safety, and product-input escalation.
- Print a prominent non-blocking reminder when tests change, and define
  tautological and change-detector tests in canonical policy.
- Enforce Python cyclomatic complexity through policy-owned Ruff C901 with a
  shared fails-at threshold of 10, and type check Python by default with
  policy-owned `ty` 0.0.65 at its normal diagnostic severity.
- Add a one-time AI adoption setup wizard that presents optional choices with
  their defaults and repository-specific recommendations before configuration.
- Clarify that both the agent and user are CEOs with ADHD while shortening
  generated guidance without changing its remaining policy meaning.
- Advance the exact release manifest contract to version 3 and record the
  carried `ty` executable.

## 0.18.0 - 2026-08-27

- Apply the 30-day admission policy to standalone third-party executable pins,
  with fixed upstream timestamp resolvers and exact expiring assessments.
