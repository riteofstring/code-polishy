# Python provider

The first-party `python` pack owns Python source recognition, static project
discovery, tool invocation, and Python-specific facts. Core retains governed
inventory, exact read and write authority, policy enforcement, tool
authentication, diagnostics, and coverage decisions.

Static discovery assigns selected source to its nearest governed
`pyproject.toml`. Nested projects remain independent, and a focused operation
materializes only the authenticated members and metadata of selected scopes.
The exact carried CPython and packaging parser derive the supported target from
`project.requires-python`, validate policy-owned Ruff settings, and bind root,
`src`, and in-tree backend source roots into opaque scope data. Invalid metadata
withholds only its project scope while unrelated projects remain analyzable.
Ruff and CPython run against the isolated tree. They cannot enumerate the target
repository or observe an ambient project environment.

Ruff format uses stdin and returns proposed edits; it never changes the
materialized source, the target repository, or a cache. Ruff lint runs with
`--no-cache` in both a sealed baseline lane and a target-configuration lane.
The adapter deduplicates their findings while preserving the release-owned
baseline. The carried CPython runtime tokenizes comments and resolves
docstrings without importing target modules. Ruff also supplies McCabe
measurements while CPython supplies matching function depth and parameter
facts for core-owned limits. `ty` checks every authenticated member of a
selected project scope and may report diagnostics on unchanged members.

Architecture analysis runs Ruff's isolated complete and runtime import graphs
over every authenticated member of the selected project scope. CPython supplies
the authored static import site and distinguishes imports guarded by a bound
`typing.TYPE_CHECKING` name and package re-exports. The adapter reconciles both
sources, resolves local modules against the most specific authenticated source
root, and returns runtime, type-only, and re-export facts. Unresolved relative or
project-local imports remain unresolved facts, and ambiguous module locations
are findings. Invalid module layouts invalidate their scope. Calls bound to
`__import__` or `importlib.import_module` remain incomplete until an explicit
pack declaration can prove their targets; the adapter does not guess from string
arguments. Core retains module ownership, dependency direction, cycle, and
coverage enforcement.

The executable quality slice claims format, lint, complexity, type checking,
and static architecture facts. Dead code, computed imports, runtime loaders,
dependency evidence, project-environment resolution, and runtime contracts
remain migration work and cannot be inferred from these claims. The pack is not
eligible for the ownership cutover until every Python ledger row has executable
evidence.
