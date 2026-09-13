# JavaScript imports of local assets

The sealed JavaScript reader resolves a literal relative stylesheet import only
when the actual file is readable inside the target repository. Missing files and
escaping symlinks remain unresolved. A type declaration cannot establish the
existence of an arbitrary local asset.

The architecture boundary distinguishes an imported asset from executable source.
A governed non-executable target need not become an executable graph node, but
its source and target module owners still participate in dependency-direction
checks. This preserves stylesheet ownership checks without claiming to analyze
stylesheet bytes as JavaScript. Executable targets absent from the graph remain
incomplete coverage.
