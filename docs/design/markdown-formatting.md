# Markdown extension preservation

The sealed formatter preserves MkDocs admonitions and expandable details using
the existing pinned Prettier parser and printer plugin interfaces. The markers
`!!!`, `???`, and `???+` introduce indentation-sensitive content that Prettier's
ordinary Markdown parser can combine into a paragraph. Successful parsing and
idempotent output do not establish preservation of that content.

The plugin replaces each affected top-level Markdown node with an opaque node
containing its original source. It includes following indented blocks, so a blank
line between the marker and its body does not split the preserved region. When
an admonition appears in a list, blockquote, or code example, its containing node
is preserved. This keeps the surrounding container's indentation and fence
boundaries together without implementing another Markdown grammar. Other
top-level nodes use Prettier's normal formatting and embedded-language behavior.

The preserved node prints literal lines, retaining the marker, body, internal
blank lines, leading indentation, and nested content. The existing formatter
normalizes document line endings. Check and write operations share the same
plugin, and parser or printer failures retain the existing unsupported-format
result without writing that file.

This supports preservation of these constructs without loading target plugins,
executing MkDocs configuration, or adding a dependency or dialect setting. It
does not validate extension semantics or add extension-aware link analysis.
The runner contract exercises actual check and write operations on temporary
files, verifies unchanged block content alongside formatted surrounding content,
and covers nested containers, code examples, blank lines, and repeated runs.
