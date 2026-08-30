# Behavior Review Release Readiness Plan

## Outcome

Finish the behavior-regression workflow as one coherent release candidate. The
workflow must bind the user's request before implementation, run through an
installed Code Polishy release on Unix and Windows, survive full CI, and be
clear enough to use on real work.

The release stays provider-neutral. The agent harness supplies the user's exact
request and acceptance criteria. Code Polishy records that input before code
changes, binds it to the repository state, and refuses stale or incomplete
evidence later.

## Starting gaps

- The current `prepare` command accepts a prose intent file after coding. An
  agent can therefore write an inaccurate summary and review against it.
- The branch has package-level local results but no authoritative hosted CI run.
- Installed-release coverage stops at documentation checkpoints. It does not
  exercise the complete behavior-review and merge workflow.
- Windows compiles the new packages without running the complete workflow.
- The workflow has not produced repeatable usability measurements across
  representative changes.
- The shortened `AGENTS.md` contract does not match the release currently
  locked by this repository.
- Release version, lock, links, docs, skill, templates, and agent guidance still
  need one coordinated cutover.

## 1. Bind intent before implementation

### User workflow

At the start of each task, before any implementation change, the harness writes
the original request and acceptance criteria to a bounded UTF-8 file and runs:

```sh
code-polishy behavior-review capture-intent --intent-file PATH
```

The command requires a clean committed repository. It copies the input into a
managed intent journal below `.code-polishy-reports/behavior-review`, binds the
entry to the exact current commit, and prints the stored entry identifier.

After implementation, preparation no longer accepts an external intent path:

```sh
code-polishy behavior-review prepare --base REF
```

Preparation selects the journal entries between the exact review base and the
candidate. It includes their original text and commit boundaries in the review
packet. A checkpoint normally selects one task entry. A final merge may select
several entries from a long-lived branch.

### Intent journal contract

- Each entry records a random identifier, the exact clean `HEAD`, the copied
  request text, its SHA-256 digest, the prior journal digest, and the new journal
  digest.
- Appends use the existing contained, restrictive, atomic artifact-writing
  rules. Existing entries are never edited in place.
- The journal is strict and versioned. Unknown fields, missing entries, broken
  digest links, invalid commits, invalid UTF-8, non-regular files, and oversized
  content fail closed.
- Entries are ordered by the journal chain. Their captured commits must advance
  monotonically through the reviewed history, though several requests may be
  captured at the same clean commit.
- The first selected entry must be captured at the exact review base. The last
  selected entry must precede a changed candidate. This catches the common case
  where an agent tries to capture intent only after coding.
- `prepare`, `finalize`, checkpoint validation, proof replay, and merge-gate
  validation bind the selected journal digest and re-read the managed journal.
  Replacing intent after preparation invalidates the workflow.
- Review packets present each original request separately. The reviewer judges
  the aggregate candidate against every selected request and acceptance
  criterion.

### Trust boundary

The harness owns capture timing and source fidelity. Code Polishy can prove that
the same bytes and commit boundaries flowed through its local workflow. A local
digest is not a signature and cannot prove that those bytes came from a
particular chat provider or user. Documentation must state this directly.

The harness should protect or transfer the complete behavior-review artifact
directory as one unit. A writer that can replace every locally stored artifact
can create a different self-consistent history.

### Implementation ownership

- `internal/behaviorreview`: intent types, journal validation, atomic append,
  selection by commit ancestry, packet inclusion, and receipt binding.
- `internal/repository`: reuse exact clean-commit and ancestry primitives. Do
  not add behavior-review policy to the repository layer.
- `internal/engine`: expose capture and require the selected journal throughout
  checkpoint and merge validation.
- `cmd/code-polishy`: add `capture-intent`, remove `--intent-file` from
  `prepare`, and keep errors concise and actionable.
- `templates/behavior-review.md`, the policy docs, workflow docs, skill, and
  generated guidance: teach the new ordering and trust boundary from one
  authoritative description.

### Verification

Tests must cover:

- successful first capture and multiple append operations;
- dirty, staged, unstaged, and untracked repository rejection;
- bounded input, regular-file, UTF-8, containment, and permission rules;
- atomic failure without a partial journal;
- missing, reordered, removed, edited, duplicated, and corrupt entries;
- unrelated branches and non-ancestor capture commits;
- multiple requests captured at one commit;
- task checkpoint selection and multi-task final-merge selection;
- capture after implementation being rejected;
- packet, result, proof, checkpoint, and merge receipt invalidation after an
  intent or candidate change;
- CLI help, strict option parsing, exit status, and concise output.

## 2. Run authoritative CI

- Run exact package tests while editing through the governed toolchain.
- Run `code-polishy test --changed --base origin/main` on the complete branch.
- Run one final `code-polishy merge-gate --base origin/main` on the clean final
  candidate.
- Push the feature branch and run the repository's full hosted workflow on the
  exact commit. Do not treat local package results as hosted CI evidence.
- Record the workflow URL and exact commit in the delivery summary. Any skipped
  or unavailable platform must be reported explicitly.

## 3. Cover installed releases and Windows

### Installed-release flow

Extend the installed-release test to build a release, install it into an empty
prefix, and use only the installed launcher for this non-documentation flow:

1. initialize a disposable repository and capture intent at its clean base;
2. make and commit a requested behavior change plus its regression evidence;
3. prepare a fresh review packet;
4. write a valid clean-review result as the external reviewer boundary;
5. prove the evidence fails at the pre-fix commit and passes on the candidate;
6. finalize the behavior review;
7. run the checkpoint gate;
8. repeat the required final review and proof for the merge base;
9. run the merge gate successfully;
10. change intent, candidate, or evidence and prove stale artifacts are
    rejected.

The fixture must use real configured tests. It must not replace a missing test
or review artifact with fake success.

### Windows runtime

- Run the behavior-review and gate-report package tests on Windows instead of
  compile-only checks.
- Add a native Windows integration path for intent capture, packet preparation,
  review finalization, proof replay, checkpoint, and merge validation.
- Exercise drive-letter paths, separators, atomic replacement, permissions that
  Windows can enforce, process cleanup, and disposable Git worktrees.
- Keep Unix shell fixtures and Windows PowerShell fixtures behaviorally
  equivalent. Shared expected JSON belongs in one fixture where practical.

## 4. Dogfood the workflow

Exercise the installed candidate against at least two disposable repositories
and several representative changes:

- a requested bug fix with a clear red/green reproducer;
- a refactor that preserves observable behavior;
- a candidate containing an unintended behavior change that must block;
- a long-lived branch with two separately captured user requests;
- a resumed gate after an unrelated late failure.

For each run, record:

- whether a first-time operator completed the flow;
- elapsed time for capture, prepare, proof, finalize, checkpoint, and merge;
- the number of corrective attempts;
- whether each failure named the next action clearly;
- artifact size and whether transfer between jobs remained understandable.

Automated disposable repositories provide repeatable release evidence. Testing
against external repositories remains a separate product acceptance step and
requires the owner to name those repositories explicitly.

## 5. Complete the public cutover

### Agent guidance

Restore the canonical guidance for the currently locked release while this
branch is under development. The repository must pass `code-polishy doctor
--strict` before feature work is considered verified.

The shortened `AGENTS.md` approach belongs in the later docs-CLI cutover, where
the matching skill, bundled docs, templates, generated guidance, and command
are released together. Do not publish guidance that tells an agent to use a
command the locked release does not contain.

### Documentation

- Update behavior-review policy, agent workflow, artifact-security,
  installation, adoption, architecture, and release-checklist surfaces.
- Fix README and documentation links that still target renamed headings.
- Keep one authoritative workflow and generate or link repeated instructions.
- State which artifacts are local integrity records and which trust still comes
  from the harness, CI, or human approval.

### Version and lock

Do not change `VERSION` or `.code-polishy.lock.json` while the release is still
unpublished. The final release branch must update the version, lock, changelog,
README links, generated guidance, skill, docs, and templates together, then run
the installed-release test against that exact candidate.

## Commit and delivery sequence

1. Commit this readiness plan and the separate docs-CLI plan.
2. Commit intent capture, journal validation, and exact tests as one coherent
   behavior contract.
3. Commit installed-release and Windows runtime coverage.
4. Commit dogfood fixtures, measurements, and usability fixes.
5. Commit the canonical-guidance restoration and permanent documentation
   cutover.
6. Run changed tests and the final merge gate, then commit any generated or
   verification-owned changes.
7. Push the exact branch commit and obtain full hosted CI evidence.
8. Create the docs-CLI branch from the verified final commit. Keep the CLI plan
   there as the next implementation contract.

The feature branch is ready only when the worktree is clean, every required
platform result is known, the installed workflow passes end to end, and no
release surface advertises an unavailable command.
