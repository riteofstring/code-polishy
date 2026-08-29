---
name: polishy
description: >-
  Operate a repository through the Code Polishy release its lock names: inspect policy health, clean up concrete findings, display testing diagnostics, run policy-selected ordinary merge verification, synchronize canonical agent guidance, and enforce caller-scoped task sessions. Use when the user invokes /polishy or $polishy, asks what tests or verification level to run, asks whether a repository is policy-ready, or requests a Code Polishy cleanup, merge gate, or AGENTS sync. Triggers on repository policy, policy check, cleanup, test levels, test plan, recommended tests, full tests, supplemental tests, behavior review, regression proof, merge gate, gate, doctor, task session, and AGENTS sync.
---

# Operate Code Polishy

Use the release named by the target repository's `.code-polishy.lock.json` as
the source of truth. Keep inspection read-only until the user asks for a change.
Use the caller's checkout for ordinary interactive work. Use one caller-scoped
`task-session` for unattended work or when the caller explicitly requests an
isolated boundary. A supervising agent may assign bounded work to its native
subagents and remains responsible for integration and verification.

## Core Principles

### 1. Establish the Local Contract

Read the target `AGENTS.md`, `.code-polishy.json`, `.code-polishy.lock.json`, and
Git status before acting. Use the installed `code-polishy` command for every
engine command; it runs the exact release the lock names. In the Code Polishy
repository itself, follow its local instructions for `./bin/code-polishy` and
the pinned Go toolchain.

If `code-polishy` is unavailable on `PATH`, use the stable launcher under a
caller-specified installation prefix. With the default prefix, try
`~/.local/bin/code-polishy`, then
`~/.local/share/code-polishy/bin/code-polishy` on Unix, or
`%LOCALAPPDATA%\CodePolishy\bin\code-polishy.exe` on Windows. Treat a failed
bare command as a discovery issue until these stable locations are checked.

Never substitute a neighboring checkout or another release for the one the lock
names. If the config or lock is missing, report that the repository is not
fully adopted. Do not install or upgrade Code Polishy unless the user asked for
adoption or an upgrade.

Preserve unrelated user changes. Treat project-specific providers, builds, and
tests declared in `.code-polishy.json` as part of the policy contract rather
than generic code to delete.

Before changing governed source, run `code-polishy design-context` for the
exact files or modules in scope and read only the paths it prints. It resolves
current mapped design rationale; plans, historical evidence, and superseded
decisions remain deliberate task-specific inputs.

Honor `quality.allowComments`. When it is false, keep governed handwritten
source free of prose comments and docstrings and retain only exact
machine-consumed directives. When it is true, preserve useful accurate comments
and add one only when it conveys information the code cannot. Move current
non-local rationale to the mapped design document that owns it.

When the caller requests an enforced autonomous edit boundary, launch the
worker before it starts through `code-polishy task-session`. The caller must
select every allowed module; never infer an all-repository scope or let the
worker add modules from inside the session. Reuse that one session for
dependency setup, implementation, and tests. Do not nest another session when
`CODE_POLISHY_TASK_SESSION=1` is already present.

### 2. Resolve the Merge Target and Inspect the Decision

Resolve the merge target in this order: an explicit user value, checked-in
repository guidance, the symbolic `origin/HEAD`, then an existing `origin/main`
or `origin/master`. If none resolves, do not run ordinary merge verification. A
no-base planner view covers current uncommitted changes only and is diagnostic
advice, not a merge decision.

Run the read-only planner:

```sh
code-polishy test-levels --base <merge-target>
```

Use the table's facts to explain the policy-selected ordinary level, reasons,
and `merge-gate` execution path. Show the emitted ASCII table only when the
caller explicitly requests raw planner output. Without a base, the result is
advice.

If `test-levels` is unavailable, the target locks an older Code Polishy release.
Run `test-plan` only for compatibility, say that the tabular command requires a
policy upgrade, and do not invent authoritative suite counts.

### 3. Keep Verification Boundaries Distinct

- Treat focused (`test --changed`, `--module`, or exact `--suite`) as routine
  implementation feedback when repository guidance permits it.
- Treat ordinary Markdown-only work as an automatic documentation lane. Run
  `format --git-changes`, fix deterministic documentation findings, and run no
  application tests. Never ask the user to authorize this lane.
- Treat recommended and full as profiles whose ordinary merge selection belongs
  only to `merge-gate`, not as a question to return to the user.
- Treat supplemental mutation and risk suites as a separate final-hardening
  stage. Run `code-polishy test --supplemental` after ordinary verification
  only when the caller or checked-in workflow requires it. Supplemental suites
  are excluded from `test --all`, `verify`, `gate`, and `merge-gate`.
- A focused or ordinary verification request does not imply supplemental
  execution.
- Treat `verify` as full ordinary tests plus builds, and `gate` as the complete
  policy, ordinary verification, build, and online supply-chain workflow. Do
  not describe either as merely another test level.
- At an ordinary merge checkpoint, resolve the trusted base and run
  `merge-gate --base <trusted-base>` without asking the user to choose
  a level. It alone selects documentation, the configured recommended merge
  profile, or the complete full gate and accepts no caller-supplied file,
  module, suite, or quick-mode scope.
- When `verification.behaviorReview.required` is enabled, prepare every clean,
  committed non-documentation candidate before that merge gate. Give only the
  generated packet to a fresh native reviewer. Record red-on-pre-fix and
  green-on-candidate `regression-proof` evidence for every behavior it
  classifies as requested, save its strict result, and run `behavior-review
finalize`. Keep `.code-polishy-reports/behavior-review` in the same workspace
  or move it only as an explicit trusted CI artifact. The supervising agent's
  existing context is not a fresh review.

Do not turn an ambiguous request such as "test it" into an ordinary merge
checkpoint. A direct request for a scoped profile remains scoped feedback, not
an alternative way to select the merge policy.

### 4. Run the Appropriate Scope

Use exact focused selectors for implementation feedback. At an ordinary merge
checkpoint, run `code-polishy merge-gate --base <trusted-base>` rather than
selecting a profile from `test-levels`. On failure, diagnose and rerun the exact
failing suite or narrow package first. Do not repeatedly restart a broad run
while its known failure remains.

Report pass/fail in plain language, actionable findings, and anything required
that was not run. Never claim that a skipped or unavailable check passed.

### 5. Clean Up Concrete Policy Findings

Only mutate code when the user asks for cleanup or fixes. Start with:

```sh
code-polishy doctor --strict
code-polishy check --git-changes
```

Use `check --all` only for a requested repository-wide cleanup. Fix confirmed
findings at their owning boundary, then run formatting and focused tests in the
smallest affected scope. Investigate non-blocking warnings when they match the
request, but do not silently convert warnings into failures or add exceptions.

Never create a broad, ownerless, or non-expiring exception to make a run green.
If a deliberate exception is necessary, explain its exact subject, owner,
reason, and expiry before adding it.

### 6. Maintain Canonical Guidance

After an intentional Code Polishy lock upgrade, run `agents check`, then
`agents sync` when stale. Treat the entire `AGENTS.md` as release-owned:
`agents install` must preserve and report a conflict for a noncanonical existing
file, while `agents sync` requires an existing file and replaces all stale bytes
with the canonical version. Review the resulting whole-file policy change. Keep
canonical guidance limited to durable rules used across tasks; put command
procedures, rationale, and edge cases in this skill or the permanent docs.
After required verification, commit all completed task-owned changes unless the
caller explicitly requests an uncommitted handoff.

## Common Mistakes

| Mistake                                           | Correction                                                                                            |
| ------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Running a sibling checkout or another release     | Use `code-polishy`, which runs the release the lock names                                             |
| Treating a `PATH` miss as a missing installation  | Probe the caller-specified or default stable launcher paths                                           |
| Pasting planner output without a request          | Explain its level and reasons in plain language; show the raw table when requested                    |
| Treating supplemental as part of full             | Run `test --supplemental` as a separate stage when the caller or checked-in workflow requires it      |
| Running application tests for ordinary Markdown   | Format it, fix documentation findings, and let `merge-gate` select documentation automatically        |
| Skipping an enabled behavior-review receipt       | Prepare the clean candidate, use a packet-only fresh reviewer, prove requested behavior, and finalize |
| Running `gate` after scoped feedback              | Keep scoped feedback scoped; use `merge-gate` at an ordinary merge checkpoint                         |
| Repeating an entire failed broad run              | Isolate and rerun the failing suite first                                                             |
| Adding an exception to silence cleanup            | Fix the owner or make any necessary exception exact, owned, and expiring                              |
| Starting autonomous work without selected modules | Require the caller to select modules before creating the task session                                 |
| Leaving completed verified changes uncommitted    | Commit task-owned changes unless the caller requests an uncommitted handoff                           |

## Output Format

For a diagnostic level request, state whether the result is no-base advice or a
trusted-base policy selection and explain the level and reasons. Show the
terminal table only when requested. At an ordinary merge checkpoint, resolve
the base and run `merge-gate` without asking the user to select a level. For
execution or cleanup, lead with the outcome, report actionable findings, and
name every required check that remains unrun. Include raw CLI output only when
the caller requests it.
