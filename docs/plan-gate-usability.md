# Gate Usability and Reliability Implementation Plan

## Objective

Address the Code Polishy 0.19 usability problems recorded in
`../code-polishy-gripes.md` without weakening gate scope or behavior-regression
evidence. The completed release must make command discovery obvious, disclose
the exact changed-test comparison, keep long gate output bounded, retain
machine-readable evidence, classify failures only from observed facts, show a
useful task-sized test reminder on long-lived branches, and safely reuse
eligible successful test phases after a failed merge gate.

## Product contract

### Contextual help

- `code-polishy COMMAND --help` and `code-polishy COMMAND -h` exit successfully
  without opening a repository or running policy.
- `code-polishy help COMMAND` prints the same command-specific text.
- Each help page states syntax, selectors, required arguments, side effects,
  exit statuses, and short examples.
- Invalid options and missing selectors include the relevant usage line and a
  likely correction when one is unambiguous.

### Explicit changed-test scope

- Every changed-scope test invocation reports whether it compares the working
  tree with `HEAD` or uses `merge-base(REF, HEAD)` plus the working tree.
- The report carries the requested base, resolved exact base, candidate, and
  governed path count as structured data. Default human output prints one
  concise scope line; verbose output may add exact details.
- Documentation and examples use `TASK_BASE` or generic `REF` when the base is
  a task boundary. `MERGE_TARGET` remains reserved for merge policy.

### Concise durable gate runs

- `checkpoint-gate` and `merge-gate` write managed artifacts below
  `.code-polishy-reports/<gate>/`: one versioned JSON run report and one bounded
  log per executed command.
- Gate commands print phase progress, the failure tail, the artifact path, and
  the final result. Full child output stays in the log. Direct implementation
  commands such as `test --suite` retain streaming output.
- The JSON report binds the gate kind, requested and exact base, candidate,
  policy level, release/config identity, command plan, attempts, reuse status,
  durations, log paths, findings, and final status.
- Artifact creation uses contained regular paths, restrictive permissions, and
  atomic replacement. A failed artifact write is an operational error, never a
  successful gate.

### Evidence-based failure metadata

- Command failures use closed categories derived from runner facts:
  `command-exit`, `timeout`, `canceled`, `environment`, `resource`, or
  `operational`.
- Test failures also report suite ownership, overlap with changed/impacted
  modules, exit status, attempt count, and log path.
- A candidate retry may establish `intermittent-observed`. A disposable exact-
  base replay may establish `candidate-regression` or `baseline-reproduced`.
  If either replay cannot run, the report says `baseline-unavailable`; it never
  guesses "flaky", "baseline", or "regression" from filenames or overlap.
- Diagnostic retries never turn the original failing gate into a pass.

### Task-aware changed-test reminder

- A checkpoint gate continues to show tests changed since its explicit previous
  checkpoint.
- A merge gate always preserves the merge-target-wide test count.
- When a valid checkpoint receipt is bound to the current candidate, the merge
  reminder also shows the latest task slice and its base. Missing, stale, or
  malformed checkpoint evidence is ignored for this advisory display and never
  changes merge selection.

### Safe merge-gate resume

- `merge-gate --base REF --resume` reuses only successful ordinary test-suite
  commands from a prior failed run with the same content-addressed run identity.
- The identity includes the exact merge base and candidate, locked Code Polishy
  release, loaded configuration, full planned command definition, platform, and
  declared command environment values.
- Checks, builds, supply-chain commands, artifact-security commands, behavior
  review proof replays, failed commands, and commands without valid receipts run
  again. Reuse never crosses candidate, base, policy, configuration, command, or
  environment changes.
- Each reusable receipt is an atomic content-addressed local digest, not an
  identity signature. CI may provide a stronger custody boundary around the
  report directory.
- A normal `merge-gate` run does not reuse receipts. Resume is explicit, reports
  every reused phase, and still performs final clean-candidate validation.

## Implementation ownership

1. CLI discovery and scope disclosure
   - Build one authoritative command-help catalog and route both help forms to
     it before repository initialization.
   - Add structured test-scope data to engine reports and render it in the CLI.
   - Cover every public command and targeted correction for selection mistakes.
2. Gate-run artifacts, concise output, and resume
   - Add a gate-run owner for secure artifact paths, canonical identities,
     command logs, JSON run reports, and reusable test receipts.
   - Wrap planned merge/checkpoint execution so gate output is captured without
     changing direct-test behavior.
   - Add strict resume parsing and validate every receipt before reuse.
3. Failure evidence and task reminders
   - Preserve typed runner results through testing and engine reports.
   - Add diagnostic retry/base replay with explicit unavailable states.
   - Read validated checkpoint evidence for the secondary merge reminder.
4. Integration and cutover
   - Generate machine output from typed report data rather than parsing terminal
     banners.
   - Remove superseded help fragments, ambiguous `MERGE_TARGET` examples for
     task tests, and any duplicate output paths or legacy wording.

## Verification

- Unit tests cover command-local help, correction messages, scope resolution,
  artifact containment, atomic writes, JSON schema shape, log truncation,
  receipt identity invalidation, eligible reuse, prohibited reuse, typed
  failure categories, diagnostic outcomes, and dual-scope reminders.
- CLI contract tests cover bounded output, stable artifact discovery, JSON
  failure/success reports, resume after a late failed suite, and rejection after
  candidate/config/command changes.
- Existing behavior-review tests prove every checkpoint and merge invocation
  still independently replays its regression proofs.
- Run exact affected package tests while integrating, then
  `code-polishy test --changed --base origin/main` and one final
  `code-polishy merge-gate --base origin/main`.

## Delivery

Commit the plan, each coherent implementation slice, the integrated behavior,
and the final documentation cleanup separately. After all verification passes,
delete this plan, update permanent docs and generated agent guidance, remove
superseded implementation and documentation, and leave a clean committed
worktree.
