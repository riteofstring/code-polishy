# Bounded JavaScript glob inputs

Repository source, workspace declarations, and package scripts can supply glob
patterns to the sealed analyzers. Disabling Knip plugins does not remove those
inputs: its script reader still contributes entry patterns. Filtering only
workspace configuration or import expressions would leave an expansion path open.

The shared glob boundary limits brace and parenthesis nesting to 64 levels before
the pinned `braces` parser constructs its recursively traversed tree. This leaves
ample stack margin while preserving ordinary nested alternatives. The linear scan
follows that parser's escaping, quoted text, character classes, and matched closing
delimiters. Literal punctuation and flat alternatives do not consume nesting depth.
Original patterns remain unchanged, so accepted matches retain their meaning.

Both analyzers temporarily bind the three public `fast-glob` methods used by
Knip 5.55.1: `glob`, `sync`, and `globStream`. The binding resolves Knip's own copy
and checks both patterns and ignores after Knip constructs them. The provider's
exact graph resolves its direct `fast-glob` dependency to that same instance, and
its adapter binds the boundary before any capability runs, covering both
`import.meta.glob` and Knip. The native runner binds it for dead-code analysis.
This covers workspace discovery, manifest scripts, and derived entry/project
patterns without reimplementing Knip's input selection. The isolated analyzer
process runs one analysis at a time and restores all methods in its existing
cleanup path. Packaged provider tests verify both direct and Knip callers after
the hoisted artifact installation.

Excessive nesting yields an explicit native analysis failure or provider incomplete
coverage. It never becomes an empty match or a passing check. No installed package
files are rewritten. The provider build includes the shared boundary inside its
verified artifact, as it does for shared policy rules. Dependency or caller changes
must recheck which methods reach expansion; this binding is intentionally specific
to the pinned analyzer, not a general package interception mechanism.
