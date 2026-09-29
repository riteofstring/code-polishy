# Repository Trends

Use `trends` to see how a repository's code holds up week by week:

```sh
code-polishy trends
code-polishy trends --since 2026-06-01 --rewrite-days 30
code-polishy trends --branch workspace-extraction --format json
```

The command is descriptive and exits successfully when analysis completes,
even if other policy checks would fail, such as an expired exception. It
reports trends; it does not score the repository or claim that Code Polishy
caused a change.

## What is measured

Each row covers one week, starting on Monday in UTC:

- **Changes** counts commits that landed on the analyzed branch.
- **New lines** counts lines of handwritten code and tests that landed.
- **Rewritten** is the share of that week's new lines that were changed or
  deleted within 14 days of landing. A value marked "so far" belongs to a week
  whose lines have not all been watched for 14 days yet, so it can still rise.
- **Copied** is the share of new lines inside blocks of six or more
  substantive lines that already existed elsewhere in the repository. Code
  moved within the same change is not counted.
- **Modules/change** is the average number of declared modules touched by the
  changes that touched at least one module. A test counts toward the module it
  verifies.
- **Reverts** counts commits created by `git revert`.
- **Flaky tests** counts test suite runs that failed and later passed on the
  same commit, out of all suite runs recorded by gates that week on any
  branch.
- **Code Polishy** shows the locked Code Polishy version at the end of the
  week. A dash means the repository had not adopted it yet.

Code Polishy does not enforce any of these values. Its checks already block
oversized files, complex functions, dead code, and similar findings, so counts
of those would only show that enforcement works. These measures instead show
how new code fares after it lands.

The human report adds the changes whose lines were most often rewritten, the
files with the most copied lines, the latest reverts, and the suites with the
most flaky failures. Use the rewritten-changes list to explain a spike: one
large change that was soon replaced can dominate a week. JSON output keeps the
complete weekly values, version changes, and bounded lists in
`repositoryTrends`.

Code Polishy upgrades can change code too, for example when a new release
reformats files. The rewritten-changes list marks changes that also changed the
Code Polishy version, and a separate list shows each version-change commit that
changed code, with the lines it added and the recent lines it rewrote. That
keeps rework in version-change commits apart from other rework. Such a commit
can also contain unrelated work, so the list shows where to look rather than
what caused the rework.

## Choose the rewrite period

Fourteen days is a convention, not a rule. `--rewrite-days N` changes how long
each new line is watched, from 1 to 365 days. A longer period also catches
rework that happens weeks later, but it counts more ordinary edits as rewrites,
and recent weeks stay "so far" for longer. Every week in one report uses the
same period, and the report records it, so compare two reports only when they
used the same value.

## Which history is analyzed

`trends` follows the first-parent history of `HEAD`, or of the branch or
commit named by `--branch`. Each commit on that line is one landed change. A
merge counts once, with everything it brings in; a repository that commits
directly to its branch counts each commit. When work happens on a long-lived
branch that the default branch has not received yet, analyze that branch.

If work is developed on one branch and later merged or promoted into another,
the receiving branch shows that work as a single large change. To see how the
code fared while it was being developed, analyze the development branch for
that period, for example
`code-polishy trends --branch old-mainline --since 2026-03-02`.

Handwritten code and tests are classified with the current configuration,
including for older weeks. Generated, data, excluded, and configuration files
are left out, as are files that are not source code. Weeks before Code Polishy
was adopted, or before the module layout changed, may classify files less
accurately. For example, generated files at old paths that today's
configuration does not name count as handwritten and raise that period's
rewrite rate and copied share. The report notes when its range includes weeks
before adoption; before comparing across that date, check whether the most
rewritten changes and most copied files are generated code. A file moved from
an excluded or generated path into tracked source is followed from that point.
Whitespace-only edits do not count as rewrites, and renames do not count as new
or rewritten lines unless Git cannot detect them because a single commit moves
more files than its rename limit allows.

Without `--since`, the report covers the current week and the 11 weeks before
it. `--since YYYY-MM-DD` starts at the week containing that date and may cover
up to 104 weeks. The analysis needs the commit just before the first week, so
a shallow clone stops with an error instead of counting its oldest fetched
commit as new code; run `git fetch --unshallow` or choose a later `--since`.
The command requires Git 2.36 or later.

## Flaky tests and gate records

Flaky tests come from checkpoint and merge gate records in every working copy
of the repository on this machine, including linked Git worktrees. Flakiness
belongs to the test suites rather than to one branch, so these records are not
filtered by `--branch`. Records written by a Code Polishy version with a
different report format are skipped, and records that are incomplete or fail
validation are skipped separately; the report counts both. Gate runs that
happened only in CI or in removed working copies are not included.

## Interpret the result

Read the direction over several weeks rather than a single value. Model
upgrades, task mix, and project maturity move these numbers too, so a change
around an adoption or upgrade date is a correlation, not proof. Useful
questions include:

- Is the rewrite rate falling or staying low? A spike often means a feature
  was replaced or a direction was abandoned soon after landing.
- Did a copied-code spike come from deliberate scaffolding, or from code that
  should share a module?
- Are changes touching more modules over time?
- Are flaky failures concentrated in a few suites?

A coding agent can answer those questions from the human or JSON report. The
report contains paths, commit IDs, revert subjects, and version numbers, but no
file contents. Code Polishy does not invoke an LLM or send repository data to
an external service.
