# Repository Size Analysis

Repository size is evidence, not a universal quality score. Project kind,
generated-output policy, asset needs, test strategy, and growth history determine
whether a footprint is reasonable. Code Polishy therefore measures composition
and change while leaving qualitative interpretation to the calling human or
agent.

The repository boundary owns measurement because it already owns contained
inventory, exclusions, path classification, module ownership, and Git merge-base
resolution. The engine converts that result into bounded tables and the
versioned report model. The CLI parses command arguments and renders the report;
it does not walk the filesystem or classify paths independently.

The report keeps two totals. Workspace size measures on-disk regular files and
symbolic-link entries, including Git metadata, dependencies, toolchains, build
output, and caches. It never follows a symbolic link. Governed size measures the
materialized tracked and non-ignored untracked content selected by the
repository's composed policy. Comparing the totals exposes local disk consumers
without mistaking them for versioned project content.

Workspace measurement is supplementary to governed counts. An unreadable
workspace-only directory or entry must not discard readable source counts,
line-budget headroom, or a Git comparison. The walker continues through readable
siblings and marks workspace totals incomplete, retaining the total number of
unreadable paths and the first 20 paths in traversal order. These totals and
shares describe only measured entries; an unreadable subtree is never treated
as empty. Governed inventory and source-reading failures remain operational
errors. Missing tracked paths are omitted from the materialized inventory;
permission and other inspection errors are returned before workspace measurement.
Untracked inventory first identifies directory subtrees fully excluded by the
repository's own glob rules and gives Git their literal paths to avoid traversing
them. File-specific exclusions cannot hide a whole directory, and directories
containing dynamic control inputs remain visible. This directory pass only
selects exclusions; Git still owns file enumeration and ignore rules. Git can
report traversal failures through diagnostics despite exiting successfully, so
diagnostics remaining after those exclusions prevent partial governed counts.
Restoring access restores complete measurement on the next invocation.

Byte totals are logical content sizes rather than filesystem block allocation.
Workspace and governed-current totals describe materialized entries, while a
base comparison describes canonical Git blob bytes. Sparse files, compression,
clones, hard links, checkout filters, and line-ending conversion can therefore
make the materialized and Git totals differ without either total being wrong.

Managed `.code-polishy-reports` and `.code-polishy-artifacts` directories are
excluded from workspace measurement. A size command writes its report only
after analysis; including prior reports would otherwise make identical reruns
grow their own input and produce a new identity indefinitely. The report names
these exclusions explicitly.

An optional base comparison resolves the Git merge base and compares its tree
with the candidate represented by the Git index plus safe unstaged and untracked
overlays. Tracked candidate sizes come from blob metadata, which keeps clean,
staged, filtered, line-ending-converted, and sparse checkouts comparable with the
base. An unstaged or untracked regular file participates only when Git would not
transform its bytes; otherwise the command requires the caller to stage or
commit it so Git can establish the canonical blob without Code Polishy invoking
content filters. Candidate policy classifies both sides consistently.

The comparison reports additions, removals, resized files, category and module
deltas, and the largest absolute changes. Equal-size content edits are
intentionally absent because they do not change repository size. Its current
total can differ from the governed working-tree total when materialization, such
as Git LFS smudging or sparse checkout, changes which bytes are present on disk.

All aggregates and largest-entry lists have deterministic ordering and bounded
cardinality. Size classification uses path metadata and composed declarations;
language groups use extensions and `scope.languages` rather than content
sniffing. Reports contain paths, classifications, counts, and byte totals, but
never file contents. Code Polishy does not choose an AI provider, transmit the
report, or silently turn an LLM opinion into enforcement.
