# Behavior and Final-State Review

Behavior review is an optional AI-assisted evidence workflow. One isolated
reviewer checks observable behavior, durable prose, and executable correction
residue. Code Polishy selects the scope, prepares bounded evidence, validates a
strict result, records red/green behavior proofs, and enforces the result at a
gate. It does not call an AI provider or claim deterministic semantic judgment.

This workflow is experimental until its installed-release Unix and native
Windows acceptance contracts pass and real multi-repository dogfood meets the
release checklist.

Every base-aware plan, checkpoint gate, and merge gate reports one behavior
state and one final-state disclosure:

```text
BEHAVIOR REVIEW: NOT RUN (optional)
BEHAVIOR REVIEW: REQUIRED (checkout, authentication)
BEHAVIOR REVIEW: PASSED (checkout)
BEHAVIOR REVIEW: FAILED (checkout)
FINAL STATE: NOT RUN (optional)
FINAL STATE: NOT RUN (required)
FINAL STATE: PASSED (checkout)
FINAL STATE: FAILED (checkout)
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
          "description": "Checkout completion and payment confirmation.",
          "aliases": ["purchase completion"],
          "modules": ["checkout", "payments"],
          "paths": ["web/checkout/**"],
          "suites": ["checkout-contract"],
          "requiredAt": "merge"
        },
        {
          "name": "authentication",
          "description": "Sign-in, sign-out, and session behavior.",
          "aliases": ["sign in"],
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
lowercase unique `name` identifies it in commands and reports. Each feature
requires a trimmed `description` of at most 512 UTF-8 bytes. `modules`
references declared project modules; `paths` contains repository-relative path
patterns. At least one module or path is required. `suites` names one or more
configured ordinary suites.

A policy can declare at most 128 features. A feature may declare up to 16
`aliases`, each at most 256 UTF-8 bytes. Names and aliases must remain unique
after Unicode NFKC normalization, case folding, and collapsing Unicode
whitespace. The declared spelling is preserved for display. An explicitly
supplied `--feature` operand resolves by exact normalized name or alias and
records only the canonical name. For example, `--feature 'PURCHASE COMPLETION'`
selects `checkout` above. Partial matches, descriptions, rankings, and keywords
in intent prose never select a feature.

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

The harness should preserve the user's exact request before implementation and
append each later correction before acting on it, even when review is currently
optional. Intent capture is cheap: it runs no tests, launches no reviewer, and
creates no review packet.

Capture the original request at the task-base commit:

```sh
code-polishy behavior-review capture-intent --intent-file PATH
```

Run the same command with a new exact intent file before acting on each
correction. Correction capture may run while the worktree contains staged,
unstaged, deleted, or untracked candidate paths. Each append records the exact
text, current HEAD, a deterministic candidate-state digest, and the previous
journal digest under one lock. If candidate state changes during capture, the
append fails. Actual review preparation and finalization still require a clean,
committed candidate.

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
base, edited journal records, or missing pre-code intent fail explicitly.
`require` still needs a clean candidate. Code Polishy never guesses feature
names from request prose and never accepts an after-the-fact paraphrase as the
original intent.

Inspect the decision without creating a packet or running commands:

```sh
code-polishy behavior-review status --base TASK_BASE
```

Capture and status always print a confirmation on standard output, including
when piped, captured by an agent harness, or invoked through the installed
launcher. Capture reports the canonical requested features and managed journal
path. Status reports the review state, configured, affected, task-requested,
required, completed, and missing features, plus the accepted receipt path when
available. Status remains read-only.

Add `--format json` to either command for one `behavior-review/v1` document
with `action`, `state`, and the typed `capture` or `status` result. This replaces
the human confirmation on standard output and does not create a report file:

```sh
code-polishy behavior-review capture-intent --intent-file PATH --format json
code-polishy behavior-review status --base TASK_BASE --format json
```

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
   snapshot, captured intents, patch, mapped current design documents, canonical
   review instructions, path roles, stable patch-hunk IDs, and bounded source
   context.

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

6. The reviewer also checks changed durable prose for task or editing narration
   and changed executable code for rejected ideas left in guards, flags,
   fallbacks, wrappers, tests, names, configuration, or compatibility paths.
   Real security rules, external-input validation, current compatibility
   contracts, and explicitly requested rollouts remain valid. Each finding must
   cite an exact packet path, line, hunk digest, and any relevant captured intent
   IDs.
7. Save the reviewer's exact JSON object at the packet's `result_path`, then
   finalize it:

   ```sh
   code-polishy behavior-review finalize --base REVIEW_BASE
   ```

   Finalization re-derives all bound material, validates behavior scopes and
   proofs, and atomically writes
   `.code-polishy-reports/behavior-review/receipt.json`.

8. Run the applicable gate with the same base:

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
  "version": 4,
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
  "findings": [],
  "final_state_findings": []
}
```

For full-candidate scope, use an empty `features` array and
`"full_candidate": true`. Unresolved findings, `unintended`, `unknown`, stale
bindings, an omitted selected feature, missing proof, unsuitable proof suite,
any `meta-note`, `correction-residue`, or `unknown-final-state` finding, or a
changed candidate stops finalization.

A final-state finding uses this bounded shape:

```json
{
  "kind": "correction-residue",
  "path": "internal/soup/soup.go",
  "line": 42,
  "patch_hunk_sha256": "an exact packet hunk digest",
  "intent_ids": ["an exact captured correction ID"],
  "summary": "The rejected ingredient remains as a guard."
}
```

Code Polishy rejects unknown kinds, invented paths or lines, mismatched hunk
digests, unknown intent IDs, control characters, oversized text, and duplicate
findings. `unknown-final-state` blocks so the reviewer cannot silently guess
when packet context is insufficient.

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
limited to 256 KiB; artifact reads are limited to 8 MiB. Intent inputs must be
regular, contained, bounded UTF-8 files. Candidate snapshots bind staged and
unstaged patches plus deleted and untracked state without printing their
contents. Artifact writes are atomic and concurrent journal appends use an
interprocess lock.

Digests and re-derivation detect stale or partially edited evidence; they are not
signatures. The harness remains responsible for authentic request capture,
reviewer isolation, and provider custody. An AI reviewer can miss a regression
or raise a false alarm. The receipt proves deterministic enforcement of the
recorded review decision and evidence, and does not replace ordinary policy
checks, configured tests, human approval, or supplemental test-strength work.
