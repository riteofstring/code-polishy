# Repository Trends Design

## Decision

Trends describe how a repository's code fares over time. They are evidence for
a human or agent to interpret, not a quality score and not proof that Code
Polishy caused a change. Separating the effect of Code Polishy from model
upgrades, task mix, and project maturity would require controlled replays,
which are outside this command.

The command reports only measures that Code Polishy does not enforce. Gate
block counts, first-attempt pass rates, exception counts, complexity, file
length, dead code, and test ratios are shaped directly by policy, so their
trends would mostly show that enforcement works. The rewrite rate, copied-code
share, change spread, reverts, and flaky suites describe what happens to code
after it lands.

Because the report is descriptive, unrelated repository policy status, such as
an expired exception or assessment, does not change its outcome. Like
design-context, it does not apply global policy checks.

## History model

A landed change is a commit on the first-parent line of the analyzed branch.
That definition keeps branch-internal iteration out of merge-based
repositories while treating direct commits individually. It also works for
long-lived branches: analysis follows the requested branch, not the default
branch. A merge lands every line it brings in at the merge time, so work
developed elsewhere and later merged or promoted appears as one large change.
Analyzing the development branch shows how that work fared while it was built.
Following every commit instead would require a separate parent state per
commit and would count the work-in-progress commits of merge-based
repositories. Mass moves beyond Git's rename limit appear as deletions and
additions because Git does not report them as renames.

The repository module replays a single `git log --first-parent` patch stream
instead of running blame for each changed file. Explicit diff options fix
prefixes, context, rename detection, whitespace handling, submodule format, and
external diff behavior, and the command clears `GIT_DIFF_OPTS`, which would
otherwise override the context setting. User Git configuration and diff
environment therefore cannot change the stream. The collector
keeps each tracked file's lines in memory together with the change that landed
each line, and it applies every hunk in order. When a hunk does not match the
tracked content, the collector counts the change in `skippedChanges` instead
of guessing and reloads the file from its parent revision at the next change.
A file moved from an untracked path into tracked source is loaded the same way,
so it stays visible after the move.

Remembering each line's landing change lets the report name the changes whose
lines were rewritten most. A large change that was soon replaced can then be
recognized in its week instead of being excluded by an arbitrary size limit.

Comparison and copy detection both ignore whitespace, matching the patch
stream. A copy is six consecutive substantive lines that already existed in
the parent tree and were not removed by the same change, so moved code is not
counted. The copy index keys windows by interned line identities; it does not
hash content.

The current composed policy classifies historical paths with the same
generated, data, source, control-input, and module-ownership rules used by
repository size analysis. Historical configuration is not replayed, and weeks
before adoption have no configuration to replay, so trends describe the
history through today's classification and note when their range includes
weeks before adoption. Generated files at old paths that today's configuration
does not name therefore count as handwritten. Classification stays exact and
configuration-owned rather than guessing from file names, so the report states
the limitation instead of correcting it heuristically.

Lock changes record adoption, upgrades, downgrades, and removals. The direction
compares numeric version components; versions that cannot be compared are
recorded as changes. Each lock change also records the tracked lines its
commit added and the recent lines it rewrote, and the rewritten-changes list
marks landing changes that changed the lock. Rework in version-change commits
therefore stays visible and separate from other rework. The report does not
attribute that rework to Code Polishy, because such a commit can include
unrelated work.

## Gate records

Flaky tests use the gate-run storage owned by `gaterun`. Its reader enumerates
checkpoint and merge executions, reads each report's version, and validates
current reports with the existing strict reader. Reports that declare another
format version are counted separately from reports that are incomplete, lack a
version, or fail validation; there is no compatibility parser. The engine collects records from
every Git worktree, because gate runs are stored in the working copy where
they ran. Records are not filtered by the analyzed branch: flakiness belongs to
the test suites, and filtering by branch would drop runs on commits that were
later amended.

A flaky failure is a test suite that failed with a test failure and then
passed on the same candidate commit, either through a retry within the run or
in a later run. Environment, resource, operational, and canceled failures are
not test failures.

## Time and bounds

Weeks start on Monday in UTC and use commit time. The rewrite period defaults
to 14 days, a common convention rather than a derived value, so
`--rewrite-days` accepts 1 to 365 days and the report records the period it
used. The analysis time decides whether a week's rewrite period is complete.
The default window is 12 weeks and the maximum is 104, which keeps the report
bounded. Lists of rewritten changes, reverts, copies, version changes, and
flaky suites also have fixed limits.

## Ownership

The repository module owns Git history analysis and worktree discovery.
`gaterun` owns reading its stored reports. The engine composes flaky-test
detection, notes, and tables, and the CLI only parses options and renders the
report. The command neither invokes an AI provider nor sends repository data
anywhere, and its report contains no file contents.
