# Agent Workflows

Read this entry point from the locked release before changing the repository.
Use `./code-polishyw` for every `code-polishy` command below; in PowerShell use
`.\code-polishyw.ps1`. The wrapper selects the repository's exact release.
Read the conditional procedures only when their stated trigger applies.

## Start and complete ordinary work

1. Use the caller's checkout and preserve unrelated changes. Read-only questions
   need no intent capture, task session, or tests. Answer capability, footprint,
   and quality-trend questions with `capabilities --query "QUERY"`,
   `size [--base MERGE_TARGET]`, and `trends [--since DATE] [--branch NAME]`.
2. Start from a clean task base with `code-polishy task-start --files PATH...`
   or repeated `--module NAME` operands. Do not mix selector kinds. Retain its
   `TASK BASE` for task-scoped verification. If review is selected, rerun with
   `--intent-file -` and supply the exact request before editing. Optional tasks
   create no intent journal. Never recapture a request already captured.
3. Read the returned design documents, operational handoffs, and any additional
   selected workflow documents. Task-start satisfies design-context retrieval
   for that scope. Reuse it until scope, mappings, or relevant documents change;
   then refresh only the affected context with `design-context`.
4. Implement the simplest end-to-end fix for the demonstrated need. Add machinery
   only when it reduces total complexity and failure risk. Honor
   `quality.allowComments`. Update mapped rationale when ownership, invariants,
   dependency direction, or consequential tradeoffs change. Mapping gaps alone
   do not block routine edits or require a document for every module.
5. Verify according to the event table below. Complete any selected review using
   its conditional procedure. A selected review that lacks intent at the task
   base must stop and report that missing boundary, not reconstruct the request.
6. Commit task-owned progress at meaningful milestones, roughly every one to two
   hours during long work. Progress commits may be unfinished or failing; record
   what remains and verification status. Finish required verification and commit
   remaining task-owned changes before delivery unless the caller requested an
   uncommitted handoff. Public API cutovers must be coherent at merge or release.
   Push, publish, and pull-request operations require explicit authorization.

`task-start` prints scope-specific follow-ups rather than repeating this sequence.
Its complete packet remains available with `--format json`. It does not infer
features from prose or decide that an ordinary task is a merge checkpoint.
Select a behavior feature only by the caller's intended canonical name or exact
alias. See [task selection and intent](capabilities.md#start-a-task).

## Choose verification from the event

| Event                                                                                                | Action                                                                          |
| ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Read-only question, status request, checkout, fetch, clean merge or rebase, tag, or push preparation | None                                                                            |
| Ordinary prose-only Markdown change                                                                  | Format and documentation checks                                                 |
| Coherent runnable source change or manually resolved source conflict                                 | One affected exact test                                                         |
| Completed source task with no final gate immediately next                                            | `check --git-changes` before committing, then `test --changed --base TASK_BASE` |
| Completed source task on a long-lived branch                                                         | `checkpoint-gate --base PREVIOUS_CHECKPOINT` after commit and selected review   |
| Genuine merge or release checkpoint                                                                  | One `merge-gate --base MERGE_TARGET` through its configured owner               |
| Stable release candidate                                                                             | Only explicitly selected supplemental suites                                    |

Control Markdown and product-input Markdown follow source verification. A
prose-only conflict remains documentation-only. Do not test after every edit or
chat turn, or run changed-scope tests immediately before the same final gate.

Ordinary task completion, a requested commit, and delivery do not select a merge
gate. At a genuine merge or release checkpoint, honor
`verification.finalGateOwner`: `local` is the default; `ci` assigns that one
execution to CI. Duplicate it only when the caller requests independent evidence.
Resolve the merge target from an explicit target, checked-in guidance,
`origin/HEAD`, then `origin/main` or `origin/master`. Task-scoped commands keep the
recorded task base; they do not substitute the merge target automatically.

Supplemental suites require an explicit caller request, a checked-in workflow
for the event, or a release-checklist selection. Declarations, including
`tests.requiredSupplementalKinds`, never schedule them. Use
`test --supplemental --resume` for selected retries. Run all only without trusted
evidence or after shared infrastructure, toolchain, selection, or unbounded-impact
changes. Credentialed, destructive, or live-provider probes require named
external approval. See [verification and evidence](policies/verification.md).

## Read conditional procedures when selected

Use `code-polishy docs read TOPIC` for the exact release-owned document.
`docs find QUERY...` locates other policy references. Add `--situation NAME` to
`task-start` when the named situation actually applies; it selects guidance and
matching repository handoffs without authorizing their operations.

Published Python libraries use the [library dependency policy](policies/supply-chain.md#published-python-libraries):
declare exact manifests in `supplyChain.pythonLibraries`, keep consumer
compatibility ranges, and pair them with compatible exact development-group
pins and frozen locks. Build requirements and tool inputs remain exact.
Treat changes to compatibility metadata as package API changes.

| Trigger                                                                  | Topic and procedure                                                                                                                                                                                                         |
| ------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Dependency change or advisory investigation (`--situation dependencies`) | `supply-chain`: [dependency updates](policies/supply-chain.md#dependency-updates) and [security decisions](policies/supply-chain.md#dependency-security-decisions)                                                          |
| Adoption or upgrade (`--situation adoption` or `upgrade`)                | `ai-adoption`: [bootstrap](ai-adoption.md#bootstrap-a-clean-policy-base) and [upgrades](ai-adoption.md#upgrades)                                                                                                            |
| Release preparation (`--situation release`)                              | `release-checklist`: [release procedure](release-checklist.md)                                                                                                                                                              |
| Selected behavior review                                                 | `behavior-review`: [intent and corrections](policies/behavior-review.md#bind-a-task-request), then [review and proofs](policies/behavior-review.md#complete-a-selected-review)                                              |
| Requested agent or architecture review                                   | `agent-reviews`: [review boundaries](agent-reviews.md); `architecture`: [architecture review](policies/architecture.md#architecture-review-workflow)                                                                        |
| Selected reliability reminder                                            | `end-to-end-reliability`: [decision questions](policies/end-to-end-reliability.md)                                                                                                                                          |
| Matching repository operational handoff                                  | `operational-handoffs`: [context selection and authority](operational-handoffs.md)                                                                                                                                          |
| Genuine checkpoint, gate retry, or evidence transfer                     | `verification`: [checkpoints](policies/verification.md#verification-checkpoints), [receipts](policies/verification.md#reusable-test-evidence), and [retry rules](policies/verification.md#gate-reuse-and-failed-run-resume) |
| Requested isolation or unattended work                                   | `task-sessions`: [scope and lifecycle](task-sessions.md)                                                                                                                                                                    |

During upgrades, outgoing guidance governs until the exact verified incoming
release atomically rewrites the lock. Only that incoming lock mutation is allowed
before cutover. Incoming guidance applies immediately afterward; follow the
upgrade procedure for any newly selected review requirement.
