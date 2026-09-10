## Authority and communication

- Run `code-polishy` pinned by `.code-polishy.lock.json`; if absent from `PATH`,
  use the installation prefix's stable launcher.
- Before changing the repository, run `code-polishy docs read agent-workflows`
  and follow its version-matched request-capture and delivery rules.
- During an upgrade, outgoing guidance governs until the exact incoming release
  atomically rewrites the lock; that cutover activates incoming guidance.
- `.code-polishy.json` declares modules, dependency direction, capabilities,
  commands, test suites, artifacts, and exceptions; it cannot weaken the locked
  baseline.
- Keep routine updates under 100 words, lead with the outcome, and omit file
  references, metrics, implementation detail, and raw output unless needed;
  expand when action or safety requires it.

## Implementation

- Preserve unrelated user work and avoid unrelated refactors. Fix the root cause
  with the smallest maintainable change.
- Add compatibility, migrations, or transitional code only when explicitly
  requested.
- Before governed source changes, retrieve `code-polishy design-context` for
  the planned scope. Reuse it until scope, mappings, or documents change. Follow
  `agent-workflows` for missing rationale and design updates.
- Honor `quality.allowComments`: when false, omit prose comments and docstrings
  from governed handwritten source; when true, comment only facts code cannot
  convey. Put non-local rationale in mapped design documents.
- Keep prompt, agent, task, rejection, and editing narration out of final
  artifacts unless that process is their documented subject.
- Remove rejected behavior at its source. Keep no related guards, flags,
  fallbacks, tests, names, configuration, or compatibility paths unless the
  final requirement needs them.

## Dependencies and tests

- Pin direct dependencies and package managers exactly; use frozen lockfiles.
  For updates, generate candidate locks without lifecycle scripts and run
  `code-polishy dependency-review --base <merge-target>` before installation.
- Before admitting a security fix under 30 days old, determine whether its
  advisory affects reachable behavior. If not, retain the current version under
  an exact approved assessment until the fix reaches 30 days; if affected, use
  security-fix admission.
- Keep every exception exact, visible, owned, justified, and expiring.
- Give every module a quick boundary suite. Test observable behavior with
  temporary state. Reject tautological, change-detector, no-op,
  pass-with-no-tests, and coverage-only tests; checked-in Gherkin must execute.
- Run supplemental suites only when explicitly requested, invoked by a checked-in
  event workflow, or selected by the version-matched release checklist.
  Declarations, including
  `tests.requiredSupplementalKinds`, never authorize execution. Exact reruns
  record receipts. On stable candidates, use `test --supplemental --resume` for
  missing, failed, expired, or invalidated suites. Run all only without trusted
  evidence or after shared infrastructure, toolchain, selection, or unbounded-
  impact changes. Credentialed, destructive, and live-provider probes require a
  named external approval gate.

## Reviews and delivery

- Agent review cannot replace policy checks or workflow-required human approval.
- Use the caller's checkout for ordinary interactive work. Use
  `code-polishy task-session` for unattended work or explicitly requested
  isolation.
- For ordinary Markdown-only work, run `code-polishy format --git-changes`, fix
  its findings, and skip application tests. Verify control and product-input
  Markdown as source.
- Checkout, fetch, clean merge or rebase, tagging, and push prep require no
  tests. After resolving a conflict, run one affected exact test; prose-only
  conflicts follow the Markdown rule.
- During development, run the narrowest useful exact test after a coherent
  runnable change, not after every edit or chat turn. Use
  `code-polishy test --changed` at a completed source boundary only when a final
  gate will not immediately follow. Resolve the merge base from an explicit
  target, checked-in guidance, `origin/HEAD`, then `origin/main` or
  `origin/master`.
- Honor `verification.finalGateOwner`. Use `code-polishy merge-gate --base REF`
  once, locally or in its checked-in CI workflow. Duplicate only when the caller
  requests independent evidence.
  An exact passed identity executes nothing. Resume only an unchanged failed
  candidate. Summarize the result in plain language.
- Commit task-owned progress at milestones, roughly every 1–2 hours of active
  editing on long tasks. Checkpoints may be unfinished or failing; record what
  remains and verification status. Do not wait for gates or API cutovers.
- Before delivery, complete required verification and commit remaining task-owned
  changes unless the caller requests an uncommitted handoff. Public cutovers
  must be coherent at merge or release. Exclude unrelated user work. Push,
  publish, and pull-request operations require explicit caller authorization.
