# Repository Size

Use `size` to see how much space a repository uses, which kinds of content
account for it, and where the largest contributors are:

```sh
./code-polishyw size
./code-polishyw size --base origin/main
./code-polishyw size --base origin/main --format json
```

The command is descriptive and exits successfully when analysis completes. It
does not enforce a universal size budget or label a project as too large. A
repository can set its own enforced line budget with `quality.lineBudget`; see
the [Code Quality Policy](policies/code-quality.md#repository-line-budget).

## What is measured

The report separates two views:

- **Workspace** includes on-disk project files, Git metadata, installed
  dependencies and toolchains, build output, and caches. Symbolic links count
  as link entries and are never followed.
- **Governed content** includes materialized tracked files and non-ignored
  untracked files after Code Polishy's default and configured exclusions. It is
  grouped by content classification, module, and language.

`.code-polishy-reports/**` and `.code-polishy-artifacts/**` are named exclusions
from workspace totals. Excluding Code Polishy's own outputs makes repeated
analysis stable.

If an ignored directory or another workspace-only entry is unreadable, `size`
still reports readable governed content, source lines, and line-budget headroom.
It labels workspace totals **incomplete** and lists up to 20 unreadable paths.
JSON records `workspace.incomplete`, `workspace.unreadablePathCount`, and
`workspace.unreadablePaths` inside `repositorySize`. Workspace totals and shares
cover only measured entries, not the inaccessible contents. Restore access and
rerun to obtain complete workspace totals. Failures to inventory or read governed
source still fail the command.

Sizes are logical byte totals, not filesystem block allocation. Sparse files,
compression, clones, and hard links can therefore use a different amount of
physical storage on a particular machine.

The human report shows bounded summaries, categories, top-level paths, modules,
languages, and largest files. It also counts non-blank lines of handwritten
source by language, split into production and test, and shows each configured
line budget with its remaining lines. JSON output retains the same structured byte and
file counts in `repositorySize`; it includes paths and metadata but no file
contents. Language grouping uses file extensions and configured language rules,
not content sniffing.

## Compare growth

`--base REF` resolves the merge base of `REF` and the current `HEAD`, then
compares that Git tree with the canonical Git representation of the current
candidate. Clean and staged tracked files use index blob sizes, so Git LFS,
line-ending conversion, and sparse checkout do not create false growth. Safe
unstaged and non-ignored untracked additions also participate. The report
includes:

- total byte and file-count change;
- added, removed, and resized file totals;
- changes by category, module, and language; and
- the largest individual size changes.

An edit that preserves a file's byte length is not a size change. Untracked,
non-ignored files appear as current additions. If an unstaged or untracked file
uses a Git content filter, working-tree encoding, ident expansion, or line-ending
conversion, stage or commit it before comparing. This lets Git establish its
canonical blob size without Code Polishy executing the transformation. The
comparison's current total can differ from the governed working-tree total when
materialization changes the on-disk representation.

## Interpret the result

Start with composition and change rather than the absolute total. A large asset
repository may be healthy while the same footprint in a small CLI could expose
committed build output or oversized fixtures. Useful questions include:

- Is most workspace space local dependency or build output rather than
  governed content?
- Did one module, category, or file cause recent growth?
- Are large generated files, fixtures, or assets intentional and correctly
  classified?
- Is the growth expected for this project's kind and current work?

A coding agent can answer those questions from the human or JSON report. Code
Polishy does not invoke an LLM or send repository metadata to an external
service.

## Propose deliberate headroom

Choose a specific ceiling for a coherent feature or milestone, with enough
headroom for the intended work. Avoid resetting the budget to the exact count
after every small addition. Work within an approved ceiling needs no further
budget edit or approval. Exceeding it still requires reducing code or the caller's
explicit permission to raise it; agents receive no automatic slack.

Measure the current size and growth without changing configuration:

```sh
./code-polishyw size --base TASK_BASE
```

The existing line-budget report includes current size, base size, actual growth,
current ceiling and remaining lines. Explain the proposed ceiling, resulting
headroom and rationale in ordinary prose. With no base, growth remains unknown.
Historical non-blank lines use current scope and ownership policy. Capture the
report once for the approval; change `.code-polishy.json` only after explicit
authorization.

Budget-only configuration edits still participate in complete test-receipt
identity. A suite can consume that file, and selection or coverage can change
with policy. No configuration exclusion or narrower invalidation is inferred.
