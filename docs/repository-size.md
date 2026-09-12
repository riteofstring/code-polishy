# Repository Size

Use `size` to see how much space a repository uses, which kinds of content
account for it, and where the largest contributors are:

```sh
code-polishy size
code-polishy size --base origin/main
code-polishy size --base origin/main --format json
```

The command is descriptive and exits successfully when analysis completes. It
does not enforce a universal size budget or label a project as too large.

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

Sizes are logical byte totals, not filesystem block allocation. Sparse files,
compression, clones, and hard links can therefore use a different amount of
physical storage on a particular machine.

The human report shows bounded summaries, categories, top-level paths, modules,
languages, and largest files. JSON output retains the same structured byte and
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
