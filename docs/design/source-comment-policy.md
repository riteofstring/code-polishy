# Source Comment Policy

## Decision

Comments are allowed by default. A repository explicitly selects the strict
boundary with `quality.allowComments: false`. In that mode, governed handwritten
source carries executable behavior and machine-consumed annotations, not prose
comments or docstrings. Code, types, schemas, and tests remain the local
authority for behavior and data shape. Current non-local rationale lives in a
mapped design document.

## Why this boundary exists

Repositories choose the strict boundary when source is written and read only by
AI and durable rationale is already mapped into documents. Inline prose is easy
to repeat, age, and leave behind after the surrounding implementation changes.
It also forces an agent to spend source-reading context on history or rationale
that may not govern the change at hand. A concise, explicitly mapped design
document gives that rationale one current owner and a bounded retrieval path.

The default protects repositories with human-written or carefully curated
comments from surprise deletion. In that mode, agents preserve useful accurate
comments and add one only when it conveys information the code cannot. Existing
comments are removed only when the task deliberately owns that change.

Under the strict boundary, some comment syntax remains because it is an input to
a compiler, runtime, or required static tool. That annotation stays next to the
code it controls. The closed registry in the
[code-quality policy](../policies/code-quality.md#source-comments-and-docstrings)
separates those machine inputs from prose.

## Design-context workflow

`documentation.design` maps a current document under `docs/design/` to one
module or to exact source paths. Each file receives at most one document: a
direct source mapping replaces its module mapping. Before changing governed
source, use `code-polishy design-context --files` or `--module` and read only
the returned paths. An empty result means the selected source has no recorded
non-local rationale.

Plans, rollout history, test narration, and superseded decisions do not belong
in this map. They are useful only when a task explicitly asks for them.
