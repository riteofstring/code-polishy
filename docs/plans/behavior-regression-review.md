# Behavior Regression Review Plan

## Outcome

Add an opt-in merge safeguard that combines a fresh semantic review with
deterministic regression evidence. Code Polishy remains provider-neutral: the
agent runtime supplies a fresh reviewer, while Code Polishy owns the exact
candidate packet, red/green proof, structured result validation, and merge-gate
enforcement.

## User workflow

1. Stabilize and commit the candidate.
2. Run `code-polishy behavior-review prepare --base REF --intent-file PATH`.
3. Give only the generated packet to a fresh reviewer. The reviewer describes
   each material observable behavior before and after the change and classifies
   it as `requested`, `preserved`, `unintended`, or `unknown`.
4. For every `requested` behavior, create or identify the exact reproducer and
   run `code-polishy regression-proof --base PRE_FIX --suite NAME --evidence
PATH... --id ID`. The command applies only the declared evidence files to a
   disposable worktree at the pre-fix commit, requires the suite to fail there,
   and requires the same suite to pass on the candidate.
5. The reviewer records proof identifiers in the structured result and runs
   `code-polishy behavior-review finalize --base REF`.
6. When `verification.behaviorReview.required` is enabled, `merge-gate` rejects
   a missing receipt, a changed candidate, unresolved review findings, or a
   requested behavior without valid red/green proof.

All behavior-review artifacts live under the already excluded
`.code-polishy-reports/behavior-review` directory. Preparation, proof, review,
and merge-gate validation must run in the same workspace or move that directory
between trusted CI jobs as an explicit artifact.

## Contracts

### Candidate and intent

- Review operates only on a clean committed candidate. Existing ordinary agent
  review continues to support dirty worktrees; the enforceable merge receipt is
  deliberately bound to an immutable commit.
- `--base` resolves through the existing trusted merge-base contract.
- `--intent-file` is a caller-owned external input containing the original
  request and acceptance criteria. It must be a non-empty regular UTF-8 file of
  bounded size.
- The packet contains the exact resolved base, candidate commit, binary-safe Git
  patch, copied intent, currently mapped design documents, a random review ID,
  and reviewer instructions. Historical plans and evidence are excluded.

### Review result

- Strict JSON rejects unknown fields and stale review, base, intent, or
  candidate identifiers.
- Every behavior has non-empty `before`, `after`, and `classification` values.
- `unintended`, `unknown`, and explicit findings prevent finalization.
- Every `requested` behavior references at least one valid proof bound to the
  same candidate. `preserved` behaviors do not require red/green proof.
- A valid finalization writes one atomic receipt. The receipt records only
  structured evidence and digests; raw prompts, diffs, and command logs remain
  separate task artifacts.

### Regression proof

- `--base` resolves to one exact ancestor of the candidate and represents the
  pre-fix state.
- `--suite` names exactly one configured ordinary suite. Supplemental, live,
  credentialed, or destructive suites are rejected.
- Every repeated `--evidence` path is a contained, regular development or test
  file. The candidate version of those files is applied to the baseline
  worktree; production changes are never copied into the red state.
- The baseline run must exit with the explicitly expected non-zero status
  (default `1`), the candidate run must exit `0`, and the candidate must remain
  clean and unchanged throughout.
- Logs and an atomic proof record are written under the review artifact
  directory. Operational failures, timeouts, invalid patches, and cleanup
  failures are typed failures rather than regression evidence.

### Merge enforcement

- Add `verification.behaviorReview.required` as a strict opt-in configuration
  contract. `false` is invalid; omit the object to disable it.
- Documentation-only merge candidates remain on the documentation contract.
- Recommended and full merge candidates validate the receipt before running
  ordinary gate work. A finding uses a stable `policy.behaviorReview` identity.
- The receipt must match the resolved merge base and current clean candidate.
  Changing source, tests, configuration, or the candidate commit invalidates it.
- Semantic agent review remains nondeterministic evidence and never replaces
  ordinary policy checks, configured tests, human approval, or supplemental
  test-strength work.

## Implementation ownership

- `internal/policy`: configuration model, defaults, and validation.
- `internal/repository`: exact clean-candidate, ancestor, patch, and disposable
  worktree primitives needed by the feature.
- `internal/behaviorreview`: packet, schemas, artifact lifecycle, regression
  proof, result validation, and receipt validation. Add it as a first-class
  module depending only on `policy`, `repository`, and `runner`.
- `internal/engine`: expose the workflow and add receipt findings to opted-in
  non-documentation merge gates.
- `cmd/code-polishy`: strict CLI parsing and concise paths/results for
  `behavior-review prepare`, `behavior-review finalize`, and
  `regression-proof`.
- `templates/behavior-review.md`: canonical fresh-review instructions consumed
  by packet preparation.

## Verification

- Policy tests accept the opt-in shape and reject false, unknown, or malformed
  configuration.
- Repository tests prove exact ancestor resolution, clean-candidate rejection,
  binary-safe evidence patches, untracked/dirty rejection, and worktree cleanup.
- Behavior-review tests cover safe artifact paths, bounded intent, packet
  contents, strict result decoding, every classification, stale identifiers,
  missing proofs, red/green execution, wrong red status, candidate mutation,
  and cleanup failure.
- Engine tests prove documentation bypass, recommended/full enforcement,
  candidate/base mismatch, and a valid receipt continuing into the unchanged
  merge execution plan.
- CLI tests cover strict options, exit statuses, concise output, and help text.
- Run exact module tests during integration, `code-polishy test --changed`, and
  one final `code-polishy merge-gate --base origin/main` on the clean candidate.

## Delivery

Commit the plan, implementation boundaries, workflow implementation, and final
documentation as coherent checkpoints. After the implementation is verified,
delete this plan, remove superseded review guidance and duplicate concepts,
update current policy, adoption, architecture, workflow, and README surfaces,
then commit the clean final candidate.
