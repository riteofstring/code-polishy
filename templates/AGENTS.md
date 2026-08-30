## Authority and communication

- Run the locally installed `code-polishy`; `.code-polishy.lock.json` names the
  exact release this repository requires. Treat a release change as a deliberate,
  reviewed change. If the command is unavailable on `PATH`, check the stable
  launcher under the caller-specified or default installation prefix.
- `.code-polishy.json` owns project modules, dependency direction, capabilities,
  commands, test suites, artifact targets, and temporary exceptions. Target
  configuration cannot weaken the locked shared baseline.
- Talk to the user like both you and the user are CEOs with ADHD. Lead with the
  outcome, use plain language, and default to one short paragraph or at most
  five bullets. Add detail only when needed for action or safety.
- State conclusions positively and directly. Avoid rhetorical contrast formulas
  such as “not X, but Y.” Translate command output into plain language. Include
  raw banners, receipts, and machine-oriented output only when requested.

## Implementation

- Preserve unrelated user work. Choose the simplest principled solution that
  fixes the root cause and remains maintainable. Avoid speculative abstractions,
  over-engineering, and workaround patches.
- Add backward compatibility, migrations, legacy support, or transitional code
  only when the caller explicitly requests it.
- Treat an atomic public cutover as an externally visible contract. Private
  implementation batches may be incremental; committed public APIs must remain
  coherent and fully cut over.
- Keep one authoritative owner for each concept and generate downstream contract
  surfaces from it. Parse external data once at a boundary into validated types.
- Before changing governed source, run `code-polishy design-context` for the
  exact files or modules and read only the returned current design documents.
  Plans, historical evidence, and superseded decisions require an explicit task
  need; do not load them as routine source context.
- Honor `quality.allowComments`. When it is false, keep governed handwritten
  source free of prose comments and docstrings. When it is true, preserve useful
  accurate comments and add one only when it conveys information the code
  cannot. Put current non-local rationale in mapped design documents.
- Keep domain modules independent of UI, HTTP, persistence, providers, and
  process details unless the checked-in architecture contract allows the edge.
- Declare separately owned files, directories, repositories, and services as
  external inputs. Make source resolution and compatibility visible, and report
  unavailable integrations explicitly.
- Validate every resource in a multi-resource mutation before writing, then use
  one atomic or transactional boundary. Prefer typed failures over silent
  fallbacks, broad catches, fake success, and keyword heuristics.

## Dependencies and tests

- Pin direct dependencies and the package manager exactly. Use frozen lockfiles
  for normal setup. For a dependency update, generate the candidate lockfile
  without lifecycle scripts and run
  `code-polishy dependency-review --base <merge-target>` before installation.
- Keep every exception exact, visible, owned, justified, and expiring.
- Give every module a quick boundary suite. Test observable behavior with
  temporary state. Reject tautological, change-detector, no-op,
  pass-with-no-tests, and coverage-only tests; checked-in Gherkin must execute.
- Run supplemental mutation or risk suites only when the caller or a checked-in
  workflow requires them. Keep credentialed, destructive, and live-provider
  probes in explicit external approval gates.

## Reviews and delivery

- Agent review cannot replace policy checks or human approval.
- For every non-documentation checkpoint or merge candidate, commit the change
  and run `code-polishy behavior-review prepare`. Start a review subagent with
  no inherited conversation and give it only the generated packet. If the
  harness cannot start subagents, use a separate clean AI invocation with only
  that packet. Prove each requested behavior red on its pre-fix base and green
  on the candidate, then finalize the review receipt. Both gates independently
  replay those proofs. Keep
  `.code-polishy-reports/behavior-review` in the same workspace or move it only
  through an explicit trusted CI artifact handoff.
- Use the caller's checkout for ordinary interactive work. Use
  `code-polishy task-session` for unattended work or explicitly requested
  isolation.
- For ordinary Markdown-only work, run `code-polishy format --git-changes` and
  skip application tests. Fix documentation findings directly without asking
  the user for authorization. Control and declared product-input Markdown
  follow ordinary source verification.
- Run exact tests while editing source and `code-polishy test --changed` for
  broader feedback. After each completed code-changing task on a long-lived
  branch, run `code-polishy checkpoint-gate --base <previous-checkpoint>`. It
  requires the finalized behavior receipt, replays cited proofs, runs affected
  checks and focused tests, then records the accepted HEAD. Do not run it for
  conversational or read-only requests; an unchanged invocation is a no-op. At
  a merge checkpoint, resolve the base from an explicit target, checked-in
  guidance, `origin/HEAD`, then `origin/main` or `origin/master`.
- Run `code-polishy merge-gate --base <merge-target>` once for the final
  candidate. Let it select documentation, recommended, or full without asking
  the user. Summarize its result in plain language.
- Commit all completed task-owned changes after required verification unless the
  caller explicitly requests an uncommitted handoff. Keep commits coherent and
  free of unrelated user work. Push, publish, and pull-request operations require
  explicit caller authorization.
