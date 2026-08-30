## Authority and communication

- Run the locally installed `code-polishy` release named by
  `.code-polishy.lock.json`. Treat a release change as a reviewed change.
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
- Honor `quality.allowComments`. Preserve useful comments when allowed; when it
  is false, keep governed handwritten source free of prose comments and
  docstrings. Put non-local rationale in mapped design documents.
- Keep domain modules independent of UI, HTTP, persistence, providers, and
  process details unless the checked-in architecture contract allows the edge.
- Declare separately owned files, directories, repositories, and services as
  external inputs. Make source resolution and compatibility visible, and report
  unavailable integrations explicitly.
- Validate every resource in a multi-resource mutation before writing, then use
  one atomic or transactional boundary. Prefer typed failures over silent
  fallbacks, broad catches, fake success, and keyword heuristics.

## Dependencies and tests

- Pin direct dependencies and the package manager exactly, use frozen lockfiles
  for normal setup, and follow the governed dependency-update workflow before
  installing a candidate.
- Keep every exception exact, visible, owned, justified, and expiring.
- Give every module a quick boundary suite. Test observable behavior with
  temporary state. Reject tautological, change-detector, no-op,
  pass-with-no-tests, and coverage-only tests; checked-in Gherkin must execute.
- Run supplemental mutation or risk suites only when the caller or a checked-in
  workflow requires them. Keep credentialed, destructive, and live-provider
  probes in explicit external approval gates.

## Delivery

- Agent review cannot replace policy checks or human approval.
- Use the caller's checkout for ordinary interactive work. Use
  `code-polishy task-session` for unattended work or explicitly requested
  isolation.
- Run exact tests while editing source and `code-polishy test --changed` for
  broader feedback. Follow the release-owned `polishy` skill and checked-in
  workflow for documentation-only validation, dependency review, checkpoints,
  merge gates, retries, and evidence custody.
- For every non-documentation checkpoint or merge candidate, commit first, give
  a clean review agent only the generated behavior packet, prove requested
  behavior red on the base and green on the candidate, and let both gates replay
  the proofs independently.
- Commit all completed task-owned changes after required verification unless the
  caller explicitly requests an uncommitted handoff. Keep commits coherent and
  free of unrelated user work. Push, publish, and pull-request operations require
  explicit caller authorization.
