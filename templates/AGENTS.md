## Authority and communication

- Run the installed `code-polishy` release pinned by `.code-polishy.lock.json`;
  if absent from `PATH`, use the installation prefix's stable launcher.
- Before changing the repository, run `code-polishy docs read agent-workflows`
  and follow its version-matched request-capture and delivery rules.
- During an upgrade, outgoing guidance governs until the exact incoming release
  atomically rewrites the lock; that cutover activates incoming guidance.
- `.code-polishy.json` declares modules, dependency direction, capabilities,
  commands, test suites, artifacts, and exceptions; it cannot weaken the locked
  baseline.
- Be succinct. For routine updates and summaries, lead with the outcome in plain
  language and stay under 100 words unless the caller requests detail or
  action/safety requires it. Omit file references, metrics, implementation
  details, and raw output unless requested or decision-relevant.

## Implementation

- Preserve unrelated user work and avoid unrelated refactors. Fix the root cause
  with the smallest maintainable change.
- Add backward compatibility, migrations, legacy support, or transitional code
  only when the caller explicitly requests it.
- Treat an atomic public cutover as an externally visible contract. Private
  implementation batches may be incremental; committed public APIs must remain
  coherent and fully cut over.
- Before changing governed source, run `code-polishy design-context` for the
  exact files or modules and read only the current documents it returns. Load
  plans, history, or superseded decisions only for an explicit task need.
- Honor `quality.allowComments`. When it is false, keep governed handwritten
  source free of prose comments and docstrings. When it is true, add comments
  only for facts the code cannot convey. Put non-local rationale in mapped
  design documents.
- Keep prompt, agent, task, rejection, and editing narration out of final
  artifacts unless that process is their documented subject.
- Remove rejected behavior at its source. Keep no related guards, flags,
  fallbacks, tests, names, configuration, or compatibility paths unless the
  final requirement needs them.

## Dependencies and tests

- Pin direct dependencies and the package manager exactly. Use frozen lockfiles
  for normal setup. For a dependency update, generate the candidate lockfile
  without lifecycle scripts and run
  `code-polishy dependency-review --base <merge-target>` before installation.
- Keep every exception exact, visible, owned, justified, and expiring.
- Give every module a quick boundary suite. Test observable behavior with
  temporary state. Reject tautological, change-detector, no-op,
  pass-with-no-tests, and coverage-only tests; checked-in Gherkin must execute.
- Run supplemental suites only when the caller explicitly requests them, a
  checked-in event workflow invokes them, or the version-matched release
  checklist selects them. Declarations, including
  `tests.requiredSupplementalKinds`, never authorize execution. Exact reruns
  record receipts. On a stable candidate, use `test --supplemental --resume` to
  run only missing, failed, expired, or invalidated suites. Run all only without
  a trusted baseline, after shared infrastructure, toolchain, or selection
  changes, or when impact is unbounded. Credentialed, destructive, and
  live-provider probes require a named external approval gate.

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
- Commit all completed task-owned changes after required verification unless the
  caller explicitly requests an uncommitted handoff. Keep commits coherent and
  free of unrelated user work. Push, publish, and pull-request operations require
  explicit caller authorization.
