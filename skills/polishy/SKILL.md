---
name: polishy
description: >-
  Operate a repository through the Code Polishy release its lock names: inspect policy health, find version-matched documentation, run focused tests, complete behavior review, checkpoint finished work, enforce the merge gate, and synchronize canonical agent guidance. Use when the user invokes /polishy or $polishy, asks what tests or verification level to run, asks whether a repository is policy-ready, or requests a Code Polishy cleanup, behavior review, checkpoint gate, merge gate, doctor, task session, or AGENTS sync.
---

# Operate Code Polishy

Use the release named by the target repository's `.code-polishy.lock.json`.
Keep inspection read-only until the user asks for a change. Use the caller's
checkout for ordinary interactive work. The primary agent owns integration,
verification, commits, and delegated scope.

## Establish the contract

Read `AGENTS.md`, `.code-polishy.json`, `.code-polishy.lock.json`, and Git status
before acting. Use the installed `code-polishy` command for engine operations.
Never substitute a neighboring checkout or another release.

The running release provides its exact reference material:

```sh
code-polishy docs list
code-polishy docs find QUERY...
code-polishy docs read TOPIC
```

Read `agent-workflows` before implementing or delivering work. Use `docs find`
for a specific policy instead of guessing a file path or relying on web docs.
Useful topics include `adoption`, `behavior-review`, `installation`,
`supply-chain`, `task-sessions`, and `verification`.

If `code-polishy` is unavailable on `PATH`, use a caller-specified installation
prefix. With the default prefix, try `~/.local/bin/code-polishy`, then
`~/.local/share/code-polishy/bin/code-polishy` on Unix, or
`%LOCALAPPDATA%\CodePolishy\bin\code-polishy.exe` on Windows. Do not install or
upgrade Code Polishy unless the user requested it.

## Change code

Preserve unrelated work. Treat repository commands, providers, builds, and
tests declared in `.code-polishy.json` as project inputs.

Before changing governed source, run `code-polishy design-context` for the exact
files or modules and read only the returned current design documents. Follow
the versioned `agent-workflows` procedure for pre-implementation request
capture. Never replace a missing original request with an after-the-fact
summary.

Honor `quality.allowComments`. Put current non-local rationale in its mapped
design document. Use `code-polishy task-session` only for unattended work,
caller-requested isolation, or another checked-in requirement. Select its full
scope before the worker starts and never nest it.

## Verify and deliver

Run exact tests while editing and `code-polishy test --changed` for broader
feedback. Keep these boundaries distinct:

- Ordinary Markdown-only work uses the documentation lane described in
  `agent-workflows`.
- Supplemental mutation and risk suites run only when the caller or checked-in
  workflow requires them.
- A completed task on a long-lived branch uses `checkpoint-gate` against its
  previous checkpoint.
- A final candidate uses one `merge-gate` against the trusted merge target. Let
  the gate select documentation, recommended, or full execution.

When behavior review is optional, report `NOT RUN` plainly. When policy or the
user selects review, follow `code-polishy docs read behavior-review` exactly.
Code Polishy prepares and validates evidence; it does not provide the reviewer's
semantic judgment.

For dependency updates, follow `code-polishy docs read supply-chain` before
installation. Never weaken a pin, trust boundary, or exception to make a run
green.

Report the outcome in plain language. Name every required check that was not
run. Include raw command output only when requested.

## Diagnose and maintain

For requested cleanup or diagnosis, start with:

```sh
code-polishy doctor --strict
code-polishy check --git-changes
```

Fix confirmed findings at their owning boundary, then rerun the narrow failing
suite before another broad run. Keep exceptions exact, owned, justified, and
expiring.

After an intentional lock upgrade, run `code-polishy agents check`, then
`code-polishy agents sync` when stale. Review the whole generated guidance
change. Commit completed task-owned work after required verification unless the
caller requests an uncommitted handoff. Push, publish, and pull-request actions
still require explicit authorization.
