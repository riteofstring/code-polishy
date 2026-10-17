# Repository Line Budget

## Decision

AI coding agents tend to grow repositories far beyond what their behavior
requires. File length and complexity budgets catch monoliths, not volume spread
across many acceptable files. `quality.lineBudget` lets a repository cap the
handwritten source it is willing to maintain and makes exceeding that cap a
blocking failure, so growth becomes a deliberate decision.

The budget is opt-in and has no default. A reasonable size depends on the
project, and a release-chosen number would either block real projects or
constrain nothing.

## What counts

The budget counts non-blank lines in the files that already receive a
file-length budget. The repository module owns this applicability rule, which is
shared by both checks so they cannot drift. Generated output, declared data,
lockfiles, symbolic links, Markdown, and configuration formats are excluded for
the reasons the code-quality policy gives for file length. Generator and
template source remains handwritten and counts.

Blank lines are excluded because they reflect layout, not volume. Comments
count. Excluding them would need a comment parser for every budgeted language,
including repository-declared ones, while `quality.allowComments` already
governs whether comments belong in source. Counting them keeps the rule the
same for every language.

The budget measures the repository, not the checkout. Git marks tracked files
outside a sparse checkout as skip-worktree and ignores their working copies, so
the count reads those files from the index, which is what a commit records.
It reads only files the budget could count, so a partial clone does not fetch
excluded content such as documentation or data. Files whose extension names no
language are still read, because a shell shebang makes them source.
Rejecting sparse checkouts instead would leave an agent working in one no way to
pass a configured budget.

Tests have their own scope because useful tests should not compete with
implementation for space. `total` covers projects that prefer one number.

## Enforcement

The count always covers the whole repository, whatever the selection, because
a focused check would otherwise pass while the repository is over budget.
Counting reads every budgeted file once per check; that is small next to the
analyzers a check already runs.

`quality.lineBudget` is not exemptable. An exception would be a second budget
without the visibility of the real one, so the only way past the limit is to
reduce the code or change the budget.

## Changing the budget

Raising the budget requires the caller's permission. An agent with a shell can
edit any local file, environment variable, or approval record, so Code Polishy
cannot prove locally who approved a change. Requiring signed commits for budget
changes could prove it, but would add key management and history verification
to every adopting repository. The control is instead:

- the over-budget remedy tells the agent to reduce the code or ask the caller
  for a specific increase, and to change the budget only at the caller's
  explicit request;
- every check with a base compares the candidate budget with the base
  configuration and reports a `quality.lineBudgetChange` warning for each
  changed scope, so the change is visible in gate reports and review; and
- repositories that need a hard guarantee protect `.code-polishy.json` with
  CODEOWNERS and required review, as the exception policy already recommends.

The base comparison reads only `quality.lineBudget` from the base
configuration. Older or otherwise invalid base configurations therefore still
compare, and a base without configuration reports nothing.

Managed `AGENTS.md` guidance does not repeat this rule. Its prefix is near its
size budget, and the remedy reaches the agent at the moment it matters: when
the budget is exceeded.

## Visibility

`size` reports counts by language and each scope's remaining lines. `task-start`
reports the remaining lines so an agent knows its capacity before it writes
code. The budget is withheld from provider requests because providers do not
use it and changing it should not invalidate their evidence.
