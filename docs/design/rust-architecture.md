# Rust Architecture Integration

The Rust facts boundary owns Cargo identity and written module resolution.
Architecture consumes its physical source closure and imports through the same
canonical graph, module ownership, direction and cycle checks as other native
languages. Module declarations establish source membership; declaration alone is
not a runtime import edge. Resolved uses and qualified references supply edges.

The [Cargo facts](cargo-facts.md) contract distinguishes written-source dependency
coverage from compilation and describes unsupported resolution. Incomplete Rust
facts withhold the canonical graph, never fabricate a successful empty graph.
Focused selections retain the connected crate closure without reporting problems
from unrelated crate graphs. Generated or provider-owned sources cannot silently
enter a native closure without their required evidence.

Discovery and syntax commands are present in the shared execution plan and use
the caller's common runner. Rust does not add another scheduler or receipt store.

Canonical fact inputs bind each Cargo manifest/root to its exact source snapshot,
parsed facts, package metadata, source partition and resolved edges. These digests
are reusable graph evidence, not local change detectors. Rust and Python analyzer
protocols, languages and project manifests cannot substitute for one another.
