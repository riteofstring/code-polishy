## Authority and communication

- Run pinned Code Polishy through `./code-polishyw` (PowerShell:
  `.\code-polishyw.ps1`); use `setup` if the release is absent.
- Before changing the repository, run `./code-polishyw docs read agent-workflows`
  and follow its version-matched request-capture and delivery rules.
- During an upgrade, outgoing guidance governs until the exact incoming release
  atomically rewrites the lock; that cutover activates incoming guidance.
- `.code-polishy.json` declares modules, dependency direction, capabilities,
  commands, test suites, artifacts, and exceptions; it cannot weaken the locked
  baseline.
- Keep updates outcome-first and under 100 words; add detail only for action or
  safety.

## Implementation

- Preserve unrelated work. Reliability machinery must address demonstrated
  needs, preserve valid results and ordinary recovery, reduce end-to-end
  complexity, and lower total failure risk.
- Hash only for trust-boundary authentication, immutable identity, or reusable
  evidence. Never hash local state for change detection, mirror an authoritative
  digest, or rehash within one trusted operation.
- Add compatibility or migration code only when requested.
- Before governed source changes, retrieve `code-polishy design-context` for
  the planned scope. Reuse it until scope, mappings, or documents change. Follow
  `agent-workflows` for missing rationale and design updates.
- Honor `quality.allowComments`: when false, omit prose comments and docstrings
  from governed handwritten source; when true, comment only facts code cannot
  convey. Put non-local rationale in mapped design documents.
- Keep prompt, agent, task, rejection, and editing narration out of final
  artifacts unless that process is their documented subject.
- Remove rejected behavior and its guards, flags, fallbacks, tests, names,
  configuration, and compatibility paths unless final requirements need them.

## Dependencies and tests

- Pin dependencies and package managers exactly; use frozen locks. Generate
  update locks without scripts. Run
  `code-polishy dependency-review --base <merge-target>` before installation.
  Then install frozen with scripts off, run `code-polishy supply-chain --offline`,
  and test.
- Before admitting a security fix under 30 days old, determine whether its
  advisory affects reachable behavior. If not, retain the current version under
  an exact approved assessment until the fix reaches 30 days; if affected, use
  security-fix admission.
- Keep exceptions exact, visible, owned, justified, and expiring.
- Give every module a quick boundary suite. Test observable behavior with
  temporary state. Reject tautological, change-detector, no-op,
  pass-with-no-tests, and coverage-only tests; checked-in Gherkin must execute.
- Run supplemental suites only when requested, triggered by a checked-in event,
  or selected by the release checklist. Declarations, including
  `tests.requiredSupplementalKinds`, never authorize execution. Reuse receipts
  with `test --supplemental --resume`; run all only without trusted evidence or
  after shared infrastructure, toolchain, selection, or unbounded-impact
  changes. Credentialed, destructive, or live-provider probes need named
  external approval.

## Reviews and delivery

- Agent review cannot replace policy checks or required human approval.
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
- Run `code-polishy merge-gate --base REF` only at a genuine merge or release
  checkpoint through `verification.finalGateOwner`. Ordinary task completion,
  commits, and delivery do not select it. Duplicate only on request; an exact
  pass executes nothing, and only an unchanged failed candidate may resume.
- Commit task-owned progress at milestones, roughly every 1–2 hours of active
  editing on long tasks. Checkpoints may be unfinished or failing; record what
  remains and verification status. Do not wait for gates or API cutovers.
- Before delivery, complete required verification and commit remaining task-owned
  changes unless the caller requests an uncommitted handoff. Public cutovers
  must be coherent at merge or release. Exclude unrelated user work. Push,
  publish, and pull-request operations require explicit caller authorization.
