# Agent Workflows

Read this guide from the repository's locked release with:

```sh
code-polishy docs read agent-workflows
```

Use `code-polishy docs find QUERY...` to locate another exact policy reference.

## Choose a workflow

| Task                                   | First command                                                                |
| -------------------------------------- | ---------------------------------------------------------------------------- |
| Read-only capability question          | `code-polishy capabilities --query "QUERY"`                                  |
| Ordinary implementation                | `code-polishy task-start --intent-file PATH --module NAME`                   |
| Explicit behavior-sensitive change     | `code-polishy task-start --intent-file PATH --module NAME --feature FEATURE` |
| Requested isolation or unattended work | `code-polishy task-session --module NAME -- WORKER ARGS...`                  |
| Dependency change                      | `code-polishy docs read supply-chain`                                        |
| Release or upgrade                     | `code-polishy docs read release-checklist`                                   |
| Final delivery                         | `code-polishy behavior-review status --base REVIEW_BASE`                     |

Use one file or directory operand with `--files PATH` instead of `--module NAME`
when that identifies the task scope. A successful `task-start` already captures
the request and returns its current design context and operational handoffs;
read that packet before editing and do not recapture the same request through
the component command. The first command does not replace selected reviews or
event-required verification. Read-only
questions require no capture or tests. For delivery, follow the status and
the configured final-gate owner. Capability queries identify candidates only;
explicit feature operands require the caller's intended canonical name or
exact declared alias. See [Capability Discovery](capabilities.md).

Ordinary interactive work may use the caller's current checkout. The primary
agent owns task decomposition, subagent delegation, integration, and
verification.

Use `code-polishy task-session` when the caller requests isolation or when an
unattended bounded task benefits from a disposable worktree. Select every
allowed module and exact artifact path before the worker starts.

## Interactive work

When adopting Code Polishy or restructuring architecture, capture the request
and read its current design context before drafting module ownership. Automated
ownership, import, cycle, and dependency checks still apply. Architecture agent
review is optional: run `code-polishy architecture-review prepare --base
REVIEW_BASE` only when the caller explicitly requests that review. Supply the
packet to a clean-context reviewer and finalize its result. Ordinary checkpoints
and merge gates do not require or consume architecture-review receipts. See the
[Architecture Policy](policies/architecture.md#architecture-review-workflow).

Before changing governed source, run `code-polishy design-context --files` with
the exact planned paths, or `code-polishy design-context --module` with the
selected modules. Read only the returned current design documents. The command
does not select plans, historical evidence, or superseded decisions; open those
only when the task specifically requires them.

Retrieve context once for the planned scope. Context already supplied by
`task-start` for that scope satisfies this requirement. Reuse what you have
read until the scope, design mappings, or relevant document contents change.
Another edit, test run, status request, or commit does not require another
lookup. Refresh the affected context when changing those inputs.

If no useful rationale is mapped, inspect the selected module boundaries and
existing current documentation. During adoption or changes to ownership,
invariants, dependency direction, or consequential tradeoffs, create or update
the relevant repository-owned rationale and its `documentation.design`
mapping as part of the work. Explain decisions that source cannot readily
convey; do not invent decisions or manufacture a document for every module.
Shared guidance is appropriate for shared constraints. An empty mapping alone
does not block routine work or require boilerplate. Keep mapped rationale
current when the design changes; the maintainer need not request this upkeep
in each task.

Design-context explains each module or exact-source match and identifies
unmapped selected paths and modules, including partial module coverage. Module
guidance remains relevant when an additional source-specific document matches.
The JSON report's `repositoryContext.designResolution` contains the complete
matches, selection counts, and coverage gaps; the human output abbreviates long
path lists. Task-start includes that same resolution and adds an action to
consider missing rationale when gaps exist. Empty scope, unmapped work, and
invalid selected documents are distinct outcomes. A lookup ignores unrelated
stale documents; doctor validates the complete mapping inventory.

The same context command selects relevant repository operational handoffs from
`documentation.handoffs`. Read each selected procedure before its associated
operation. Add an exact `--situation authentication`, `--situation release`, or
`--situation deployment` when that operational situation applies; repositories
may declare other exact identifiers. File and module triggers select matching
handoffs automatically. Context lists their paths and selection reasons, while
`--format json` includes their bounded contents and SHA-256 identities. An
invalid selected handoff blocks context composition. Discovery does not execute
procedure commands, obtain credentials, or grant approval. Keep managed
`AGENTS.md` canonical; see [Operational Handoffs](operational-handoffs.md).

Before implementing a non-documentation request, have the harness save the
user's original request and supplied acceptance criteria to a bounded UTF-8
file, then run this command from the clean task-base commit:

```sh
code-polishy behavior-review capture-intent --intent-file PATH
```

Code Polishy copies that text into its managed journal. If implementation has
already started without a capture at the task base, stop and report the missing
boundary instead of writing a new summary of the request.

`task-start --intent-file PATH` with one file, directory, or module selector
performs this same capture after validating its complete bounded context
packet. Use either capture entry point once for each exact request or later
correction; see [Capability Discovery](capabilities.md#start-a-task).

An upgrade has one explicit authority transition. The outgoing locked release
and its installed guidance govern until the exact verified incoming release's
`lock` command atomically replaces `.code-polishy.lock.json`. That command may
run while the outgoing lock is active and is the only incoming mutation allowed
before the cutover. Incoming guidance governs immediately afterward. If the
outgoing release had no intent-capture requirement, capture the caller's exact
upgrade request from the new clean lock commit before making any further target
change.

When the user corrects the implementation, capture that exact correction before
acting on it. This later append may run while candidate files are staged,
unstaged, deleted, or untracked. It records the current HEAD and a candidate
state digest; it still runs no tests or AI review.

Choose verification from the event that actually changed risk:

| Event                                                     | Action                                            |
| --------------------------------------------------------- | ------------------------------------------------- |
| Read-only, conversational, or status request              | None                                              |
| Ordinary prose-only Markdown change                       | Format and documentation checks only              |
| Checkout, fetch, clean merge or rebase, tag, or push prep | None                                              |
| Manually resolved source conflict                         | One affected exact test                           |
| Coherent runnable source change                           | One affected exact test                           |
| Completed source task with no final gate next             | `test --changed --base TASK_BASE`                 |
| Final candidate                                           | One base-aware merge gate, owned locally or by CI |
| Stable release candidate                                  | Only explicitly selected supplemental suites      |

Use the first applicable row. Making a progress commit does not itself select
tests, a review, or a checkpoint gate. A conflict resolution is a new change
only for the files edited to resolve it; a prose-only conflict stays
documentation-only.
Do not run `test --changed` immediately before a final merge gate over the same
candidate because the gate already selects changed-impact tests.

Without `--base`, changed-scope tests compare the working tree with `HEAD`;
with `TASK_BASE`, they compare `merge-base(TASK_BASE, HEAD)` plus the working
tree. On a long-lived branch, finish each completed code-changing task with
`code-polishy checkpoint-gate --base <previous-checkpoint>` after committing
and completing any selected behavior review. At a genuine merge checkpoint,
run one `code-polishy merge-gate --base <merge-target>` for the unchanged final
candidate. Local and CI execution need separate final gates only when the caller
explicitly requests independent evidence. Honor `verification.finalGateOwner`:
`local` is the default, while `ci` means the checked-in workflow owns that one
final execution.

A stable release candidate is the exact committed tree intended for tagging,
with ordinary verification green and no planned source or policy edits. Run
`code-polishy test --supplemental` only when the caller explicitly requests it,
a checked-in workflow invokes it for that event, or the release checklist
selects it. Declarations, including `tests.requiredSupplementalKinds`, do not
schedule execution. After a supplemental failure, rerun only failed suites and
passes invalidated by changes to their tested production files or tests, or
their own commands or configuration. Exact `test --suite` evidence composes
with still-valid passes. Repeat every suite only when shared mutation
infrastructure, toolchain, or selection changes, or impact cannot be bounded.
Ordinary development, gates, guidance synchronization, and lock upgrades leave
unselected supplemental hardening `NOT RUN`.

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

An accepted checkpoint receipt binds the exact passed run identity, execution,
and report digest. It remains valid only while that report is the current,
fully validated checkpoint report. Receipt or report publication failures are
operational failures and cannot leave readable checkpoint acceptance.

A merge reminder always preserves the merge-target-wide changed-test count. If
a valid checkpoint receipt is bound to the candidate, it also names the latest
task slice and its base. This advisory data never changes merge selection.

An identical passed merge-gate identity reports `already-passed` and executes no
validation commands. A new gate automatically reuses successful suite receipts
only when the suite explicitly declares `reusable: true`, its sealed read-only
execution view was enforced, and its complete release, platform, toolchain,
command, configuration, environment, ownership, and file-input identities still
match. All non-test phases, behavior-proof replays, failed commands, and
non-reusable suites execute.

Use `code-polishy merge-gate --base <merge-target> --resume` only to retry an
otherwise-identical failed merge gate. It can additionally resume successful
ordinary suites from that failed report. For an explicitly selected
supplemental retry, use `code-polishy test --supplemental --resume`.

CI may export current unexpired local receipts and import one bundle whose
SHA-256 arrives through a trusted CI boundary:

```sh
code-polishy test-receipts export --output /tmp/test-receipts.json
code-polishy test-receipts import \
  --source /tmp/test-receipts.json \
  --sha256 <trusted-bundle-sha256>
```

The bundle composes exact evidence; it does not aggregate partial shards or
turn an incompatible receipt into a pass.

Commit task-owned progress at meaningful milestones, such as finishing a
subtask or reaching a useful stopping point before switching focus. During
long tasks, aim for a checkpoint roughly every one to two hours of active
editing. Use judgment about the boundary; do not create a commit for every
small edit or let hours of accumulated changes wait for the entire goal to
finish.

A progress commit may contain unfinished work or known failures. State what it
captures, what remains, and which checks passed, failed, or have not run in its
message. Commit related work together and exclude unrelated user changes.
Verification follows the events above; a progress commit does not require a
new test run, a clean full suite, a review, or a gate. Continue any verification
already required by the work. The checkpoint gate applies when a code-changing
task is complete, not to every progress commit within that task.

Atomic public API cutovers must be coherent at merge or release. Intermediate
branch commits may record incomplete implementation without adding temporary
compatibility code merely to make each checkpoint complete. Before final
delivery, finish required verification and commit remaining task-owned changes
unless the caller explicitly requests an uncommitted handoff. Push, publish,
and pull-request operations require the caller's explicit authorization.

## Agent reviews

Agent reviews report only major or severe, evidence-backed defects. Do not turn
nitpicks, preferences, optional refactors, minor prose issues, or speculative
concerns into blocking findings. Behavior/final-state review retains its existing
explicit-request and repository-configuration opt-ins. Architecture review
requires an explicit caller request. Code Polishy does not launch review agents.

Perform one review for the selected request or configured review event. Present
unresolved findings and their consequences to the caller; do not automatically
repeat review after fixes, choose a different reviewer to obtain acceptance, or
start a new review cycle. Any follow-up requires explicit caller authorization
and stays within the requested scope. A required behavior review remains failed
until resolved; stopping iteration never fabricates acceptance or waives checks.

A requested ordinary agent review binds to an explicit trusted base and exact
candidate. A dirty-worktree review covers committed, staged, unstaged, and
untracked changes so it cannot miss candidate state.

Keep structured evidence in the task's review record. Report actionable
findings or one concise no-findings outcome to the user; omit empty sections,
machine sentinels, and duplicates of automated checks. Distinguish repository
contract concerns from requested-outcome concerns and tie each finding to its
source instruction or objective and affected file or hunk. Agent review is
non-deterministic evidence and does not replace policy checks or human approval.

## Behavior and final-state review

Use the [Behavior and Final-State Review Policy](policies/behavior-review.md) when a
repository rule or explicit task request selects review. Optional review that
was skipped reports `NOT RUN` and does not block. A selected clean-context
subagent review becomes gate-checkable evidence without making the subagent a
policy engine:

1. Before implementation, run `code-polishy behavior-review capture-intent`
   from the task base with the exact user request supplied by the harness. Run
   it again before acting on every later user correction. Later captures may
   occur while code is staged, unstaged, deleted, or untracked; Code Polishy
   binds each entry to the exact HEAD and a digest of that candidate state.
   Repeat `--feature` only for configured features the user explicitly named.
   Capture itself runs no tests or AI review.
2. If the user adds review coverage later, commit the clean candidate and run
   `code-polishy behavior-review require --base TASK_BASE --feature NAME`.
   Requirements are additive and cannot be removed. Never infer a feature from
   request keywords.
3. Commit the candidate, run the read-only behavior-review status command for
   `REVIEW_BASE`, and keep it clean apart from excluded review reports. If
   status is optional, continue to the gate without creating a packet.
4. When selected, run `code-polishy behavior-review prepare --base REVIEW_BASE`.
   Start a
   review subagent with no inherited conversation and give it only the generated
   packet. If the harness cannot start subagents, use a separate clean AI
   invocation with only that packet.
5. For every behavior the review subagent classifies as `requested`, the primary
   agent runs one or more `code-polishy regression-proof` commands that fail on
   the declared pre-fix base and pass on the candidate. Run them after
   preparation and do not choose a pre-fix revision older than the packet's
   reviewed merge base. Each behavior names its selected feature scope, and its
   proofs use only suites allowed by that scope.
6. The same reviewer checks observable behavior, durable prose, and executable
   correction residue. Every final-state finding must cite an exact packet path,
   line, patch hunk, and relevant intent ID. Save the strict result at the
   packet's result path, then run `code-polishy behavior-review finalize` to
   write the receipt. Any final-state finding blocks finalization.
7. Run `code-polishy checkpoint-gate --base <previous-checkpoint>` after a
   completed task on a long-lived branch, or
   `code-polishy merge-gate --base <merge-target>` for the final candidate.
   Either gate validates the receipt and independently reruns every cited
   proof. The checkpoint gate then runs changed-scope checks and focused tests
   and records the accepted HEAD; the merge gate runs its selected
   documentation, recommended, or full workflow. The report and logs record
   the executed phases separately from the behavior-review receipt.

Keep `.code-polishy-reports/behavior-review` in the same workspace from intent
capture through the gate. A multi-job CI run may transfer the complete
directory only as an explicit trusted artifact. Ordinary Markdown-only
candidates stay optional unless a task request or deliberately configured
feature selects them. Ordinary agent reviews remain useful advisory evidence.
Local digests do not authenticate the source of the request, subagent identity,
or subagent context; see the policy's trust limits.

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
