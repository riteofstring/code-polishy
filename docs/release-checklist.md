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
   lifecycle. After policy-selected ordinary acceptance passes, Code Polishy
   automatically runs impact-relevant declared local supplemental mutation/risk
   hardening. Credentialed, destructive, production-mutating, and live-provider
   probes remain typed external approval gates. Run the full gate once for an
   unchanged candidate when that is the selected ordinary profile.
   Fast-forwarding a branch, running preflight, creating the annotated tag,
   installing the release, or updating a target lock does
   not invalidate that result. Rerun the gate only after the candidate commit
   changes.
   The unchanged candidate must also pass the native CI lanes on Ubuntu, macOS,
   and Windows. Windows runs the executable and process-containment tests in
   PowerShell without WSL or Bash. Retain the CI run URL with the release
   evidence.
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
   exact tag. On Linux or macOS, acquire the pinned tools and run
   `./scripts/install.sh`. On Windows x64, acquire the pinned tools and run
   `.\scripts\install.ps1`. Confirm each installation reports the same release
   identity while its manifest carries the correct host-specific entries. The
   unchanged candidate's native CI run is acceptable Windows evidence; retain
   its URL with the release evidence. No release asset publication step exists.
7. Move each consuming repository's `.code-polishy.lock.json` to the installed
   release with `<release>/bin/code-polishy lock`, as that repository's own
   reviewed change. For this repository, then run
   `./scripts/test-installed-release.sh`, which proves the locked release
   against target shapes this repository is not one of.

The source is licensed under Apache-2.0. Pushing the version tag, changing
visibility, or changing branch and tag protection remains an explicit
maintainer action.
