## Authority and communication

- Run the locally installed `code-polishy`; `.code-polishy.lock.json` names the
  exact release this repository requires. Treat a release change as a deliberate,
  reviewed change. If the command is unavailable on `PATH`, check the stable
  launcher under the caller-specified or default installation prefix.
- `.code-polishy.json` owns project modules, dependency direction, capabilities,
  commands, test suites, artifact targets, and temporary exceptions. Target
  configuration cannot weaken the locked shared baseline.
- Talk to the user like a CEO with ADHD. Lead with the outcome, use plain
  language, and default to one short paragraph or at most five bullets. Add
  detail only when needed for action or safety.
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
- Keep every exception exact, visible, owned, justified, and expiring. Keep
  release-age, vulnerability, dependency-override, and general exceptions
  separate; vulnerability assessments require distinct owner and approver
  identities plus linked analysis, approval, and remediation.
- Give every module a quick boundary suite. Test observable behavior with
  temporary state. Reject tautological, no-op, pass-with-no-tests, and
  coverage-only tests; checked-in Gherkin must execute.
- Run supplemental mutation or risk suites only when the caller or a checked-in
  workflow requires them. Keep credentialed, destructive, and live-provider
  probes in explicit external approval gates.

## Reviews and delivery

- Bind a requested agent review to an explicit trusted base and exact candidate.
  Include committed, staged, unstaged, and untracked changes when reviewing a
  dirty worktree. Report actionable findings or one concise no-findings outcome.
  Agent review cannot replace policy checks or human approval.
- Use the caller's checkout for ordinary interactive work. Use
  `code-polishy task-session` for unattended work or explicitly requested
  isolation; select its modules before the worker starts and never nest it.
  The supervisor owns delegated scope, integration, commits, and quiescence.
- Run exact tests while editing and `code-polishy test --changed` for broader
  feedback. At a merge checkpoint, resolve the base from an explicit target,
  checked-in guidance, `origin/HEAD`, then `origin/main` or `origin/master`.
- Run `code-polishy merge-gate --base <merge-target>` once for the final
  candidate. Let it select recommended or full without asking the user. Summarize
  its result in plain language; keep detailed receipts in verbose logs.
- Commit all completed task-owned changes after required verification unless the
  caller explicitly requests an uncommitted handoff. Keep commits coherent and
  free of unrelated user work. Push, publish, and pull-request operations require
  explicit caller authorization.
