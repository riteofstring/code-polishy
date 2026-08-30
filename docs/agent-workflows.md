# Agent Workflows

## Choose a workflow

Ordinary interactive work may use the caller's current checkout. The primary
agent owns task decomposition, subagent delegation, integration, and
verification.

Use `code-polishy task-session` when the caller requests isolation or when an
unattended bounded task benefits from a disposable worktree. Select every
allowed module and exact artifact path before the worker starts.

## Interactive work

Before changing governed source, run `code-polishy design-context --files` with
the exact planned paths, or `code-polishy design-context --module` with the
selected modules. Read only the returned current design documents. The command
does not select plans, historical evidence, or superseded decisions; open those
only when the task specifically requires them.

For ordinary Markdown-only work, run `code-polishy format --git-changes`, fix
documentation findings, and skip application tests without asking the user for
authorization. Run exact tests while editing source and
`code-polishy test --changed --base TASK_BASE` when broader feedback needs to
cover a completed task boundary. Without `--base`, changed-scope tests compare
the working tree with `HEAD`; with `TASK_BASE`, they compare
`merge-base(TASK_BASE, HEAD)` plus the working tree. On a long-lived branch,
finish each completed code-changing task with
`code-polishy checkpoint-gate --base <previous-checkpoint>` after committing
and preparing its behavior evidence. At a merge checkpoint, run one
`code-polishy merge-gate --base <merge-target>` for the unchanged final
candidate. Run `code-polishy test --supplemental` only when the caller or a
checked-in workflow requires that separate hardening stage. Conversational,
read-only, and status requests do not create checkpoints; an invoked
checkpoint with no changes is a no-op.

When added or modified test files are in the candidate, the default or
change-aware checkpoints show one prominent, non-blocking test-quality reminder.
Use it to check both new and existing tests for tautological and change-detector
behavior. It neither changes the selected work nor requests authorization; see
[Verification and Testing Policy](policies/verification.md#test-quality-reminder)
for its exact trigger and quiet modes.

## Gate evidence and retries

Checkpoint and merge gates keep terminal output short: phase progress, a final
result, and, on failure, a bounded tail with the managed log path. Each gate
that executes work writes a versioned JSON report and bounded command logs
below `.code-polishy-reports/<gate>/`. Use those files for durable evidence and
machine inspection instead of parsing terminal output.

A merge reminder always preserves the merge-target-wide changed-test count. If
a valid checkpoint receipt is bound to the candidate, it also names the latest
task slice and its base. This advisory data never changes merge selection.

Use `code-polishy merge-gate --base <merge-target> --resume` only to retry a
failed merge gate. It can reuse a successful ordinary test suite with a valid
receipt from the same content identity. Changes to the exact base, candidate,
release, configuration, command plan, platform, or declared command environment
prevent reuse. All non-test phases, behavior-proof replays, failed commands,
and commands without valid receipts run again; a normal merge gate does not
reuse prior work.

Commit all completed task-owned changes after required verification unless the
caller explicitly requests an uncommitted handoff. Keep each commit coherent
and free of unrelated user work. Push, publish, and pull-request operations
require the caller's explicit authorization.

## Agent reviews

A requested ordinary agent review binds to an explicit trusted base and exact
candidate. A dirty-worktree review covers committed, staged, unstaged, and
untracked changes so it cannot miss candidate state.

Keep structured evidence in the task's review record. Report actionable
findings or one concise no-findings outcome to the user; omit empty sections,
machine sentinels, and duplicates of automated checks. Distinguish repository
contract concerns from requested-outcome concerns and tie each finding to its
source instruction or objective and affected file or hunk. Agent review is
non-deterministic evidence and does not replace policy checks or human approval.

## Behavior regression review

Use the [Behavior Regression Review Policy](policies/behavior-review.md) for
every non-documentation checkpoint or merge candidate. It turns a clean-context
subagent review into gate-checkable evidence without making the subagent a
policy engine:

1. Commit the candidate and keep it clean, apart from the excluded review
   reports.
2. Run `code-polishy behavior-review prepare`. Start a review subagent with no
   inherited conversation and give it only the generated packet. If the harness
   cannot start subagents, use a separate clean AI invocation with only that
   packet.
3. For every behavior the review subagent classifies as `requested`, the primary
   agent runs one or more `code-polishy regression-proof` commands that fail on
   the declared pre-fix base and pass on the candidate. Run them after
   preparation and do not choose a pre-fix revision older than the packet's
   reviewed merge base.
4. Save the review subagent's strict result at the packet's result path, then run
   `code-polishy behavior-review finalize` to write the receipt.
5. Run `code-polishy checkpoint-gate --base <previous-checkpoint>` after a
   completed task on a long-lived branch, or
   `code-polishy merge-gate --base <merge-target>` for the final candidate.
   Either gate validates the receipt and independently reruns every cited
   proof. The checkpoint gate then runs changed-scope checks and focused tests
   and records the accepted HEAD; the merge gate runs its selected
   documentation, recommended, or full workflow. The report and logs record
   the executed phases separately from the behavior-review receipt.

Keep `.code-polishy-reports/behavior-review` in the same workspace throughout
the workflow. A multi-job CI run may transfer that directory only as an explicit
trusted artifact. Documentation-only candidates bypass the receipt; ordinary
agent reviews remain useful advisory evidence. Local digests do not authenticate
subagent identity or context; see the policy's trust limits.

## Isolated task sessions

A task session executes one worker command in a disposable Git worktree:

```sh
code-polishy task-session \
  --module application \
  --module documentation \
  --promote \
  -- WORKER [ARG...]
```

Before the worker starts, the session freezes the trusted base, caller-selected
edit boundary, exact release executable, worker command, and governed command
environment. It validates committed, staged, unstaged, deleted, renamed, and
untracked paths against that boundary.

The worker may use runner-native subagents. The primary worker owns their scope,
integration, commits, and quiescence. Every subagent operates in the same
worktree and task boundary. `CODE_POLISHY_TASK_SESSION=1` forbids nested task
sessions.

`--promote` fast-forwards the unchanged clean source branch only after the
worker exits successfully with a clean committed candidate inside the frozen
boundary. The session stores its receipt, command log, boundary report, and any
rejected patch outside the repository. An interruption or workspace-identity
failure retains the worktree for manual inspection.

Task sessions provide isolation and path enforcement. The worker remains
responsible for implementation quality, tests, and any requested independent
review.
