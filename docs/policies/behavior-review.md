# Behavior Regression Review

Behavior regression review is the default merge prerequisite for changes that
need both a fresh semantic review and executable regression evidence. The agent
runtime supplies the fresh reviewer. Code Polishy prepares the exact packet,
records red/green proofs, validates the structured result, and replays every
cited proof at the merge gate.

Ordinary agent reviews remain useful advisory evidence and can inspect a dirty
working tree; this workflow is deliberately bound to one clean committed
candidate.

## Default and explicit opt-out

No configuration is needed to require the receipt. This explicit setting has
the same behavior as omission:

```json
{
  "verification": {
    "behaviorReview": {
      "required": true
    }
  }
}
```

`required` defaults to `true` when either the property or the whole
`behaviorReview` object is omitted. A repository that cannot use this workflow
may explicitly opt out with `"required": false`. Malformed values and unknown
properties remain invalid configuration.

The receipt is required when either the resolved merge base or the candidate
requires it. A candidate therefore cannot weaken a required base by adding an
opt-out. The opt-out change itself needs one final valid receipt; after that
change reaches the trusted base, candidates that retain `false` are exempt.

The receipt is required for recommended and full merge candidates. Ordinary
Markdown-only candidates use the documentation merge level and bypass it.

## Required workflow

1. Stabilize and commit the candidate. Its tracked, staged, unstaged, and
   untracked candidate files must be clean. The excluded report directory is
   the only workflow output that may remain untracked.
2. Put the original request and acceptance criteria in a non-empty regular
   UTF-8 intent file, then prepare the packet:

   ```sh
   code-polishy behavior-review prepare \
     --base origin/main \
     --intent-file .code-polishy-reports/behavior-review/intent.txt
   ```

   The command resolves the base, freezes the exact candidate, and writes
   `.code-polishy-reports/behavior-review/packet.json` plus `prepare.json`,
   which binds that prepared packet to its review ID, base, and candidate.

3. Give only that packet to a fresh native reviewer. The supervising runtime
   must keep that reviewer from reading the current workspace, prior reviews,
   plans, or external context. The packet contains the original intent,
   resolved base and candidate, binary-safe Git patch, current mapped design
   documents, a random review ID, and the canonical review instructions. After
   a proof exists, the reviewer may read only that proof's JSON record and named
   logs below the packet's proof directory.
4. The reviewer describes every material observable behavior as `requested`,
   `preserved`, `unintended`, or `unknown`. For every `requested` behavior,
   create at least one proof ID:

   ```sh
   code-polishy regression-proof \
     --base PRE_FIX \
     --suite focused-suite \
     --evidence path/to/behavior_test.go \
     --id behavior-proof
   ```

   Run proofs only after packet preparation. `PRE_FIX` must be an ancestor of
   the candidate at or after the packet's reviewed merge base. Repeat
   `--evidence` for each evidence file. Use `--red-exit STATUS` only when the
   expected baseline failure is not status `1`.

5. Save the reviewer's exact JSON result at the packet's `result_path`, with no
   surrounding prose, and finalize it:

   ```sh
   code-polishy behavior-review finalize --base origin/main
   ```

   Finalization re-derives the patch, instructions, and mapped design documents
   from the bound revisions; validates the preparation marker, result,
   candidate, base, and cited proofs; then atomically writes
   `.code-polishy-reports/behavior-review/receipt.json`.

6. Run the normal merge checkpoint:

   ```sh
   code-polishy merge-gate --base origin/main
   ```

   For a non-documentation candidate, the gate validates the receipt and reruns
   every cited proof in disposable baseline and candidate worktrees before it
   runs ordinary recommended or full verification. Recorded success or edited
   logs alone cannot satisfy the gate.

## Long-lived branch checkpoints

Do not wait for a merge into the main branch when an AI is making a sequence of
changes on one long-lived branch. After each accepted task, use the previous
known-good commit as the base and run the same preparation, review, proof,
finalization, and gate workflow:

```sh
code-polishy merge-gate --base PREVIOUS_CHECKPOINT
```

Only record the current commit as the next checkpoint after that gate passes.
This keeps the review packet focused on the latest task and reruns durable
regression tests before the next change begins. The final merge checkpoint must
still repeat the workflow against the actual merge target; a receipt bound to a
branch checkpoint cannot satisfy a gate against a different base.

## What makes a result valid

The review result is strict JSON. It accepts no unknown fields and must bind the
packet's review ID, base, candidate, and intent digest. It must contain at least
one behavior, each with non-empty `before` and `after` descriptions, one allowed
classification, and an explicit proof-ID array. A `preserved` behavior needs no
red/green proof. Every `requested` behavior needs one or more valid proof IDs.

Unresolved findings, an `unintended` behavior, an `unknown` behavior, stale
identifiers, missing proof, or a changed candidate stops finalization. The
receipt records structured identifiers and digests, while prompts, patches, and
logs remain separate artifacts.

## What makes a proof valid

`regression-proof` resolves `--base` to one exact ancestor of the clean
candidate that is no earlier than the prepared review base. It binds the proof
to that review ID and candidate. Its named suite must be one configured ordinary
suite with no declared environment: supplemental, live, credentialed, and
destructive work cannot act as receipt evidence. Every evidence path must be
unique, contained, regular, and classified as development or test material.

Code Polishy builds a binary patch from only those evidence files, applies it in
a disposable worktree at the pre-fix base, and runs the suite there. That run
must exit with the expected non-zero status, which defaults to `1`. It then runs
the same suite on the candidate, which must exit `0`. The merge gate
independently repeats both executions from the recorded patch and configured
suite. The source candidate and replay candidate must remain the same clean
commit throughout. Failed patch application, timeout, invalid evidence,
candidate mutation, or worktree cleanup failure is an error, never proof.

## Artifact boundary and limits

All preparation-marker, packet, result, proof, log, worktree, and receipt
artifacts live below `.code-polishy-reports/behavior-review`. The directory is
excluded from candidate selection, so report updates cannot change merge
classification. Keep it in the same workspace for preparation, proof, review,
finalization, and merge-gate. A multi-job CI workflow may transfer it only as an
explicit trusted artifact, preserving the exact candidate and base it names.

Intent and reviewer-instruction inputs are limited to 64 KiB. A review result
and each mapped design document are limited to 256 KiB; artifact reads are
limited to 8 MiB. Intent is a non-empty regular UTF-8 file; artifact targets
and evidence paths remain contained. These limits keep the reviewer packet
bounded and prevent artifacts from becoming a second ungoverned source tree.

Digests and re-derivation detect stale or individually edited artifacts; they
are not signatures. Code Polishy can bind artifacts to exact Git revisions and
independently replay executable proofs. It cannot authenticate which model or
session wrote the review, prove that the reviewer had fresh context, or stop a
writer with artifact access from replacing a complete self-consistent artifact
set. The supervising runtime and CI custody boundary must enforce those parts.

The receipt adds merge evidence. It does not replace ordinary policy checks,
configured tests, human approval, or separate supplemental test-strength work.
