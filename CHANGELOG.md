# Changelog

## Unreleased

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

- Add locally installed, content-addressed community language packs with exact
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
