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

Candidate test retries are outcome-bearing attempts, not baseline diagnostics.
The first full attempt establishes all tests except the observed failures; an
optional repository-declared retry command can establish those failures at a
narrower runner-native boundary. A passing retry therefore satisfies the suite
while both attempts remain immutable in the report, but only after a normal
test-command exit; infrastructure and artifact failures stay diagnostic.
Baseline replays remain diagnostic and cannot satisfy the candidate. The gate
runner, rather than the report renderer, authorizes the configured retry command
and writes the receipt for the resulting combined outcome.

Failed-run resume is an optimization over complete gate execution, not a
precondition for recovery. A valid failed report with the exact current gate
identity contributes only eligible ordinary-suite receipts. When no run exists
for that identity, the gate executes every phase. Once an exact run directory
exists, missing or invalid report evidence is an integrity failure and cannot
be reclassified as absence.

Some scanners use a nonzero exit to report findings rather than an operational
failure. The gate records such an attempt as report-bearing only when the
planned command declares a recognized protocol and its protocol-specific parser
accepts a complete, nonempty finding report. The original exit status remains
in evidence, while malformed, empty, timed-out, canceled, and operational
results remain failed commands. OSV-Scanner's exit status 1 is the first such
protocol; vulnerability assessment policy, not process status, decides whether
its accepted report permits the gate to pass.
