# Execution Observability and Repository Facts

Repository classification depends on the fully composed policy. Code Polishy
therefore attaches a path-fact cache after packs and policy modules have been
applied, and replaces that cache whenever a different configuration is used.
Language, generated/data/test status, executable status, and module ownership
are computed once per path for that immutable configuration. Returned slices
are copied so a caller cannot change cached authority.

Glob matching uses a bounded process cache of compiled, anchored expressions.
This preserves the segment-aware `*`, `**`, `**/`, and `?` grammar while
avoiding compilation for every path-to-pattern comparison. The cache contains
derived policy machinery only; it does not retain repository contents.

Selected checks still run repository-wide coverage when a selected control
file can change global policy. They may add source-bound analyzer inputs for
declared runtime contracts, but they do not add unrelated project sources.
Reports expose requested and expanded paths, analysis-context paths, graph
size, total evaluation time, phase durations, exact analyzer commands, resource
wait, analyzer-owned Vulture subphases, and cache activity so retained evidence
identifies both scope and cost.

Repository-wide Python dead-code analysis runs only for an explicit complete
selection or merge gate. Focused Python checks run selected Ruff and ty work;
they do not construct global reachability evidence that cannot establish a
complete-project result. Vulture still reads the full project when selected,
but framework walks are limited to diagnostic targets, repeated exact ancestry
queries are memoized, and its response records source parsing, Vulture scan,
type-fact, contract, reachability, and diagnostic durations.

Cache lifetime follows its evidence lifetime. Repository facts are reused
within one command after configuration composition. Cross-process analyzer
results are reused only through an existing digest-bound receipt mechanism;
ordinary files are never trusted from a timestamp-only persistent cache.
