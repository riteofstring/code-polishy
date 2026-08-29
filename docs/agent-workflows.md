# Agent Workflows

## Choose a workflow

Ordinary interactive work may use the caller's current checkout. The supervising
agent owns task decomposition, native-subagent delegation, integration, and
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
`code-polishy test --changed` when broader feedback is useful. At a merge checkpoint, run one
`code-polishy merge-gate --base <merge-target>` for the unchanged final
candidate. Run `code-polishy test --supplemental` only when the caller or a
checked-in workflow requires that separate hardening stage.

When added or modified test files are in the candidate, the default or
change-aware checkpoints show one prominent, non-blocking test-quality reminder.
Use it to check both new and existing tests for tautological and change-detector
behavior. It neither changes the selected work nor requests authorization; see
[Verification and Testing Policy](policies/verification.md#test-quality-reminder)
for its exact trigger and quiet modes.

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

## Required behavior regression review

When a repository enables `verification.behaviorReview.required`, use the
[Behavior Regression Review Policy](policies/behavior-review.md) for every
non-documentation merge candidate. It turns a fresh semantic review into a
merge-checkable receipt without making the reviewer a policy engine:

1. Commit the candidate and keep it clean, apart from the excluded review
   reports.
2. Run `code-polishy behavior-review prepare` and give only its packet to a
   fresh native reviewer. The packet is that reviewer's complete authority, and
   the supervising runtime is responsible for enforcing fresh context.
3. For every behavior the reviewer classifies as `requested`, run one or more
   `code-polishy regression-proof` commands that fail on the declared pre-fix
   base and pass on the candidate. Run them after preparation and do not choose
   a pre-fix revision older than the packet's reviewed merge base.
4. Save the strict review result at the packet's result path, then run
   `code-polishy behavior-review finalize` to write the receipt.
5. Run `code-polishy merge-gate --base <merge-target>`. It validates the
   receipt and independently reruns every cited proof before its normal
   recommended or full work.

Keep `.code-polishy-reports/behavior-review` in the same workspace throughout
the workflow. A multi-job CI run may transfer that directory only as an explicit
trusted artifact. Documentation-only candidates bypass the receipt; ordinary
agent reviews remain available for work that does not opt in. Local digests do
not authenticate reviewer identity or history; see the policy's trust limits.

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

The worker may use runner-native subagents. The supervising worker owns their
scope, integration, commits, and quiescence. Every subagent operates in the same
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
