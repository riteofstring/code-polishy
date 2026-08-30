# Behavior Regression Review

Every non-documentation checkpoint and merge gate uses a behavior review
subagent. The primary agent is the agent implementing the change. Code Polishy
does not launch AI agents; it prepares the exact packet, records red/green
proofs, validates the structured result, and replays every cited proof whenever
either gate runs.

Ordinary agent reviews remain useful advisory evidence and can inspect a dirty
working tree; this workflow is deliberately bound to one clean committed
candidate.

## When it runs

The receipt is required for every non-documentation `checkpoint-gate` and for
recommended and full `merge-gate` candidates. An unchanged checkpoint is a
no-op. Ordinary Markdown-only candidates run the documentation contract and
bypass behavior review.

## Required workflow

1. Choose the task base before implementation. For a long-lived branch task,
   this is the previous accepted commit. The repository must be clean and
   committed at that exact starting point.
2. Have the agent harness write the user's original request and any supplied
   acceptance criteria to a non-empty regular UTF-8 file. Capture it before
   changing code:

   ```sh
   code-polishy behavior-review capture-intent --intent-file PATH
   ```

   The command copies the exact bytes into the managed intent journal and binds
   that entry to the current commit. It rejects a dirty repository. Do not
   recreate or paraphrase the request after implementation. If the user adds a
   separate request later, reach a clean committed boundary and capture that
   request before implementing it.

3. Implement the request, then commit the candidate. Its tracked, staged,
   unstaged, and untracked candidate files must be clean. The excluded report
   directory is the only workflow output that may remain untracked.
4. Prepare the packet against the task base:

   ```sh
   code-polishy behavior-review prepare --base REVIEW_BASE
   ```

   Preparation selects the captured requests from the reviewed history,
   resolves the base, freezes the exact candidate, and writes
   `.code-polishy-reports/behavior-review/packet.json` plus `prepare.json`.
   Missing or after-the-fact intent capture fails instead of accepting a new
   account of what the user asked for.

5. Start a review subagent with no inherited conversation and give it only the
   generated packet. If the harness cannot start subagents, use a separate clean
   AI invocation with only that packet. The review subagent must not inspect the
   current workspace, parent conversation, prior reviews, plans, or external
   context. The packet contains the ordered original requests, resolved base and
   candidate, binary-safe Git patch, current mapped design documents, a random
   review ID, and the canonical review instructions.
6. The review subagent describes every material observable behavior as
   `requested`, `preserved`, `unintended`, or `unknown`. For every `requested`
   behavior, the primary agent creates at least one proof ID:

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

   After a proof exists, the review subagent may read only that proof's JSON
   record and named logs below the packet's proof directory.

   Proof suites execute from disposable Git worktrees. Their commands must use
   checked-in inputs and executables supplied by the governed command
   environment. An untracked dependency or tool directory that exists only in
   the caller's checkout is unavailable inside a proof worktree.

7. Save the review subagent's exact JSON result at the packet's `result_path`,
   with no surrounding prose, and finalize it:

   ```sh
   code-polishy behavior-review finalize --base REVIEW_BASE
   ```

   Finalization re-derives the patch, instructions, and mapped design documents
   from the bound revisions; validates the preparation marker, result,
   candidate, base, and cited proofs; then atomically writes
   `.code-polishy-reports/behavior-review/receipt.json`.

8. Run the applicable gate with that same base:

   ```sh
   code-polishy checkpoint-gate --base REVIEW_BASE
   # Or, for the final candidate:
   code-polishy merge-gate --base REVIEW_BASE
   ```

   Run only the command appropriate to the checkpoint. For a non-documentation
   candidate, either gate validates the receipt and reruns every cited proof in
   disposable baseline and candidate worktrees before further verification.
   Recorded success or edited logs alone cannot satisfy the gate.

## Long-lived branch checkpoints

Do not wait for a merge into the main branch when an AI is making a sequence of
changes on one long-lived branch. Capture each new request at the previous
accepted commit before implementing it. After implementation, use that commit
as the base and run the same preparation, review, proof, finalization, and
checkpoint workflow:

```sh
code-polishy checkpoint-gate --base PREVIOUS_CHECKPOINT
```

The command runs changed-scope policy checks and focused tests after behavior
evidence passes. It prepares the exact passed run report, writes
`.code-polishy-reports/checkpoint-gate/receipt.json` bound to that report's
identity, execution, and digest, then publishes the report. The receipt is
accepted only while that exact passed report remains current. A receipt or
report publication failure is operational and leaves no readable acceptance.
When the base yields no governed candidate paths, the command reports an
unchanged no-op and writes nothing. This makes an always-invoked wrapper
harmless after a conversational or read-only request, although agent guidance
should invoke the command only after a completed committed task.

Use the accepted candidate commit as the next task's `PREVIOUS_CHECKPOINT`. The
checkpoint receipt records that fact for audit; the next invocation still
requires an explicit base. The final merge checkpoint must repeat preparation,
review, proof, and finalization against the actual merge target before
`merge-gate`. Preparation selects the ordered request entries beginning at that
merge base, so a long-lived branch is reviewed against all captured tasks. A
receipt bound to a branch checkpoint cannot satisfy a gate against a different
base.

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
the same suite on the candidate, which must exit `0`. Both checkpoint and merge
gates independently repeat both executions from the recorded patch and
configured suite. The source candidate and replay candidate must remain the
same clean commit throughout. Failed patch application, timeout, invalid
evidence, candidate mutation, or worktree cleanup failure is an error, never
proof.

## Artifact boundary and limits

The intent journal, preparation marker, packet, result, proof, log, worktree,
and receipt artifacts live below `.code-polishy-reports/behavior-review`. The
directory is excluded from candidate selection, so report updates cannot change
merge classification. Keep it in the same workspace from intent capture through
the applicable gate. A multi-job CI workflow may transfer the complete
directory only as an explicit trusted artifact, preserving the exact candidate
and base it names. The accepted checkpoint receipt lives separately below
`.code-polishy-reports/checkpoint-gate`; both report directories are excluded
from candidate selection. `agents install` and `agents sync` maintain the exact
root `.gitignore` rule that keeps the shared report root workspace-local.

Each captured request and the subagent instructions are limited to 64 KiB. The
strict journal allows at most 128 entries and 4 MiB. A review result and each
mapped design document are limited to 256 KiB; artifact reads are limited to
8 MiB. Request input must be a non-empty regular UTF-8 file; artifact targets
and evidence paths remain contained. These limits keep the review packet
bounded and prevent artifacts from becoming a second ungoverned source tree.

The harness owns capture timing and source fidelity. Code Polishy binds the
captured bytes and their commit boundaries through preparation, finalization,
and gate replay. Digests and re-derivation detect stale or individually edited
artifacts; they are not signatures. Code Polishy cannot authenticate which user
or provider supplied the captured text, which model or session wrote the
review, prove that the review subagent started without inherited context, or
stop a writer with artifact access from replacing a complete self-consistent
artifact set. The agent harness and CI custody boundary must enforce those
parts.

The receipt adds gate evidence. It does not replace ordinary policy checks,
configured tests, human approval, or separate supplemental test-strength work.
