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
- Lead routine updates with the outcome in plain language; stay under 100 words
  unless detail is requested or necessary. Omit file references, metrics,
  implementation details, and raw output unless requested or decision-relevant.

## Implementation

- Preserve unrelated work; avoid unrelated refactors. Fix root causes with the
  smallest maintainable change.
- Add backward compatibility, migrations, legacy support, or transitional code
  only when the caller explicitly requests it.
- Before changing governed source, retrieve `code-polishy design-context` for
  the planned scope. Reuse it until scope, mappings, or documents change. Follow
  `agent-workflows` for missing rationale and design updates.
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
- Run supplemental suites only when explicitly requested, invoked by a
  checked-in event workflow, or selected by the version-matched release
  checklist. Declarations, including
  `tests.requiredSupplementalKinds`, never authorize execution.
  Use `test --supplemental --resume` for retries; run all for major releases,
  absent trusted evidence, shared infrastructure/toolchain/selection changes,
  or unbounded impact. Credentialed, destructive, and live-provider probes
  require a named external approval gate.

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
- Ordinary delivery follows `verification.finalGateOwner`: run
  `code-polishy merge-gate --base REF` locally or in CI. Duplicate only on
  request. Exact passes execute nothing; resume only unchanged failures.
- Every release requires a passing local full `code-polishy gate` on the final
  candidate before tagging or publishing, regardless of `finalGateOwner`.
  Required CI checks still apply.
- Major version releases require all mutation suites on the stable candidate.
  Minor and patch releases select them only on explicit caller or event-workflow
  request.
- Commit task-owned progress at milestones, roughly every 1–2 hours of active
  editing on long tasks. Checkpoints may be unfinished or failing; record what
  remains and verification status. Do not wait for gates or API cutovers.
- Before delivery, verify and commit task-owned changes unless an uncommitted
  handoff is requested. Public cutovers must be coherent at merge or release.
  Push, publish, and pull-request operations require explicit authorization.
