# Release Checklist

A Code Polishy release is one reviewed commit. It is named by the annotated tag
`v<VERSION>` derived from its own `VERSION` file. That immutable source tag is
the published install authority; Code Polishy does not require a GitHub Release
or downloadable binary asset. Each target repository requires the resulting
native release through `.code-polishy.lock.json`. The read-only preflight proves
the local release facts; a maintainer performs every action that creates,
pushes, or installs state.

1. Bring the candidate to release shape as its own reviewed commit: `VERSION`
   stores exactly the strict `MAJOR.MINOR.PATCH` version being released, and
   this release's entries move from `## Unreleased` to an exact
   `## <VERSION> - <YYYY-MM-DD>` heading in `CHANGELOG.md`. Use focused checks
   while preparing this commit; do not run the full gate against a dirty
   worktree as a separate release proof.
2. Check out the exact candidate commit and verify it through the managed
   lifecycle. The agent harness must have captured each original user request
   at its task base and each later correction before implementation continued.
   Inspect behavior-review status
   against the merge target and complete a fresh review only when checked-in or
   task policy selects it. The final merge gate must print `NOT RUN` for an
   optional skip or validate the exact selected receipt; it runs once for the
   unchanged candidate. Follow the [behavior-review
   workflow](policies/behavior-review.md) rather than creating an intent summary
   during release preparation. After the candidate has stopped changing, run
   `code-polishy test --suite mutation-wrapper-contract` as the fast harness
   smoke check. Do not start Gremlins if that contract fails. Then run
   `code-polishy test --supplemental` once as the first stable candidate's
   separate release-hardening stage. This explicit checklist event is the
   trigger; declared suites and `tests.requiredSupplementalKinds` alone do not
   schedule it. After a failure, rerun only failed suites and passed suites
   invalidated by changes to their tested production files or tests, or their
   own commands or configuration. Fresh exact `code-polishy test --suite`
   evidence composes with still-valid passed suites. Repeat every supplemental
   suite only when shared mutation infrastructure, toolchain, or selection
   changes, or the impact cannot be bounded. Credentialed, destructive,
   production-mutating, and live-provider probes remain typed external approval
   gates.
   Fast-forwarding a branch, running preflight, creating the annotated tag,
   installing the release, or updating a target lock does
   not invalidate that result. A later candidate source or policy change
   requires a fresh gate and invalidates supplemental evidence only under the
   preceding retry rule.
   The unchanged candidate must also pass the native CI lanes on Ubuntu, macOS,
   and Windows. Windows runs the executable, process-containment, journal-lock,
   optional and selected installed behavior-review workflows in PowerShell
   without WSL or Bash. Retain the CI run URL with the release evidence.
3. Run `./scripts/release-preflight.sh <candidate-commit-id>`, naming the
   candidate with Git's canonical lowercase full commit object ID, exactly as
   `git rev-parse HEAD` prints it; a symbolic, abbreviated, or uppercase
   spelling is refused so the proof binds to the reviewed commit rather than
   to whatever that spelling resolves to. Without creating or changing
   anything, it proves that the candidate is the exact current commit, the
   worktree carries no staged, unstaged, or untracked drift, `VERSION` stores
   exactly a strict semantic version under the same strict reader the
   installer runs, `CHANGELOG.md` records the released-version heading, and
   the derived `v<VERSION>` tag either does not exist yet or is already
   annotated at the exact candidate. Fix exactly what it names and rerun it
   until it passes.
4. Create the annotated tag the preflight reported as the next explicit
   action, and rerun the preflight to confirm the tag points directly at the
   exact candidate:

   ```sh
   git tag -a v<VERSION> -m "Code Polishy <VERSION>" <candidate-commit-id>
   ./scripts/release-preflight.sh <candidate-commit-id>
   ```

5. Push `main` and the new tag to the canonical
   `github.com/riteofstring/code-polishy` repository without rewriting
   published history. Protect release tags against deletion or update. Never
   move or reuse a published version tag; publish a new patch version for every
   correction.
6. Exercise the public installation contract from fresh shallow clones of the
   exact tag. On Linux or macOS, run `./tools/install-policy-tools.sh` and then
   `./scripts/install.sh`. On Windows x64, run
   `.\tools\install-policy-tools.ps1` and then `.\scripts\install.ps1`. Confirm
   each installation reports the same release identity while its manifest
   carries the correct host-specific entries. The unchanged candidate's native
   CI run is acceptable Windows evidence; retain its URL with the release
   evidence. No release asset publication step exists.
7. Prove an installed candidate before the public lock cutover by writing a
   temporary lock with its release engine, then run
   `./scripts/test-installed-release.sh --prefix PREFIX --lock LOCK`. This
   exercises optional, task-requested, merge-required, checkpoint-required, and
   strict full-candidate behavior review plus target shapes this repository is
   not one of. After publication, move each consuming
   repository's `.code-polishy.lock.json` to the installed release with
   `<release>/bin/code-polishy lock`, as that repository's own reviewed atomic
   authority cutover. Outgoing guidance governs until this command replaces the
   lock; incoming guidance governs afterward.
   Rerun the script with its default lock for this repository.
8. Keep behavior review experimental until real workflows in more than one
   repository cover an optional `NOT RUN` refactor, a user-requested feature, a
   merge-required feature, a checkpoint-required task, a strict full-candidate
   repository, an unintended behavior fixed and reviewed again, and a late
   request backed by pre-code intent. Retain completion rate, wall and proof
   time, corrective attempts, reviewer disagreements, false alarms, later-found
   misses, and operator-status clarity with the release evidence. Promote the
   workflow only when that evidence shows useful findings beyond ordinary
   tests often enough to justify its cost.

The source is licensed under Apache-2.0. Pushing the version tag, changing
visibility, or changing branch and tag protection remains an explicit
maintainer action.
