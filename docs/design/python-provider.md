# Python provider

The first-party `python` pack owns Python source recognition, static project
discovery, tool invocation, and Python-specific facts. Core retains governed
inventory, exact read and write authority, policy enforcement, tool
authentication, diagnostics, and coverage decisions.

Static discovery assigns selected source to its nearest governed
`pyproject.toml`. Nested projects remain independent, and a focused operation
materializes only the authenticated members and metadata of selected scopes.
Ruff and CPython run against that isolated tree. They cannot enumerate the
target repository or observe an ambient project environment.

Ruff format uses stdin and returns proposed edits; it never changes the
materialized source, the target repository, or a cache. Ruff lint runs with
`--no-cache` in both a sealed baseline lane and a target-configuration lane.
The adapter deduplicates their findings while preserving the release-owned
baseline. The carried CPython runtime tokenizes comments and resolves
docstrings without importing target modules.

The initial executable slice claims only format and lint. Type checking,
complexity, dead code, architecture, dependency evidence, and runtime contracts
remain migration work and cannot be inferred from these claims. The pack is not
eligible for the ownership cutover until every Python ledger row has executable
evidence.
