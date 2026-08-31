# Behavior Regression Review

Behavior regression review is an optional AI-assisted evidence workflow. Code
Polishy selects its scope, prepares a bounded packet, validates a strict result,
records red/green proofs, and replays those proofs at a gate. It does not call an
AI provider or claim deterministic semantic regression detection.

Every base-aware plan, checkpoint gate, and merge gate reports exactly one
state:

```text
BEHAVIOR REVIEW: NOT RUN (optional)
BEHAVIOR REVIEW: REQUIRED (checkout, authentication)
BEHAVIOR REVIEW: PASSED (checkout)
BEHAVIOR REVIEW: FAILED (checkout)
```

`NOT RUN` is a successful outcome when neither checked-in policy nor the bound
task requested review. `REQUIRED` and `FAILED` block before expensive ordinary
gate commands.

## Configure review policy

Omitting `verification.behaviorReview` keeps review optional. A present policy
defaults to `defaultRequiredAt: on-request`:

```json
{
  "verification": {
    "behaviorReview": {
      "defaultRequiredAt": "on-request",
      "features": [
        {
          "name": "checkout",
          "modules": ["checkout", "payments"],
          "paths": ["web/checkout/**"],
          "suites": ["checkout-contract"],
          "requiredAt": "merge"
        },
        {
          "name": "authentication",
          "modules": ["auth"],
          "suites": ["auth-contract"],
          "requiredAt": "checkpoint"
        }
      ]
    }
  }
}
```

The persistent levels are ordered `on-request < merge < checkpoint`:

- `on-request` selects nothing automatically. A user may request configured
  features for one bound task.
- `merge` requires each affected feature once against the final merge base.
- `checkpoint` requires each affected feature at every completed checkpoint
  and again at final merge.

A feature can strengthen the repository default and cannot weaken it. Its
lowercase unique `name` identifies it in commands and reports. `modules`
references declared project modules; `paths` contains repository-relative path
patterns. At least one module or path is required. `suites` names one or more
configured ordinary suites.

Feature suites must work in disposable worktrees without credentials or secret
environment. Supplemental, mutation, live, destructive, credentialed, and
environment-dependent suites cannot be behavior-review suites or proofs.
Commands remain owned by `tests.suites`.

Code Polishy selects every matching feature and deduplicates shared suites. A
changed module also affects features owned by its reverse dependents. Deleted
paths retain their previous ownership. Base and candidate policies are unioned,
so editing or removing candidate policy cannot weaken an applicable base rule.
An explicit task request selects its features even when path matching does not.

To require a full-candidate review for every non-documentation merge, use:

```json
{
  "verification": {
    "behaviorReview": {
      "defaultRequiredAt": "merge",
      "features": []
    }
  }
}
```

Using `checkpoint` instead also requires full-candidate review at every changed
checkpoint. Ordinary Markdown-only candidates stay in the documentation lane
unless a task explicitly requests a feature or a configured feature deliberately
includes that product input.

## Bind a task request

The harness should preserve the user's exact request before implementation even
when review is currently optional. Intent capture is cheap: it runs no tests,
launches no reviewer, and creates no review packet.

From the clean task-base commit:

```sh
code-polishy behavior-review capture-intent --intent-file PATH
```

To request configured features immediately, repeat `--feature`:

```sh
code-polishy behavior-review capture-intent \
  --intent-file PATH \
  --feature checkout \
  --feature search
```

If the user asks for more coverage after implementation, first commit the clean
candidate, then append a requirement tied to the original intent and task base:

```sh
code-polishy behavior-review require \
  --base TASK_BASE \
  --feature authentication
```

Requirement records are additive. Repeating the command produces a stable
union; there is no remove or replace operation. Unknown features, an unrelated
base, a dirty candidate, edited journal records, or missing pre-code intent fail
explicitly. Code Polishy never guesses feature names from request prose and
never accepts an after-the-fact paraphrase as the original intent.

Inspect the decision without creating a packet or running commands:

```sh
code-polishy behavior-review status --base TASK_BASE
```

The status reports configured, affected, task-requested, required, completed,
and missing features.

## Complete a selected review

When status or a gate says review is required:

1. Commit the candidate and keep tracked, staged, unstaged, and untracked
   candidate paths clean. Managed report files are excluded from the candidate.
2. Prepare the selected packet:

   ```sh
   code-polishy behavior-review prepare --base REVIEW_BASE
   ```

   Preparation resolves the exact base and clean candidate, selects the
   applicable base policy, candidate policy, and task requirements, then writes
   `.code-polishy-reports/behavior-review/packet.json` and `prepare.json`.
   Their digests bind the selected feature definitions and reasons, requirement
   snapshot, captured intents, patch, mapped current design documents, and
   canonical review instructions.

3. Start a fresh review subagent with no inherited conversation and give it
   only the generated packet. If the harness cannot start subagents, use a
   separate clean AI invocation with only that packet. The reviewer must not
   inspect the workspace, parent conversation, plans, prior reviews, or external
   context.
4. For each material observable behavior, the reviewer records `requested`,
   `preserved`, `unintended`, or `unknown` and scopes it either to one or more
   selected features or to the explicit full candidate. A requested behavior
   needs at least one proof ID.
5. After packet preparation, create each proof:

   ```sh
   code-polishy regression-proof \
     --base PRE_FIX \
     --suite checkout-contract \
     --evidence path/to/behavior_test.go \
     --id checkout-submit
   ```

   `PRE_FIX` must be an ancestor of the candidate at or after the packet's
   reviewed merge base. Repeat `--evidence` as needed. Use `--red-exit STATUS`
   only when the expected baseline failure is not status `1`. A feature-scoped
   behavior may cite only suites declared by that feature. Full-candidate
   behavior may use any eligible ordinary suite.

6. Save the reviewer's exact JSON object at the packet's `result_path`, then
   finalize it:

   ```sh
   code-polishy behavior-review finalize --base REVIEW_BASE
   ```

   Finalization re-derives all bound material, validates behavior scopes and
   proofs, and atomically writes
   `.code-polishy-reports/behavior-review/receipt.json`.

7. Run the applicable gate with the same base:

   ```sh
   code-polishy checkpoint-gate --base PREVIOUS_CHECKPOINT
   code-polishy merge-gate --base MERGE_TARGET
   ```

   The gate independently validates the exact selected receipt, replays every
   cited red/green proof, forces selected feature suites into the ordinary test
   plan without duplicate execution, and records the structured outcome in its
   versioned gate report.

The result is strict JSON. It includes the packet's `review_id`, `base`,
`candidate`, `intent_sha256`, `selection_sha256`, and `decision_sha256`. Each
behavior has an explicit scope:

```json
{
  "version": 3,
  "review_id": "the packet review_id",
  "base": "the packet base",
  "candidate": "the packet candidate",
  "intent_sha256": "the packet intent_sha256",
  "selection_sha256": "the packet selection_sha256",
  "decision_sha256": "the packet decision_sha256",
  "behaviors": [
    {
      "before": "observable behavior before the candidate",
      "after": "observable behavior after the candidate",
      "classification": "requested",
      "proof_ids": ["checkout-submit"],
      "scope": {
        "features": ["checkout"],
        "full_candidate": false
      }
    }
  ],
  "findings": []
}
```

For full-candidate scope, use an empty `features` array and
`"full_candidate": true`. Unresolved findings, `unintended`, `unknown`, stale
bindings, an omitted selected feature, missing proof, unsuitable proof suite, or
a changed candidate stops finalization.

## Checkpoints, fixes, and final merge

A merge-only feature does not require a checkpoint review. A checkpoint feature
requires review against every completed checkpoint base and against the final
merge target. The final merge always recalculates impact and requirements from
the real merge base; a branch-checkpoint receipt cannot satisfy a different
base.

If review finds an unintended behavior, fix it, commit the new candidate, and
repeat preparation, review, proof, and finalization. Candidate changes invalidate
the previous packet and receipt. Code Polishy does not automatically fix the
regression.

Resume never reuses proof replay. Ordinary commands retain their exact-identity
reuse rules.

## Evidence and trust boundary

The intent journal, lock, packet, preparation marker, result, review, proof
records, bounded logs, and receipt live below
`.code-polishy-reports/behavior-review`. Keep that complete directory in one
workspace through the gate. A multi-job CI workflow may transfer it only as an
explicit trusted artifact. Checkpoint and merge reports live in their own
directories below `.code-polishy-reports`.

Each captured request and the canonical reviewer instructions are limited to
64 KiB. The journal permits at most 128 intent entries and 128 additive
requirement entries within 4 MiB. Results and mapped design documents are
limited to 256 KiB; artifact reads are limited to 8 MiB. Inputs must be regular,
contained, bounded UTF-8 files. Artifact writes are atomic and concurrent
journal appends use an interprocess lock.

Digests and re-derivation detect stale or partially edited evidence; they are not
signatures. The harness remains responsible for authentic request capture,
reviewer isolation, and provider custody. An AI reviewer can miss a regression
or raise a false alarm. The receipt proves deterministic enforcement of the
recorded review decision and evidence, and does not replace ordinary policy
checks, configured tests, human approval, or supplemental test-strength work.
